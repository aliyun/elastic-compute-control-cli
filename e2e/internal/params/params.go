// Package params resolves bounded, account-compatible values before an E2E run
// creates its shared fixture.
package params

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Query issues one read-only ecctl command and returns its decoded JSON output.
type Query func(context.Context, string) (any, error)

// FatalQueryError marks an error that makes the whole run invalid (for
// example credentials or permission failures), rather than a candidate that
// is simply unavailable in the current region.
type FatalQueryError struct{ Err error }

func (e *FatalQueryError) Error() string { return e.Err.Error() }
func (e *FatalQueryError) Unwrap() error { return e.Err }

func MarkFatalQueryError(err error) error {
	if err == nil {
		return nil
	}
	return &FatalQueryError{Err: err}
}

func IsFatalQueryError(err error) bool {
	var fatal *FatalQueryError
	return errors.As(err, &fatal)
}

// IsCandidateUnavailable reports whether a query failed because the current
// instance/zone/disk candidate cannot be used. These errors are safe to skip
// while trying the bounded policy candidates; unknown API errors must not be
// silently converted into a region fallback.
func IsCandidateUnavailable(err error) bool {
	if err == nil {
		return false
	}
	return IsCandidateUnavailableText(err.Error())
}

func IsCandidateUnavailableText(text string) bool {
	text = strings.ToLower(strings.ReplaceAll(text, "_", ""))
	for _, marker := range []string{
		"invalidregionid",
		"invalidregion",
		"regionnotsupport",
		"regionsnotsupported",
		"unsupportedregion",
		"forbidden.region",
		"forbiddenregion",
		"invalidzoneid",
		"zonenotsupported",
		"unsupportedzone",
		"invalidparameter.cluster",
		"invalidparametercluster",
		"unsupportedclustertype",
		"invalidclustertype",
		"invalidinstancetype",
		"unsupportedinstancetype",
		"invaliddiskcategory",
		"unsupporteddiskcategory",
		"insufficientstock",
		"nostock",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// ECSPolicy bounds the resolver to reviewed candidates rather than allowing an
// unbounded account inventory scan.
type ECSPolicy struct {
	Cores           []int  `yaml:"cores"`
	ImageFamily     string `yaml:"image_family"`
	ImageOwnerAlias string `yaml:"image_owner_alias"`
	IOOptimized     string `yaml:"io_optimized"`
	StaticImageID   string `yaml:"static_image_id"`
}

// Policy contains all bounded selector inputs used by a run.
type Policy struct {
	ECS ECSPolicy `yaml:"ecs"`
}

// Supports reports whether a dotted template key can be produced by the
// current bounded resolver policy.
func (p Policy) Supports(key string) bool {
	switch key {
	case "ecs.region", "ecs.zone", "ecs.instance_type", "ecs.image_id", "ecs.system_disk_category", "ecs.data_disk_category":
		return true
	case "ack.region", "ack.cluster_type", "ack.version", "ack.upgrade_version", "ack.edition", "ack.profile", "ack.runtime", "ack.runtime_version", "ack.zone", "ack.instance_type", "ack.image_id", "ack.system_disk_category", "ack.data_disk_category":
		return true
	case "lingjun.region", "lingjun.cluster_type", "lingjun.hpn_zone", "lingjun.zone", "lingjun.machine_type", "lingjun.image_id":
		return true
	default:
		return false
	}
}

// LoadPolicy reads the tracked parameter policy and validates its structure.
// Candidate-set requirements are checked later by ValidateFor, once the exact
// parameter keys selected for a run are known.
func LoadPolicy(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	var policy Policy
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&policy); err != nil {
		return Policy{}, fmt.Errorf("%s: %w", path, err)
	}
	seenCores := map[int]bool{}
	for _, core := range policy.ECS.Cores {
		if core <= 0 {
			return Policy{}, fmt.Errorf("%s: ecs cores must contain only positive integers", path)
		}
		if seenCores[core] {
			return Policy{}, fmt.Errorf("%s: ecs cores must not contain duplicate values", path)
		}
		seenCores[core] = true
	}
	if policy.ECS.IOOptimized != "" && policy.ECS.IOOptimized != "optimized" {
		return Policy{}, fmt.Errorf("%s: ecs io_optimized must be optimized", path)
	}
	return policy, nil
}

// ValidateFor checks only the policy fields required to resolve the requested
// keys. Metadata-only ACK and zone-only ECS runs therefore do not need an ECS
// instance or image candidate policy.
func (p Policy) ValidateFor(keys []string) error {
	needsInstanceCandidates := false
	needsImageSelector := false
	for _, key := range keys {
		switch key {
		case "ecs.instance_type", "ecs.image_id", "ecs.system_disk_category", "ecs.data_disk_category",
			"ack.instance_type", "ack.image_id", "ack.system_disk_category", "ack.data_disk_category":
			needsInstanceCandidates = true
		}
		switch key {
		case "ecs.image_id", "ack.image_id":
			needsImageSelector = true
		}
	}
	if needsInstanceCandidates && len(p.ECS.Cores) == 0 {
		return fmt.Errorf("ecs policy requires a non-empty cores list for selected parameters")
	}
	if needsImageSelector && strings.TrimSpace(p.ECS.ImageFamily) == "" && strings.TrimSpace(p.ECS.StaticImageID) == "" {
		return fmt.Errorf("ecs policy requires image_family or static_image_id for selected parameters")
	}
	return nil
}

// ECSResult is exposed to case templates below .params.ecs.
type ECSResult struct {
	Region             string
	Zone               string
	InstanceType       string
	ImageID            string
	SystemDiskCategory string
	DataDiskCategory   string
}

// ECSConstraints narrows the stocked ECS tuples to capabilities required by
// selected cases. Empty constraints preserve the default resolver behavior.
type ECSConstraints struct {
	MinENIQuantity                 int
	MinENIPrivateIPAddressQuantity int
	AllowedSystemDiskCategories    []string
	AllowedDataDiskCategories      []string
}

// NoCompatibleECSCombinationError means inventory queries succeeded but no
// stocked tuple satisfied the requested capabilities. Runners may safely skip
// only the constrained cases instead of treating this as a setup failure.
type NoCompatibleECSCombinationError struct {
	Region       string
	ExplicitZone string
}

func (e *NoCompatibleECSCombinationError) Error() string {
	if e.ExplicitZone != "" {
		return fmt.Sprintf("no compatible ECS test combination found in explicit zone %q", e.ExplicitZone)
	}
	return fmt.Sprintf("no compatible ECS test combination found in region %q", e.Region)
}

func IsNoCompatibleECSCombination(err error) bool {
	var target *NoCompatibleECSCombinationError
	return errors.As(err, &target)
}

// ACKResult contains the API-selected ACK mode and version together with the
// ECS-compatible node profile that the cluster/nodepool case can consume.
type ACKResult struct {
	Region             string
	ClusterType        string
	Version            string
	UpgradeVersion     string
	Edition            string
	Profile            string
	Runtime            string
	RuntimeVersion     string
	Zone               string
	InstanceType       string
	ImageID            string
	SystemDiskCategory string
	DataDiskCategory   string
}

// NoCompatibleACKUpgradePathError means creatable ACK versions exist, but none
// exposes an upgrade target. Upgrade-only cases may be skipped independently.
type NoCompatibleACKUpgradePathError struct {
	Region string
}

func (e *NoCompatibleACKUpgradePathError) Error() string {
	return fmt.Sprintf("no compatible ACK upgrade path found in region %q", e.Region)
}

func IsNoCompatibleACKUpgradePath(err error) bool {
	var target *NoCompatibleACKUpgradePathError
	return errors.As(err, &target)
}

// LingjunResult contains the available Lite node profile discovered for the
// selected region. Network IDs remain dependency outputs from the shared stack.
type LingjunResult struct {
	Region      string
	ClusterType string
	HPNZone     string
	Zone        string
	MachineType string
	ImageID     string
}

// ResolveACK selects the first cluster type with at least one creatable
// Kubernetes version. Cluster types are deliberately supplied by code/policy,
// not by account-specific global configuration.
func ResolveACK(ctx context.Context, query Query, region string, clusterTypes, requirements []string, ecs ECSResult) (ACKResult, error) {
	if query == nil {
		return ACKResult{}, fmt.Errorf("ACK parameter query is required")
	}
	requireUpgrade := containsString(requirements, "ack.upgrade_version")
	requireRuntime := containsString(requirements, "ack.runtime") || containsString(requirements, "ack.runtime_version")
	foundCreatableVersion := false
	for _, clusterType := range uniqueStrings(clusterTypes) {
		value, err := query(ctx, ackVersionCommand(clusterType))
		if err != nil {
			if IsFatalQueryError(err) || !IsCandidateUnavailable(err) {
				return ACKResult{}, err
			}
			continue
		}
		version, upgradeVersion, versionInfo := firstACKVersionInfo(value, requireUpgrade, requireRuntime)
		if version == "" && requireUpgrade {
			for _, candidateInfo := range allACKVersionInfos(value, requireRuntime) {
				candidateVersion := firstStringForAnyKey(candidateInfo, "version", "kubernetes_version")
				if candidateVersion == "" {
					continue
				}
				foundCreatableVersion = true
				upgradeValue, err := query(ctx, ackUpgradeVersionCommand(clusterType, candidateVersion))
				if err != nil {
					if IsFatalQueryError(err) || !IsCandidateUnavailable(err) {
						return ACKResult{}, err
					}
					continue
				}
				queriedVersion, queriedUpgrade, _ := firstACKVersionInfo(upgradeValue, true, false)
				if queriedVersion != candidateVersion || queriedUpgrade == "" {
					continue
				}
				version, upgradeVersion, versionInfo = candidateVersion, queriedUpgrade, candidateInfo
				break
			}
		}
		if version == "" {
			continue
		}
		return ackResult(region, clusterType, version, upgradeVersion, versionInfo, ecs), nil
	}
	if requireUpgrade && foundCreatableVersion {
		return ACKResult{}, &NoCompatibleACKUpgradePathError{Region: region}
	}
	return ACKResult{}, fmt.Errorf("no compatible creatable ACK cluster version found in region %q", region)
}

func ackResult(region, clusterType, version, upgradeVersion string, versionInfo map[string]any, ecs ECSResult) ACKResult {
	runtimeName, runtimeVersion := firstACKRuntime(versionInfo)
	edition := firstStringForAnyKey(versionInfo, "edition", "cluster_edition")
	if edition == "" {
		edition = "ack.standard"
	}
	profile := firstStringForAnyKey(versionInfo, "profile", "scenario")
	if profile == "" {
		profile = "Default"
	}
	return ACKResult{
		Region: region, ClusterType: clusterType, Version: version, UpgradeVersion: upgradeVersion,
		Edition: edition, Profile: profile, Runtime: runtimeName, RuntimeVersion: runtimeVersion,
		Zone: ecs.Zone, InstanceType: ecs.InstanceType, ImageID: ecs.ImageID,
		SystemDiskCategory: ecs.SystemDiskCategory, DataDiskCategory: ecs.DataDiskCategory,
	}
}

func ackVersionCommand(clusterType string) string {
	return "ecctl ack version list --cluster-type " + clusterType + " --mode creatable"
}

func ackUpgradeVersionCommand(clusterType, version string) string {
	return "ecctl ack version list --cluster-type " + clusterType + " --kubernetes-version " + version + " --query-upgradable-version"
}

func allACKVersionInfos(value any, requireRuntime bool) []map[string]any {
	var result []map[string]any
	var walk func(any)
	walk = func(v any) {
		switch current := v.(type) {
		case map[string]any:
			candidate := firstStringForAnyKey(current, "version", "kubernetes_version")
			creatable, hasCreatable := firstBoolForAnyKey(current, "creatable", "is_creatable")
			runtimeName, runtimeVersion := firstACKRuntime(current)
			looksLikeVersion := hasCreatable || hasNormalizedKey(current, "runtimes") || hasNormalizedKey(current, "upgradable_versions")
			if candidate != "" && looksLikeVersion && (!hasCreatable || creatable) &&
				(!requireRuntime || (runtimeName != "" && runtimeVersion != "")) {
				result = append(result, current)
				return
			}
			for _, child := range current {
				walk(child)
			}
		case []any:
			for _, child := range current {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

func hasNormalizedKey(value map[string]any, wanted string) bool {
	for key := range value {
		if normalizeFieldKey(key) == normalizeFieldKey(wanted) {
			return true
		}
	}
	return false
}

func firstACKVersionInfo(value any, requireUpgrade, requireRuntime bool) (string, string, map[string]any) {
	var version, upgradeVersion string
	var versionInfo map[string]any
	var walk func(any)
	walk = func(v any) {
		if version != "" {
			return
		}
		switch x := v.(type) {
		case map[string]any:
			candidate := firstStringForAnyKey(x, "version", "kubernetes_version")
			upgrades := firstStringsForAnyKey(x, "upgradable_versions", "upgrade_versions")
			runtimeName, runtimeVersion := firstACKRuntime(x)
			if candidate != "" && (!requireUpgrade || len(upgrades) > 0) &&
				(!requireRuntime || (runtimeName != "" && runtimeVersion != "")) {
				creatable, hasCreatable := firstBoolForAnyKey(x, "creatable", "is_creatable")
				if !hasCreatable || creatable {
					version, versionInfo = candidate, x
					if len(upgrades) > 0 {
						upgradeVersion = upgrades[0]
					}
					return
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(value)
	return version, upgradeVersion, versionInfo
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func firstACKRuntime(value map[string]any) (string, string) {
	for key, child := range value {
		if normalizeFieldKey(key) != normalizeFieldKey("runtimes") {
			continue
		}
		items, ok := child.([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			runtime, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := firstStringForAnyKey(runtime, "name", "runtime")
			version := firstStringForAnyKey(runtime, "version", "runtime_version")
			if strings.EqualFold(name, "containerd") && version != "" {
				return name, version
			}
		}
	}
	return "", ""
}

func firstStringsForAnyKey(value map[string]any, keys ...string) []string {
	wanted := map[string]bool{}
	for _, key := range keys {
		wanted[normalizeFieldKey(key)] = true
	}
	for key, child := range value {
		if !wanted[normalizeFieldKey(key)] {
			continue
		}
		var result []string
		switch items := child.(type) {
		case []any:
			for _, item := range items {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					result = append(result, text)
				}
			}
		case []string:
			for _, item := range items {
				if strings.TrimSpace(item) != "" {
					result = append(result, item)
				}
			}
		}
		return result
	}
	return nil
}

// ResolveLingjun verifies that each configured node group has a compatible
// free node in the selected region. Node-group IDs are the only account-owned
// Lingjun inputs; zone, HPN zone, machine type and image all come from the live
// inventory.
func ResolveLingjun(ctx context.Context, query Query, region, clusterType string, nodeGroupIDs []string) (LingjunResult, error) {
	if query == nil {
		return LingjunResult{}, fmt.Errorf("Lingjun parameter query is required")
	}
	if strings.TrimSpace(clusterType) == "" {
		return LingjunResult{}, fmt.Errorf("Lingjun cluster type is required")
	}
	if len(nodeGroupIDs) != 2 {
		return LingjunResult{}, fmt.Errorf("Lingjun cluster requires exactly two node_group_ids")
	}
	normalizedNodeGroupIDs := make([]string, len(nodeGroupIDs))
	for i := range nodeGroupIDs {
		normalizedNodeGroupIDs[i] = strings.TrimSpace(nodeGroupIDs[i])
		if normalizedNodeGroupIDs[i] == "" {
			return LingjunResult{}, fmt.Errorf("Lingjun cluster node_group_ids must not contain empty values")
		}
	}
	nodeGroupIDs = normalizedNodeGroupIDs
	if nodeGroupIDs[0] == nodeGroupIDs[1] {
		return LingjunResult{}, fmt.Errorf("Lingjun cluster node_group_ids must be distinct")
	}
	value, err := query(ctx, "ecctl lingjun node list --free --limit 100")
	if err != nil {
		return LingjunResult{}, err
	}
	profiles := lingjunProfilesByNodeGroup(value, clusterType)
	selected := make([]LingjunResult, 0, len(nodeGroupIDs))
	for _, nodeGroupID := range nodeGroupIDs {
		profile, ok := profiles[nodeGroupID]
		if !ok {
			return LingjunResult{}, fmt.Errorf("no free Lingjun node found for node group %q in region %q", nodeGroupID, region)
		}
		selected = append(selected, profile)
	}
	if !compatibleLingjunProfiles(selected[0], selected[1]) {
		return LingjunResult{}, fmt.Errorf("Lingjun node groups %q and %q do not expose a compatible free-node profile in region %q", nodeGroupIDs[0], nodeGroupIDs[1], region)
	}
	profile := selected[0]
	profile.Region = region
	profile.ClusterType = clusterType
	return profile, nil
}

func lingjunProfilesByNodeGroup(value any, clusterType string) map[string]LingjunResult {
	result := map[string]LingjunResult{}
	var walk func(any, string)
	walk = func(v any, inheritedClusterType string) {
		switch x := v.(type) {
		case map[string]any:
			currentClusterType := inheritedClusterType
			if explicit := firstStringForAnyKey(x, "cluster_type", "clusterType"); explicit != "" {
				currentClusterType = explicit
			}
			machine := firstStringForAnyKey(x, "machine_type", "machineType", "machine")
			zone := firstStringForAnyKey(x, "zone", "zone_id", "zoneId", "az")
			hpn := firstStringForAnyKey(x, "hpn_zone", "hpnZone", "hpn")
			nodeGroup := firstStringForAnyKey(x, "node_group", "node_group_id", "nodeGroup", "nodeGroupId")
			if nodeGroup != "" && machine != "" && zone != "" && hpn != "" &&
				(currentClusterType == "" || strings.EqualFold(currentClusterType, clusterType)) {
				if _, exists := result[nodeGroup]; !exists {
					result[nodeGroup] = LingjunResult{MachineType: machine, Zone: zone, HPNZone: hpn, ImageID: firstStringForAnyKey(x, "image_id", "imageId", "image")}
				}
			}
			for _, child := range x {
				walk(child, currentClusterType)
			}
		case []any:
			for _, child := range x {
				walk(child, inheritedClusterType)
			}
		}
	}
	walk(value, "")
	return result
}

func compatibleLingjunProfiles(left, right LingjunResult) bool {
	return left.HPNZone == right.HPNZone && left.Zone == right.Zone &&
		left.MachineType == right.MachineType && left.ImageID == right.ImageID
}

func firstStringForAnyKey(value map[string]any, keys ...string) string {
	wanted := map[string]bool{}
	for _, key := range keys {
		wanted[normalizeFieldKey(key)] = true
	}
	for key, child := range value {
		if wanted[normalizeFieldKey(key)] {
			if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

func firstBoolForAnyKey(value map[string]any, keys ...string) (bool, bool) {
	wanted := map[string]bool{}
	for _, key := range keys {
		wanted[normalizeFieldKey(key)] = true
	}
	for key, child := range value {
		if !wanted[normalizeFieldKey(key)] {
			continue
		}
		switch value := child.(type) {
		case bool:
			return value, true
		case string:
			if strings.EqualFold(value, "true") {
				return true, true
			}
			if strings.EqualFold(value, "false") {
				return false, true
			}
		}
	}
	return false, false
}

func normalizeFieldKey(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	return value
}

// ResolveECS preserves the original full-profile resolver for callers that
// need every ECS dimension.
func ResolveECS(ctx context.Context, policy ECSPolicy, query Query, region, explicitZone string) (ECSResult, error) {
	return ResolveECSFor(ctx, policy, query, region, explicitZone, []string{
		"ecs.instance_type", "ecs.image_id", "ecs.system_disk_category", "ecs.data_disk_category",
	})
}

// ResolveECSFor resolves only the cloud dimensions needed by the requested
// keys. Dependencies are still discovered (for example image selection needs
// an instance type), but unrelated disk and image APIs are not queried.
func ResolveECSFor(ctx context.Context, policy ECSPolicy, query Query, region, explicitZone string, requirements []string) (ECSResult, error) {
	return ResolveECSForWithConstraints(ctx, policy, query, region, explicitZone, requirements, ECSConstraints{})
}

// ResolveECSForWithConstraints resolves the requested dimensions and rejects
// stocked candidates that do not satisfy case-specific capability constraints.
func ResolveECSForWithConstraints(ctx context.Context, policy ECSPolicy, query Query, region, explicitZone string, requirements []string, constraints ECSConstraints) (ECSResult, error) {
	if query == nil {
		return ECSResult{}, fmt.Errorf("ECS parameter query is required")
	}
	if err := validateECSConstraints(constraints); err != nil {
		return ECSResult{}, err
	}
	if err := (Policy{ECS: policy}).ValidateFor(requirements); err != nil {
		return ECSResult{}, err
	}
	wanted := map[string]bool{}
	for _, requirement := range requirements {
		wanted[requirement] = true
	}
	result := ECSResult{Region: region}
	needsZoneInventory := wanted["ecs.zone"]
	needsInstance := wanted["ecs.instance_type"] || wanted["ecs.image_id"] || wanted["ecs.system_disk_category"] || wanted["ecs.data_disk_category"]
	needsImage := wanted["ecs.image_id"]
	needsSystemDisk := wanted["ecs.system_disk_category"] || wanted["ecs.data_disk_category"] || len(constraints.AllowedSystemDiskCategories) > 0
	needsDataDisk := wanted["ecs.data_disk_category"] || len(constraints.AllowedDataDiskCategories) > 0

	var allowedZones map[string]bool
	if needsZoneInventory {
		value, err := query(ctx, "ecctl ecs zone list --verbose")
		if err != nil {
			return ECSResult{}, err
		}
		zones := vSwitchZones(value, region)
		if len(zones) == 0 {
			return ECSResult{}, fmt.Errorf("no ECS zone supporting VSwitch creation found in region %q", region)
		}
		allowedZones = make(map[string]bool, len(zones))
		for _, zone := range zones {
			allowedZones[zone] = true
		}
		if explicitZone != "" {
			if !allowedZones[explicitZone] {
				return ECSResult{}, fmt.Errorf("explicit zone %q does not support VSwitch creation", explicitZone)
			}
			result.Zone = explicitZone
		} else {
			result.Zone = zones[0]
		}
	}
	if !needsInstance {
		if result.Zone == "" {
			result.Zone = explicitZone
		}
		return result, nil
	}
	if policy.IOOptimized != "" && policy.IOOptimized != "optimized" {
		return ECSResult{}, fmt.Errorf("ECS parameter policy requires io_optimized=optimized")
	}
	ioOptimized := effectiveIOOptimized(policy.IOOptimized)
	for _, core := range policy.Cores {
		availability, err := query(ctx, availableResourceCommand(core, ioOptimized))
		if err != nil {
			if IsFatalQueryError(err) {
				return ECSResult{}, err
			}
			if !IsCandidateUnavailable(err) {
				return ECSResult{}, err
			}
			continue
		}
		candidates := stockedInstanceCandidates(availability)
		if explicitZone != "" {
			filtered := make([]instanceCandidate, 0, len(candidates))
			for _, candidate := range candidates {
				if candidate.Zone == explicitZone {
					filtered = append(filtered, candidate)
				}
			}
			candidates = filtered
		}
		if len(allowedZones) > 0 {
			filtered := make([]instanceCandidate, 0, len(candidates))
			for _, candidate := range candidates {
				if allowedZones[candidate.Zone] {
					filtered = append(filtered, candidate)
				}
			}
			candidates = filtered
		}
		for _, candidate := range candidates {
			compatible, err := instanceTypeSatisfiesConstraints(ctx, query, candidate.InstanceType, constraints)
			if err != nil {
				if IsFatalQueryError(err) || !IsCandidateUnavailable(err) {
					return ECSResult{}, err
				}
				continue
			}
			if !compatible {
				continue
			}
			systemCategories := []string{""}
			if needsSystemDisk {
				systemAvailability, err := query(ctx, diskAvailabilityCommand("SystemDisk", "", candidate.Zone, candidate.InstanceType, ioOptimized))
				if err != nil {
					if IsFatalQueryError(err) || !IsCandidateUnavailable(err) {
						return ECSResult{}, err
					}
					continue
				}
				systemCategories = availableDiskCategories(systemAvailability, "SystemDisk")
				systemCategories = allowedDiskCategories(systemCategories, constraints.AllowedSystemDiskCategories)
				if len(systemCategories) == 0 {
					continue
				}
			}
			for _, systemCategory := range systemCategories {
				dataCategories := []string{""}
				if needsDataDisk {
					dataAvailability, err := query(ctx, diskAvailabilityCommand("DataDisk", systemCategory, candidate.Zone, candidate.InstanceType, ioOptimized))
					if err != nil {
						if IsFatalQueryError(err) || !IsCandidateUnavailable(err) {
							return ECSResult{}, err
						}
						continue
					}
					dataCategories = availableDiskCategories(dataAvailability, "DataDisk")
					dataCategories = allowedDiskCategories(dataCategories, constraints.AllowedDataDiskCategories)
					if len(dataCategories) == 0 {
						continue
					}
				}
				for _, dataCategory := range dataCategories {
					imageID := strings.TrimSpace(policy.StaticImageID)
					if needsImage && strings.TrimSpace(policy.ImageFamily) != "" {
						image, err := query(ctx, imageCommand(candidate.InstanceType, policy.ImageOwnerAlias, policy.ImageFamily))
						if err != nil {
							if IsFatalQueryError(err) || !IsCandidateUnavailable(err) {
								return ECSResult{}, err
							}
							continue
						}
						imageID = firstStringForKey(image, "ImageId")
					}
					if needsImage && imageID == "" {
						continue
					}
					return ECSResult{
						Region: region, Zone: candidate.Zone, InstanceType: candidate.InstanceType, ImageID: imageID,
						SystemDiskCategory: systemCategory, DataDiskCategory: dataCategory,
					}, nil
				}
			}
		}
	}
	return ECSResult{}, &NoCompatibleECSCombinationError{Region: region, ExplicitZone: explicitZone}
}

func validateECSConstraints(constraints ECSConstraints) error {
	if constraints.MinENIQuantity < 0 {
		return fmt.Errorf("minimum ENI quantity must not be negative")
	}
	if constraints.MinENIPrivateIPAddressQuantity < 0 {
		return fmt.Errorf("minimum ENI private IP address quantity must not be negative")
	}
	seen := map[string]bool{}
	for _, category := range constraints.AllowedSystemDiskCategories {
		category = strings.TrimSpace(category)
		if category == "" {
			return fmt.Errorf("allowed system disk categories must not contain empty values")
		}
		if seen[category] {
			return fmt.Errorf("allowed system disk categories must not contain duplicates")
		}
		seen[category] = true
	}
	seen = map[string]bool{}
	for _, category := range constraints.AllowedDataDiskCategories {
		category = strings.TrimSpace(category)
		if category == "" {
			return fmt.Errorf("allowed data disk categories must not contain empty values")
		}
		if seen[category] {
			return fmt.Errorf("allowed data disk categories must not contain duplicates")
		}
		seen[category] = true
	}
	return nil
}

func instanceTypeSatisfiesConstraints(ctx context.Context, query Query, instanceType string, constraints ECSConstraints) (bool, error) {
	if constraints.MinENIQuantity == 0 && constraints.MinENIPrivateIPAddressQuantity == 0 {
		return true, nil
	}
	value, err := query(ctx, instanceTypeCapabilityCommand(instanceType, constraints))
	if err != nil {
		return false, err
	}
	return containsInstanceType(value, instanceType), nil
}

func instanceTypeCapabilityCommand(instanceType string, constraints ECSConstraints) string {
	parts := []string{"ecctl call ecs DescribeInstanceTypes --InstanceTypes.1", instanceType}
	if constraints.MinENIQuantity > 0 {
		parts = append(parts, "--MinimumEniQuantity", strconv.Itoa(constraints.MinENIQuantity))
	}
	if constraints.MinENIPrivateIPAddressQuantity > 0 {
		parts = append(parts, "--MinimumEniPrivateIpAddressQuantity", strconv.Itoa(constraints.MinENIPrivateIPAddressQuantity))
	}
	return strings.Join(parts, " ")
}

func containsInstanceType(value any, wanted string) bool {
	switch current := value.(type) {
	case map[string]any:
		if strings.EqualFold(firstStringForAnyKey(current, "InstanceTypeId", "instance_type_id"), wanted) {
			return true
		}
		for _, child := range current {
			if containsInstanceType(child, wanted) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if containsInstanceType(child, wanted) {
				return true
			}
		}
	}
	return false
}

func allowedDiskCategories(available, allowed []string) []string {
	if len(allowed) == 0 {
		return available
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, category := range allowed {
		allowedSet[strings.ToLower(strings.TrimSpace(category))] = true
	}
	result := make([]string, 0, len(available))
	for _, category := range available {
		if allowedSet[strings.ToLower(category)] {
			result = append(result, category)
		}
	}
	return result
}

func vSwitchZones(value any, region string) []string {
	var zones []string
	var walk func(any)
	walk = func(current any) {
		switch item := current.(type) {
		case map[string]any:
			zone := firstStringForAnyKey(item, "id", "zone_id", "ZoneId")
			if isPublicECSZone(region, zone) {
				for key, child := range item {
					if normalizeFieldKey(key) == normalizeFieldKey("available_resource_creation") && containsFold(flattenStrings(child), "VSwitch") {
						zones = append(zones, zone)
						break
					}
				}
			}
			for _, child := range item {
				walk(child)
			}
		case []any:
			for _, child := range item {
				walk(child)
			}
		}
	}
	walk(value)
	zones = uniqueStrings(zones)
	return zones
}

// Public ECS zones are the region ID followed by one availability-zone
// letter (for example cn-heyuan-a or ap-southeast-1a). Zone inventory can
// also contain internal Alipay/SPE entries that advertise VSwitch creation
// but reject public VPC requests; exclude those before selecting a stack zone.
func isPublicECSZone(region, zone string) bool {
	suffix := strings.TrimPrefix(zone, region)
	if suffix == zone {
		return false
	}
	if len(suffix) == 2 && suffix[0] == '-' {
		suffix = suffix[1:]
	}
	return len(suffix) == 1 && suffix[0] >= 'a' && suffix[0] <= 'z'
}

func flattenStrings(value any) []string {
	var result []string
	var walk func(any)
	walk = func(current any) {
		switch item := current.(type) {
		case string:
			result = append(result, item)
		case map[string]any:
			for _, child := range item {
				walk(child)
			}
		case []any:
			for _, child := range item {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}

func effectiveIOOptimized(value string) string {
	if value == "" {
		return "optimized"
	}
	return value
}

func availableResourceCommand(core int, ioOptimized string) string {
	parts := []string{
		"ecctl call ecs DescribeAvailableResource",
		"--DestinationResource InstanceType",
		"--InstanceChargeType PostPaid",
		fmt.Sprintf("--Cores %d", core),
		"--WithStock true",
		"--IoOptimized " + effectiveIOOptimized(ioOptimized),
	}
	return strings.Join(parts, " ")
}

func diskAvailabilityCommand(destination, systemDiskCategory, zone, instanceType, ioOptimized string) string {
	parts := []string{
		"ecctl call ecs DescribeAvailableResource",
		"--ZoneId " + zone,
		"--ResourceType instance",
		"--DestinationResource " + destination,
		"--InstanceType " + instanceType,
		"--InstanceChargeType PostPaid",
		"--WithStock true",
		"--IoOptimized " + effectiveIOOptimized(ioOptimized),
	}
	if systemDiskCategory != "" {
		parts = append(parts, "--SystemDiskCategory "+systemDiskCategory)
	}
	return strings.Join(parts, " ")
}

func imageCommand(instanceType, ownerAlias, family string) string {
	parts := []string{
		"ecctl call ecs DescribeImages",
		"--Status Available",
		"--InstanceType " + instanceType,
	}
	if ownerAlias != "" {
		parts = append(parts, "--ImageOwnerAlias "+ownerAlias)
	}
	if family != "" {
		parts = append(parts, "--ImageFamily "+family)
	}
	return strings.Join(parts, " ")
}

type instanceCandidate struct {
	Zone         string
	InstanceType string
}

// stockedInstanceCandidates parses the ECS DescribeAvailableResource shape
// explicitly. The old recursive string search could associate a type with the
// wrong zone and treated unrelated nested strings as stock evidence.
func stockedInstanceCandidates(value any) []instanceCandidate {
	var candidates []instanceCandidate
	for _, zoneValue := range listField(responseField(value), "AvailableZones", "AvailableZone") {
		zone, ok := zoneValue.(map[string]any)
		if !ok || !stockedStatus(zone) {
			continue
		}
		zoneID, _ := zone["ZoneId"].(string)
		if zoneID == "" {
			continue
		}
		for _, resourceValue := range listField(zone, "AvailableResources", "AvailableResource") {
			resource, ok := resourceValue.(map[string]any)
			if !ok || resource["Type"] != "InstanceType" {
				continue
			}
			for _, supportedValue := range listField(resource, "SupportedResources", "SupportedResource") {
				supported, ok := supportedValue.(map[string]any)
				if !ok || !stockedStatus(supported) {
					continue
				}
				instanceType, _ := supported["Value"].(string)
				if instanceType != "" {
					candidates = append(candidates, instanceCandidate{Zone: zoneID, InstanceType: instanceType})
				}
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Zone != candidates[j].Zone {
			return candidates[i].Zone < candidates[j].Zone
		}
		return candidates[i].InstanceType < candidates[j].InstanceType
	})
	result := make([]instanceCandidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		key := candidate.Zone + "\x00" + candidate.InstanceType
		if !seen[key] {
			seen[key] = true
			result = append(result, candidate)
		}
	}
	return result
}

func availableDiskCategories(value any, resourceType string) []string {
	var categories []string
	for _, zoneValue := range listField(responseField(value), "AvailableZones", "AvailableZone") {
		zone, ok := zoneValue.(map[string]any)
		if !ok || !stockedStatus(zone) {
			continue
		}
		for _, resourceValue := range listField(zone, "AvailableResources", "AvailableResource") {
			resource, ok := resourceValue.(map[string]any)
			if !ok || resource["Type"] != resourceType {
				continue
			}
			for _, supportedValue := range listField(resource, "SupportedResources", "SupportedResource") {
				supported, ok := supportedValue.(map[string]any)
				if !ok || !stockedStatus(supported) {
					continue
				}
				category, _ := supported["Value"].(string)
				if category != "" {
					categories = append(categories, category)
				}
			}
		}
	}
	categories = uniqueStrings(categories)
	sort.Strings(categories)
	return categories
}

func responseField(value any) map[string]any {
	root, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	if response, ok := root["response"].(map[string]any); ok {
		return response
	}
	return root
}

func listField(value map[string]any, path ...string) []any {
	var current any = value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	items, _ := current.([]any)
	return items
}

func stockedStatus(value map[string]any) bool {
	status, statusOK := value["Status"].(string)
	if !statusOK || !strings.EqualFold(status, "Available") {
		return false
	}
	category, hasCategory := value["StatusCategory"].(string)
	return !hasCategory || strings.EqualFold(category, "WithStock")
}

func firstStringForKey(value any, key string) string {
	switch x := value.(type) {
	case map[string]any:
		if value, ok := x[key].(string); ok {
			return value
		}
		for _, child := range x {
			if value := firstStringForKey(child, key); value != "" {
				return value
			}
		}
	case []any:
		for _, child := range x {
			if value := firstStringForKey(child, key); value != "" {
				return value
			}
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
