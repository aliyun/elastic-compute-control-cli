// Package prereqcheck verifies configured account resources before E2E cases
// that depend on them are assigned to a region profile.
package prereqcheck

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/fixtureconfig"
	paramspkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/params"
)

const (
	ACKAutoRepairPolicy          = "ack.auto_repair_policy"
	LingjunCluster               = "lingjun.cluster"
	LingjunClusterNetwork        = "lingjun.cluster_network"
	LingjunNetTest               = "lingjun.net_test"
	LingjunNetwork               = "lingjun.network"
	RGAssociatedTransferDisabled = "rg.associated_transfer_disabled"
	RGNotificationDisabled       = "rg.notification_disabled"
)

type fieldContract struct {
	Prerequisite string
	Fields       []string
}

var declarativeFieldContracts = []fieldContract{
	{Prerequisite: ACKAutoRepairPolicy},
	{Prerequisite: LingjunNetTest, Fields: []string{"cluster_id", "node_a", "node_b"}},
	{Prerequisite: LingjunNetwork, Fields: []string{"zone"}},
	{Prerequisite: LingjunClusterNetwork, Fields: []string{
		"eni_id", "resource_group_id", "vpc_id", "vswitch_id",
		"security_group_id", "hpn_zone", "zone", "machine_type", "image_id",
	}},
	{Prerequisite: RGAssociatedTransferDisabled},
	{Prerequisite: RGNotificationDisabled},
}

// RequiresValidation reports whether Check owns validation or probing for a
// prerequisite. Other prerequisites are scheduled by the generic run planner.
func RequiresValidation(prerequisite string) bool {
	switch prerequisite {
	case LingjunCluster:
		return true
	}
	for _, contract := range declarativeFieldContracts {
		if prerequisite == contract.Prerequisite {
			return true
		}
	}
	return false
}

// Options describes the profiles and prerequisite bundles needed by the
// selected cases. PrimaryRegion limits probes when --region pins the run.
type Options struct {
	Profiles      []fixtureconfig.RegionProfile
	Required      map[string]bool
	PrimaryRegion string
	EcctlBin      string
	Env           []string
}

// Warning records one configured prerequisite that cannot be used. Fatal
// probe errors are returned from Check instead of being represented here.
type Warning struct {
	Region       string
	Prerequisite string
	Reason       string
}

// Result contains profiles with unavailable bundles removed and the Lingjun
// inventory result that the runner can reuse without querying twice.
type Result struct {
	Profiles        []fixtureconfig.RegionProfile
	Warnings        []Warning
	LingjunByRegion map[string]paramspkg.LingjunResult
}

