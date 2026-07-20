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
	ACKRootAccount = "ack.root_account"
	ECSImage       = "ecs.image"
	LingjunCluster = "lingjun.cluster"
)

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
	callerIdentityType := ""
	for i := range result.Profiles {
		profile := &result.Profiles[i]
		if opt.PrimaryRegion != "" && profile.ID != opt.PrimaryRegion {
			continue
		}
		if opt.Required[ACKRootAccount] {
			if _, declared := profile.Prerequisites[ACKRootAccount]; declared {
				if callerIdentityType == "" {
					var err error
					callerIdentityType, err = probeCallerIdentity(ctx, opt, profile.ID)
					if err != nil {
						return Result{}, err
					}
				}
				if callerIdentityType != "Account" {
					removePrerequisite(profile, ACKRootAccount)
					result.Warnings = append(result.Warnings, Warning{
						Region:       profile.ID,
						Prerequisite: ACKRootAccount,
						Reason:       fmt.Sprintf("current caller identity type %s is not Account", callerIdentityType),
					})
				}
			}
		}
		if opt.Required[ECSImage] {
			if bundle, declared := profile.Prerequisites[ECSImage]; declared {
				bucket, _ := bundle["oss_bucket"].(string)
				bucket = strings.TrimSpace(bucket)
				if bucket == "" {
					removePrerequisite(profile, ECSImage)
					result.Warnings = append(result.Warnings, Warning{Region: profile.ID, Prerequisite: ECSImage, Reason: "ecs.image.oss_bucket is empty"})
				} else if missing, reason, err := probeOSSBucket(ctx, opt, profile.ID, bucket); err != nil {
					return Result{}, err
				} else if missing {
					removePrerequisite(profile, ECSImage)
					result.Warnings = append(result.Warnings, Warning{Region: profile.ID, Prerequisite: ECSImage, Reason: reason})
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

func probeCallerIdentity(ctx context.Context, opt Options, region string) (string, error) {
	const command = "ecctl call sts GetCallerIdentity"
	result := execpkg.Run(ctx, execpkg.Config{Bin: opt.EcctlBin, Region: region, Env: opt.Env}, command)
	if result.Err != nil {
		return "", fmt.Errorf("probe region %q prerequisite %s: %w", region, ACKRootAccount, result.Err)
	}
	if result.Exit != 0 {
		code := findJSONString(result.JSON, "code")
		message := findJSONString(result.JSON, "message")
		return "", fmt.Errorf("probe region %q prerequisite %s: %s exited %d: %s %s", region, ACKRootAccount, result.Command, result.Exit, code, message)
	}
	if result.JSON == nil {
		return "", fmt.Errorf("probe region %q prerequisite %s: %s returned no JSON", region, ACKRootAccount, result.Command)
	}
	identityType := strings.TrimSpace(findJSONString(result.JSON, "IdentityType"))
	if identityType == "" {
		return "", fmt.Errorf("probe region %q prerequisite %s: %s response omitted IdentityType", region, ACKRootAccount, result.Command)
	}
	return identityType, nil
}

func probeOSSBucket(ctx context.Context, opt Options, region, bucket string) (bool, string, error) {
	command := strings.Join([]string{
		"ecctl call resourcecenter GetResourceConfiguration",
		"--ResourceId", strconv.Quote(bucket),
		"--ResourceRegionId", strconv.Quote(region),
		"--ResourceType", "ACS::OSS::Bucket",
	}, " ")
	result := execpkg.Run(ctx, execpkg.Config{Bin: opt.EcctlBin, Region: region, Env: opt.Env}, command)
	if result.Err != nil {
		return false, "", fmt.Errorf("probe region %q prerequisite %s: %w", region, ECSImage, result.Err)
	}
	if result.Exit != 0 {
		code := findJSONString(result.JSON, "code")
		message := findJSONString(result.JSON, "message")
		if strings.EqualFold(code, "NotExists.Resource") {
			if strings.TrimSpace(message) == "" {
				message = "OSS bucket does not exist"
			}
			return true, message, nil
		}
		return false, "", fmt.Errorf("probe region %q prerequisite %s: %s exited %d: %s %s", region, ECSImage, result.Command, result.Exit, code, message)
	}
	if result.JSON == nil {
		return false, "", fmt.Errorf("probe region %q prerequisite %s: %s returned no JSON", region, ECSImage, result.Command)
	}
	return false, "", nil
}

func query(opt Options, region string) paramspkg.Query {
	return func(ctx context.Context, command string) (any, error) {
		result := execpkg.Run(ctx, execpkg.Config{Bin: opt.EcctlBin, Region: region, Env: opt.Env}, command)
		if result.Err != nil {
			return nil, paramspkg.MarkFatalQueryError(result.Err)
		}
		if result.Exit != 0 {
			code := findJSONString(result.JSON, "code")
			message := findJSONString(result.JSON, "message")
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
