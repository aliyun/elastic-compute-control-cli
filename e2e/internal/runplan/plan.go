// Package runplan builds cloud-free E2E execution units from selected cases
// and configured region profiles.
package runplan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/fixtureconfig"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

const PrimaryRole = "primary"

// Request is the complete input needed to plan a run before cloud access.
type Request struct {
	Suites             []*scenario.Suite
	Profiles           []fixtureconfig.RegionProfile
	PrimaryRegion      string
	StackPrerequisites map[string][]string // case path -> selected stack bundle requirements
}

// ExecutionUnit groups cases that have the same region-role requirement
// signature. Assignments are ordered fallback candidates for this unit.
type ExecutionUnit struct {
	ID           string
	Signature    string
	Suites       []*scenario.Suite
	Requirements map[string][]string
	DistinctFrom map[string]string
	Assignments  []Assignment
}

// Assignment is one complete role-to-region profile mapping.
type Assignment struct {
	Regions map[string]fixtureconfig.RegionProfile
}

// Build validates every selected case has at least one complete assignment.
// It returns no partial plan when any unit is unschedulable.
func Build(request Request) ([]ExecutionUnit, error) {
	if len(request.Suites) == 0 {
		return nil, fmt.Errorf("no cases selected")
	}
	if len(request.Profiles) == 0 {
		return nil, fmt.Errorf("no region profiles configured")
	}

	unitIndex := map[string]int{}
	units := make([]ExecutionUnit, 0)
	for _, suite := range request.Suites {
		requirements := map[string][]string{
			PrimaryRole: uniqueSorted(append(
				append([]string(nil), suite.RequiresPrerequisites...),
				request.StackPrerequisites[suite.Path]...,
			)),
		}
		distinct := map[string]string{}
		for role, requirement := range suite.RegionRequirements {
			requirements[role] = uniqueSorted(requirement.RequiresPrerequisites)
			if requirement.DistinctFrom != "" {
				distinct[role] = requirement.DistinctFrom
			}
		}
		signature := requirementSignature(requirements, distinct)
		if capability := dynamicCapabilitySignature(suite); capability != "" {
			signature += ";capabilities=[" + capability + "]"
		}
		if index, ok := unitIndex[signature]; ok {
			units[index].Suites = append(units[index].Suites, suite)
			continue
		}
		unitIndex[signature] = len(units)
		units = append(units, ExecutionUnit{
			Signature:    signature,
			Suites:       []*scenario.Suite{suite},
			Requirements: requirements,
			DistinctFrom: distinct,
		})
	}

	for i := range units {
		units[i].ID = fmt.Sprintf("execution-%02d", i+1)
		units[i].Assignments = buildAssignments(units[i], request.Profiles, strings.TrimSpace(request.PrimaryRegion))
		if len(units[i].Assignments) == 0 {
			paths := make([]string, len(units[i].Suites))
			for j, suite := range units[i].Suites {
				paths[j] = suite.Path
			}
			return nil, fmt.Errorf("no complete region assignment for %s (%s): %s", strings.Join(paths, ", "), units[i].Signature, describeRequirements(units[i].Requirements))
		}
	}
	return units, nil
}

func dynamicCapabilitySignature(suite *scenario.Suite) string {
	var parts []string
	ecs := suite.ParameterConstraints.ECS
	if ecs.MinENIQuantity > 0 {
		parts = append(parts, fmt.Sprintf("ecs.min-eni=%d", ecs.MinENIQuantity))
	}
	if ecs.MinENIPrivateIPAddressQuantity > 0 {
		parts = append(parts, fmt.Sprintf("ecs.min-eni-private-ip=%d", ecs.MinENIPrivateIPAddressQuantity))
	}
	if len(ecs.AllowedSystemDiskCategories) > 0 {
		categories := make([]string, 0, len(ecs.AllowedSystemDiskCategories))
		for _, category := range ecs.AllowedSystemDiskCategories {
			categories = append(categories, strings.ToLower(strings.TrimSpace(category)))
		}
		sort.Strings(categories)
		parts = append(parts, "ecs.system-disk="+strings.Join(categories, ","))
	}
	if len(ecs.AllowedDataDiskCategories) > 0 {
		categories := make([]string, 0, len(ecs.AllowedDataDiskCategories))
		for _, category := range ecs.AllowedDataDiskCategories {
			categories = append(categories, strings.ToLower(strings.TrimSpace(category)))
		}
		sort.Strings(categories)
		parts = append(parts, "ecs.data-disk="+strings.Join(categories, ","))
	}
	for _, requirement := range suite.RequiresParams {
		if requirement == "ack.upgrade_version" {
			parts = append(parts, "ack.upgrade=true")
			break
		}
	}
	return strings.Join(parts, ";")
}

func buildAssignments(unit ExecutionUnit, profiles []fixtureconfig.RegionProfile, primaryRegion string) []Assignment {
	roles := sortedRoles(unit.Requirements)
	assignments := make([]Assignment, 0)
	selected := make(map[string]fixtureconfig.RegionProfile, len(roles))
	var visit func(int)
	visit = func(index int) {
		if index == len(roles) {
			regions := make(map[string]fixtureconfig.RegionProfile, len(selected))
			for role, profile := range selected {
				regions[role] = profile
			}
			assignments = append(assignments, Assignment{Regions: regions})
			return
		}
		role := roles[index]
		for _, profile := range profiles {
			if role == PrimaryRole && primaryRegion != "" && profile.ID != primaryRegion {
				continue
			}
			if !profile.HasPrerequisites(unit.Requirements[role]) {
				continue
			}
			if violatesDistinct(role, profile, selected, unit.DistinctFrom) {
				continue
			}
			selected[role] = profile
			visit(index + 1)
			delete(selected, role)
		}
	}
	visit(0)
	return assignments
}

func violatesDistinct(role string, profile fixtureconfig.RegionProfile, selected map[string]fixtureconfig.RegionProfile, distinct map[string]string) bool {
	if otherRole := distinct[role]; otherRole != "" {
		if other, ok := selected[otherRole]; ok && other.ID == profile.ID {
			return true
		}
	}
	for existingRole, existing := range selected {
		if distinct[existingRole] == role && existing.ID == profile.ID {
			return true
		}
	}
	return false
}

func requirementSignature(requirements map[string][]string, distinct map[string]string) string {
	parts := make([]string, 0, len(requirements))
	for _, role := range sortedRoles(requirements) {
		part := role + "=[" + strings.Join(requirements[role], ",") + "]"
		if other := distinct[role]; other != "" {
			part += "!=" + other
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ";")
}

func describeRequirements(requirements map[string][]string) string {
	parts := make([]string, 0, len(requirements))
	for _, role := range sortedRoles(requirements) {
		parts = append(parts, role+"=["+strings.Join(requirements[role], ",")+"]")
	}
	return strings.Join(parts, ", ")
}

func sortedRoles(requirements map[string][]string) []string {
	roles := make([]string, 0, len(requirements))
	for role := range requirements {
		if role != PrimaryRole {
			roles = append(roles, role)
		}
	}
	sort.Strings(roles)
	return append([]string{PrimaryRole}, roles...)
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