// Check performs read-only probes for the supported account prerequisites.
func Check(ctx context.Context, opt Options) (Result, error) {
	result := Result{
		Profiles:        append([]fixtureconfig.RegionProfile(nil), opt.Profiles...),
		LingjunByRegion: map[string]paramspkg.LingjunResult{},
	}
	for i := range result.Profiles {
		profile := &result.Profiles[i]
		if opt.PrimaryRegion != "" && profile.ID != opt.PrimaryRegion {
			continue
		}
		for _, contract := range declarativeFieldContracts {
			if !opt.Required[contract.Prerequisite] {
				continue
			}
			bundle, declared := profile.Prerequisites[contract.Prerequisite]
			if !declared {
				continue
			}
			if field := missingStringField(bundle, contract.Fields); field != "" {
				removePrerequisite(profile, contract.Prerequisite)
				result.Warnings = append(result.Warnings, Warning{
					Region:       profile.ID,
					Prerequisite: contract.Prerequisite,
					Reason:       fmt.Sprintf("%s.%s is empty", contract.Prerequisite, field),
				})
			}
		}
		if opt.Required[RGAssociatedTransferDisabled] {
			if _, declared := profile.Prerequisites[RGAssociatedTransferDisabled]; declared {
				status, err := probeStringField(ctx, opt, profile.ID, RGAssociatedTransferDisabled,
					"ecctl rg associated-transfer list", "status")
				if err != nil {
					return Result{}, err
				}
				if !strings.EqualFold(status, "Disable") {
					removePrerequisite(profile, RGAssociatedTransferDisabled)
					result.Warnings = append(result.Warnings, Warning{
						Region: profile.ID, Prerequisite: RGAssociatedTransferDisabled,
						Reason: fmt.Sprintf("associated transfer status is %q, want Disable before lifecycle test", status),
					})
				}
			}
		}
		if opt.Required[RGNotificationDisabled] {
			if _, declared := profile.Prerequisites[RGNotificationDisabled]; declared {
				status, err := probeBoolField(ctx, opt, profile.ID, RGNotificationDisabled,
					"ecctl rg notification get", "status")
				if err != nil {
					return Result{}, err
				}
				if status {
					removePrerequisite(profile, RGNotificationDisabled)
					result.Warnings = append(result.Warnings, Warning{
						Region: profile.ID, Prerequisite: RGNotificationDisabled,
						Reason: "resource group notification is enabled; refusing a lifecycle that restores Disabled",
					})
				}
			}
		}
		if opt.Required[LingjunClusterNetwork] {
			if bundle, declared := profile.Prerequisites[LingjunClusterNetwork]; declared {
				if missing, reason, err := probeLingjunClusterNetwork(ctx, opt, profile.ID, bundle); err != nil {
					return Result{}, err
				} else if missing {
					removePrerequisite(profile, LingjunClusterNetwork)
					result.Warnings = append(result.Warnings, Warning{
						Region:       profile.ID,
						Prerequisite: LingjunClusterNetwork,
						Reason:       reason,
					})
				}
			} else {
				resolved, err := resolveLingjunClusterNetwork(ctx, opt, profile.ID)
				if err != nil {
					if paramspkg.IsFatalQueryError(err) {
						return Result{}, fmt.Errorf("probe region %q prerequisite %s: %w", profile.ID, LingjunClusterNetwork, err)
					}
					result.Warnings = append(result.Warnings, Warning{
						Region: profile.ID, Prerequisite: LingjunClusterNetwork,
						Reason: fmt.Sprintf("Lingjun cluster network discovery failed: %v", err),
					})
				} else {
					if profile.Prerequisites == nil {
						profile.Prerequisites = map[string]map[string]any{}
					}
					profile.Prerequisites[LingjunClusterNetwork] = resolved
				}
			}
		}
		if opt.Required[LingjunNetwork] {
			if _, declared := profile.Prerequisites[LingjunNetwork]; !declared {
				zone, err := probeLingjunZone(ctx, opt, profile.ID)
				if err != nil {
					removePrerequisite(profile, LingjunNetwork)
					result.Warnings = append(result.Warnings, Warning{
						Region: profile.ID, Prerequisite: LingjunNetwork,
						Reason: fmt.Sprintf("Lingjun zone discovery failed: %v", err),
					})
				} else {
					if profile.Prerequisites == nil {
						profile.Prerequisites = map[string]map[string]any{}
					}
					profile.Prerequisites[LingjunNetwork] = map[string]any{"zone": zone}
				}
			}
		}
		if opt.Required[LingjunCluster] {
			if bundle, declared := profile.Prerequisites[LingjunCluster]; declared {
				nodeGroupIDs := stringSlice(bundle["node_group_ids"])
				resolved, err := paramspkg.ResolveLingjun(ctx, query(opt, profile.ID), profile.ID, "Lite", nodeGroupIDs)
				if err != nil {
					if paramspkg.IsFatalQueryError(err) {
						return Result{}, fmt.Errorf("probe region %q prerequisite %s: %w", profile.ID, LingjunCluster, err)
					}
					removePrerequisite(profile, LingjunCluster)
					result.Warnings = append(result.Warnings, Warning{Region: profile.ID, Prerequisite: LingjunCluster, Reason: err.Error()})
				} else {
					result.LingjunByRegion[profile.ID] = resolved
				}
			}
		}
	}
	return result, nil
}

func probeStringField(ctx context.Context, opt Options, region, prerequisite, command, field string) (string, error) {
	result := execpkg.Run(ctx, execpkg.Config{Bin: opt.EcctlBin, Region: region, Env: opt.Env}, command)
	if result.Err != nil {
		return "", fmt.Errorf("probe region %q prerequisite %s: %w", region, prerequisite, result.Err)
	}
	if result.Exit != 0 {
		code := findErrorString(result.JSON, "code")
		message := findErrorString(result.JSON, "message")
		return "", fmt.Errorf("probe region %q prerequisite %s: %s exited %d: %s %s", region, prerequisite, result.Command, result.Exit, code, message)
	}
	if result.JSON == nil {
		return "", fmt.Errorf("probe region %q prerequisite %s: %s returned no JSON", region, prerequisite, result.Command)
	}
	value := strings.TrimSpace(findJSONString(result.JSON, field))
	if value == "" {
		return "", fmt.Errorf("probe region %q prerequisite %s: %s response omitted %s", region, prerequisite, result.Command, field)
	}
	return value, nil
}

func probeBoolField(ctx context.Context, opt Options, region, prerequisite, command, field string) (bool, error) {
	result := execpkg.Run(ctx, execpkg.Config{Bin: opt.EcctlBin, Region: region, Env: opt.Env}, command)
	if result.Err != nil {
		return false, fmt.Errorf("probe region %q prerequisite %s: %w", region, prerequisite, result.Err)
	}
	if result.Exit != 0 {
		code := findErrorString(result.JSON, "code")
		message := findErrorString(result.JSON, "message")
		return false, fmt.Errorf("probe region %q prerequisite %s: %s exited %d: %s %s", region, prerequisite, result.Command, result.Exit, code, message)
	}
	if result.JSON == nil {
		return false, fmt.Errorf("probe region %q prerequisite %s: %s returned no JSON", region, prerequisite, result.Command)
	}
	value, found := findJSONBool(result.JSON, field)
	if !found {
		return false, fmt.Errorf("probe region %q prerequisite %s: %s response omitted boolean %s", region, prerequisite, result.Command, field)
	}
	return value, nil
}

func probeLingjunClusterNetwork(ctx context.Context, opt Options, region string, bundle map[string]any) (bool, string, error) {
	eniID, _ := bundle["eni_id"].(string)
	command := strings.Join([]string{
		"ecctl call eflo ListElasticNetworkInterfaces",
		"--ElasticNetworkInterfaceId", strconv.Quote(strings.TrimSpace(eniID)),
		"--PageSize", "10",
	}, " ")
	result := execpkg.Run(ctx, execpkg.Config{Bin: opt.EcctlBin, Region: region, Env: opt.Env}, command)
	if result.Err != nil {
		return false, "", fmt.Errorf("probe region %q prerequisite %s: %w", region, LingjunClusterNetwork, result.Err)
	}
	if result.Exit != 0 {
		code := findErrorString(result.JSON, "code")
		message := findErrorString(result.JSON, "message")
		return false, "", fmt.Errorf(
			"probe region %q prerequisite %s: %s exited %d: %s %s",
			region, LingjunClusterNetwork, result.Command, result.Exit, code, message,
		)
	}
	if result.JSON == nil {
		return false, "", fmt.Errorf(
			"probe region %q prerequisite %s: %s returned no JSON",
			region, LingjunClusterNetwork, result.Command,
		)
	}
	if actual := findJSONString(result.JSON, "ElasticNetworkInterfaceId"); actual != strings.TrimSpace(eniID) {
		return true, fmt.Sprintf("Lingjun CUSTOM ENI %q does not exist", eniID), nil
	}
	for configured, output := range map[string]string{
		"resource_group_id": "ResourceGroupId",
		"vpc_id":            "VpcId",
		"vswitch_id":        "VSwitchId",
		"security_group_id": "SecurityGroupId",
		"zone":              "ZoneId",
	} {
		expected, _ := bundle[configured].(string)
		if actual := findJSONString(result.JSON, output); actual != strings.TrimSpace(expected) {
			return true, fmt.Sprintf(
				"Lingjun CUSTOM ENI %q field %s is %q, want %q",
				eniID, output, actual, strings.TrimSpace(expected),
			), nil
		}
	}
	if actual := findJSONString(result.JSON, "Type"); !strings.EqualFold(actual, "CUSTOM") {
		return true, fmt.Sprintf("Lingjun ENI %q type is %q, want CUSTOM", eniID, actual), nil
	}
	return false, "", nil
}

// resolveLingjunClusterNetwork discovers the account-owned Lingjun cluster
// network inputs that a run cannot create: the zone (and the HPN zone derived
// from it), a public machine type with its image, and a resource group. The
// VPC, vSwitch and security group are provisioned by the run's shared stack
// instead.
//
// It validates each HPN zone + machine type combination using
// ListMachineNetworkInfo and returns the first valid match, skipping zones
// or machine types that are incompatible.
func resolveLingjunClusterNetwork(ctx context.Context, opt Options, region string) (map[string]any, error) {
	q := query(opt, region)

	raw, err := q(ctx, "ecctl call eflo-controller DescribeZones")
	if err != nil {
		return nil, err
	}
	zones := allPublicZones(raw)
	if len(zones) == 0 {
		return nil, fmt.Errorf("no public Lingjun zone found in region %q", region)
	}

	machineTypes, err := q(ctx, "ecctl call eflo-controller ListMachineTypes")
	if err != nil {
		return nil, err
	}
	images, err := q(ctx, "ecctl call eflo-controller ListImages")
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		hpnZone, err := deriveHPNZone(zone)
		if err != nil {
			continue
		}
		machineType, imageID := firstCompatibleLingjunProfile(machineTypes, images, func(mt string) (bool, error) {
			return checkMachineNetworkInfo(ctx, q, region, hpnZone, mt)
		})
		if machineType == "" {
			continue
		}

		raw, err = q(ctx, "ecctl rg group list --limit 100")
		if err != nil {
			return nil, err
		}
		groupID := firstResourceGroup(raw)
		if groupID == "" {
			return nil, fmt.Errorf("no usable resource group found in region %q", region)
		}

		return map[string]any{
			"zone":              zone,
			"hpn_zone":          hpnZone,
			"machine_type":      machineType,
			"image_id":          imageID,
			"resource_group_id": groupID,
		}, nil
	}
	return nil, fmt.Errorf("no compatible Lingjun cluster network configuration found in region %q", region)
}

// deriveHPNZone maps a Lingjun zone to its HPN zone: the zone suffix letter
// is the HPN zone letter and the HPN zone number is always 1, e.g.
// cn-hangzhou-b -> B1, cn-zhangjiakou-d -> D1.
func deriveHPNZone(zone string) (string, error) {
	suffix := zone[strings.LastIndex(zone, "-")+1:]
	if len(suffix) != 1 || suffix < "a" || suffix > "z" {
		return "", fmt.Errorf("cannot derive HPN zone from zone %q", zone)
	}
	return strings.ToUpper(suffix) + "1", nil
}

// probeResourceGroup returns the ID of a usable resource group in the region.
// Resource groups are account-owned and cannot be created by the run.
func probeResourceGroup(ctx context.Context, opt Options, region string) (string, error) {
	result := execpkg.Run(ctx, execpkg.Config{Bin: opt.EcctlBin, Region: region, Env: opt.Env},
		"ecctl rg group list --limit 100")
	if result.Err != nil {
		return "", fmt.Errorf("list resource groups: %w", result.Err)
	}
	if result.Exit != 0 {
		code := findErrorString(result.JSON, "code")
		message := findErrorString(result.JSON, "message")
		return "", fmt.Errorf("list resource groups exited %d: %s %s", result.Exit, code, message)
	}
	if result.JSON == nil {
		return "", fmt.Errorf("list resource groups returned no JSON")
	}
	groupID := firstResourceGroup(result.JSON)
	if groupID == "" {
		return "", fmt.Errorf("no usable resource group found in region %q", region)
	}
	return groupID, nil
}

// firstResourceGroup walks the response for the first usable resource group,
// preferring the default group over the first OK one.
func firstResourceGroup(value any) string {
	var groups []struct{ id, status, name string }
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			id, status, name := "", "", ""
			for field, child := range typed {
				switch {
				case strings.EqualFold(field, "Id"):
					id, _ = child.(string)
				case strings.EqualFold(field, "Status"):
					status, _ = child.(string)
				case strings.EqualFold(field, "Name"):
					name, _ = child.(string)
				}
			}
			if strings.HasPrefix(id, "rg-") {
				groups = append(groups, struct{ id, status, name string }{id, status, name})
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	for _, group := range groups {
		if strings.EqualFold(group.name, "default") && usableResourceGroup(group.status) {
			return group.id
		}
	}
	for _, group := range groups {
		if usableResourceGroup(group.status) {
			return group.id
		}
	}
	return ""
}

func usableResourceGroup(status string) bool {
	return status == "" || strings.EqualFold(status, "OK")
}

func query(opt Options, region string) paramspkg.Query {
	return func(ctx context.Context, command string) (any, error) {
		result := execpkg.Run(ctx, execpkg.Config{Bin: opt.EcctlBin, Region: region, Env: opt.Env}, command)
		if result.Err != nil {
			return nil, paramspkg.MarkFatalQueryError(result.Err)
		}
		if result.Exit != 0 {
			code := findErrorString(result.JSON, "code")
			message := findErrorString(result.JSON, "message")
			return nil, paramspkg.MarkFatalQueryError(fmt.Errorf("%s exited %d: %s %s", result.Command, result.Exit, code, message))
		}
		if result.JSON == nil {
			return nil, paramspkg.MarkFatalQueryError(fmt.Errorf("%s returned no JSON", result.Command))
		}
		return result.JSON, nil
	}
}

func removePrerequisite(profile *fixtureconfig.RegionProfile, name string) {
	prerequisites := make(map[string]map[string]any, len(profile.Prerequisites))
	for current, bundle := range profile.Prerequisites {
		if current != name {
			prerequisites[current] = bundle
		}
	}
	profile.Prerequisites = prerequisites
}

func stringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil
			}
			result = append(result, text)
		}
		return result
	default:
		return nil
	}
}

func missingStringField(bundle map[string]any, fields []string) string {
	for _, field := range fields {
		value, ok := bundle[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return field
		}
	}
	return ""
}

func findJSONString(value any, key string) string {
	switch current := value.(type) {
	case map[string]any:
		for field, child := range current {
			if strings.EqualFold(field, key) {
				if text, ok := child.(string); ok {
					return text
				}
			}
			if text := findJSONString(child, key); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range current {
			if text := findJSONString(child, key); text != "" {
				return text
			}
		}
	}
	return ""
}

func findJSONBool(value any, key string) (bool, bool) {
	switch current := value.(type) {
	case map[string]any:
		for field, child := range current {
			if strings.EqualFold(field, key) {
				if boolean, ok := child.(bool); ok {
					return boolean, true
				}
			}
			if boolean, ok := findJSONBool(child, key); ok {
				return boolean, true
			}
		}
	case []any:
		for _, child := range current {
			if boolean, ok := findJSONBool(child, key); ok {
				return boolean, true
			}
		}
	}
	return false, false
}

func probeLingjunZone(ctx context.Context, opt Options, region string) (string, error) {
	result := execpkg.Run(ctx, execpkg.Config{Bin: opt.EcctlBin, Region: region, Env: opt.Env},
		"ecctl call eflo-controller DescribeZones")
	if result.Err != nil {
		return "", fmt.Errorf("DescribeZones: %w", result.Err)
	}
	if result.Exit != 0 {
		code := findErrorString(result.JSON, "code")
		message := findErrorString(result.JSON, "message")
		return "", fmt.Errorf("DescribeZones exited %d: %s %s", result.Exit, code, message)
	}
	if result.JSON == nil {
		return "", fmt.Errorf("DescribeZones returned no JSON")
	}
	zone := firstPublicZone(result.JSON)
	if zone == "" {
		return "", fmt.Errorf("no public Lingjun zone found in region %q", region)
	}
	return zone, nil
}

// allPublicZones returns all non-alipay Lingjun zone IDs from the
// DescribeZones response.
func allPublicZones(value any) []string {
	var zones []string
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for field, child := range typed {
				if strings.EqualFold(field, "ZoneId") {
					if text, ok := child.(string); ok && strings.TrimSpace(text) != "" && !strings.Contains(strings.ToLower(text), "alipay") {
						zones = append(zones, text)
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return zones
}

func firstPublicZone(value any) string {
	zones := allPublicZones(value)
	if len(zones) > 0 {
		return zones[0]
	}
	return ""
}

// firstCompatibleLingjunProfile iterates through public machine types from
// ListMachineTypes, finds all matching images, and validates each HPN zone +
// machine type combination using the provided check function. Returns the
// first valid machine type name and the image ID with the highest version,
// or empty strings if none is compatible.
func firstCompatibleLingjunProfile(machineTypes, images any, check func(mt string) (bool, error)) (string, string) {
	for _, machine := range nestedMapsWithKey(machineTypes, "Name") {
		if !strings.EqualFold(firstStringKey(machine, "Type"), "Public") {
			continue
		}
		machineType := firstStringKey(machine, "Name")
		ok, err := check(machineType)
		if err != nil || !ok {
			continue
		}
		// Collect all matching images and pick the one with the highest
		// version; the first match may be expired and the API does not
		// provide an expiry flag.
		bestImageID, bestVersion := "", ""
		for _, image := range nestedMapsWithKey(images, "ImageId", "ImageID") {
			searchable := firstStringKey(image, "Description") + " " + firstStringKey(image, "ImageName")
			if strings.Contains(strings.ToLower(searchable), strings.ToLower(machineType)) {
				if version := firstStringKey(image, "ImageVersion"); version > bestVersion {
					bestVersion = version
					bestImageID = firstStringKey(image, "ImageId", "ImageID")
				}
			}
		}
		if bestImageID != "" {
			return machineType, bestImageID
		}
	}
	return "", ""
}

// findMatchingLingjunImage finds the first image whose description or name
// contains the given machine type name.
func findMatchingLingjunImage(machineType string, images any) string {
	for _, image := range nestedMapsWithKey(images, "ImageId", "ImageID") {
		searchable := firstStringKey(image, "Description") + " " + firstStringKey(image, "ImageName")
		if strings.Contains(strings.ToLower(searchable), strings.ToLower(machineType)) {
			return firstStringKey(image, "ImageId", "ImageID")
		}
	}
	return ""
}

// checkMachineNetworkInfo calls ListMachineNetworkInfo to verify that the
// HPN zone and machine type combination is valid in the given region.
func checkMachineNetworkInfo(ctx context.Context, q paramspkg.Query, region, hpnZone, machineType string) (bool, error) {
	request := fmt.Sprintf(`{"MachineHpnInfo":[{"HpnZone":"%s","MachineType":"%s","RegionId":"%s"}]}`, hpnZone, machineType, region)
	raw, err := q(ctx, "ecctl call eflo-controller ListMachineNetworkInfo --request '"+request+"'")
	if err != nil {
		return false, err
	}
	// A non-empty MachineNetworkInfo array means the combination is valid.
	return hasNonEmptyMachineNetworkInfo(raw), nil
}

// hasNonEmptyMachineNetworkInfo checks whether the ListMachineNetworkInfo
// response contains at least one entry in the MachineNetworkInfo array.
func hasNonEmptyMachineNetworkInfo(value any) bool {
	found := false
	var walk func(any)
	walk = func(current any) {
		if found {
			return
		}
		switch typed := current.(type) {
		case map[string]any:
			for field, child := range typed {
				if strings.EqualFold(field, "MachineNetworkInfo") {
					if arr, ok := child.([]any); ok && len(arr) > 0 {
						found = true
						return
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return found
}

// nestedMapsWithKey walks the JSON tree and returns all maps that contain
// at least one of the given keys. This is a local copy of the logic in
// the params package to avoid exporting.
func nestedMapsWithKey(value any, keys ...string) []map[string]any {
	var result []map[string]any
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			if firstStringKey(typed, keys...) != "" {
				result = append(result, typed)
			}
			childKeys := make([]string, 0, len(typed))
			for key := range typed {
				childKeys = append(childKeys, key)
			}
			for _, key := range childKeys {
				walk(typed[key])
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

// firstStringKey returns the first non-empty string value found for any of
// the given keys in the map. This is a local copy of the logic in the
// params package to avoid exporting.
func firstStringKey(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
		for k, v := range m {
			if strings.EqualFold(k, key) {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func findErrorString(value any, key string) string {
	if root, ok := value.(map[string]any); ok {
		if envelope, ok := root["error"].(map[string]any); ok {
			if text, ok := envelope[key].(string); ok {
				return text
			}
		}
	}
	return findJSONString(value, key)
}
