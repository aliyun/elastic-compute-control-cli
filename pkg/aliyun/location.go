package aliyun

import (
	"sort"
	"strings"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

// Location service used to validate region IDs without depending on a static
// list. The API is public but undocumented; the call shape mirrors what the
// aliyun-cli does internally.
const (
	locationDomain             = "location-readonly.aliyuncs.com"
	locationProduct            = "Location"
	locationVersion            = "2015-06-12"
	locationAPIName            = "DescribeEndpoints"
	locationDefaultServiceCode = "ecs"
	locationDefaultSignRegion  = "cn-hangzhou"

	// ErrCodeInvalidRegion is returned by RegionVerifier.Verify when the
	// Location service reports no endpoints for a region. Treat this as a
	// hard failure — the user almost certainly typed the region wrong.
	ErrCodeInvalidRegion = "InvalidRegion"
	// ErrCodeVerificationUnavailable is returned when the verification check
	// cannot be performed (missing credentials, network error, etc.). Callers
	// should fall back to a warning rather than blocking the operation.
	ErrCodeVerificationUnavailable = "VerificationUnavailable"
)

// RegionVerifier checks that a region ID is recognized by the Alibaba Cloud
// Location service. Build one with NewRegionVerifier; inject a fake processor
// via newRegionVerifierWithProcessor in tests.
type RegionVerifier struct {
	executor openAPIExecutor
}

// NewRegionVerifier constructs a RegionVerifier using the credentials from the
// resolved profile. Returns a typed VerificationUnavailable error when no
// credentials are available — the CLI should treat that as a soft skip rather
// than a hard failure during initial setup.
func NewRegionVerifier(profileName, configPath string, getenv func(string) string) (*RegionVerifier, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	profile, _, err := ecconfig.EffectiveProfile(profileName, configPath, ecconfig.AliyunConfigPath(getenv))
	if err != nil {
		return nil, ecerrors.Client(ErrCodeVerificationUnavailable, err.Error())
	}
	openAPIProfile := toOpenAPIProfile(profile, "", getenv)
	if openAPIProfile.AccessKeyID == "" || openAPIProfile.AccessKeySecret == "" {
		return nil, ecerrors.Client(ErrCodeVerificationUnavailable, "Alibaba Cloud credentials are required to verify the region; configure access keys first or pass --no-verify")
	}
	// Location is a global service. Pin a known-good RegionId for signing even
	// when the user-supplied region is invalid.
	openAPIProfile.RegionID = firstNonEmptyString(openAPIProfile.RegionID, locationDefaultSignRegion)
	executor, err := newDarabonbaExecutor(openAPIProfile)
	if err != nil {
		return nil, ecerrors.Client(ErrCodeVerificationUnavailable, callerSanitizeCloudError(err))
	}
	return &RegionVerifier{executor: executor}, nil
}

// newRegionVerifierWithExecutor lets tests substitute the OpenAPI executor with a fake.
func newRegionVerifierWithExecutor(executor openAPIExecutor) *RegionVerifier {
	return &RegionVerifier{executor: executor}
}

// VerifyRegion is a convenience wrapper that constructs a RegionVerifier and
// validates the region in one step.
func VerifyRegion(profileName, configPath, region, serviceCode string, getenv func(string) string) error {
	verifier, err := NewRegionVerifier(profileName, configPath, getenv)
	if err != nil {
		return err
	}
	return verifier.Verify(region, serviceCode)
}

// Verify calls Location/DescribeEndpoints and reports whether the region is
// known to Alibaba Cloud. serviceCode defaults to "ecs" — the universal probe
// because every region with any public API exposes ECS endpoints.
func (v *RegionVerifier) Verify(region, serviceCode string) error {
	if v == nil || v.executor == nil {
		return ecerrors.Client(ErrCodeVerificationUnavailable, "region verifier is not initialised")
	}
	if region == "" {
		return ecerrors.Client("MissingRegion", "region is required")
	}
	if serviceCode == "" {
		serviceCode = locationDefaultServiceCode
	}
	req := newOpenAPIRequest()
	req.Domain = locationDomain
	req.Product = locationProduct
	req.Version = locationVersion
	req.ApiName = locationAPIName
	req.Method = "GET"
	req.Scheme = "https"
	req.RegionId = locationDefaultSignRegion
	req.QueryParams["Id"] = region
	req.QueryParams["ServiceCode"] = serviceCode
	req.QueryParams["Type"] = "openAPI"
	decoded, err := v.executor.ExecuteOpenAPI(nil, req)
	if err != nil {
		if isInvalidRegionCloudError(err) {
			return v.invalidRegionError(region, serviceCode)
		}
		return ecerrors.Client(ErrCodeVerificationUnavailable, callerSanitizeCloudError(err))
	}
	if locationResponseHasEndpoints(decoded, region) {
		return nil
	}
	return v.invalidRegionError(region, serviceCode)
}

// invalidRegionError builds the typed InvalidRegion error and, on a best-effort
// basis, attaches a Suggestion field listing the closest known region plus the
// full set of valid regions. Failure to enumerate regions degrades silently —
// the user still gets a precise InvalidRegion error, just without the list.
func (v *RegionVerifier) invalidRegionError(region, serviceCode string) *ecerrors.AppError {
	options := []ecerrors.Option{}
	if suggestion := v.regionSuggestion(region); suggestion != "" {
		options = append(options, ecerrors.WithSuggestion(suggestion))
	}
	return ecerrors.Client(ErrCodeInvalidRegion, locationInvalidRegionMessage(region, serviceCode), options...)
}

func (v *RegionVerifier) regionSuggestion(region string) string {
	candidates, err := v.knownRegionIDs()
	if err != nil || len(candidates) == 0 {
		return ""
	}
	return buildRegionSuggestion(region, candidates)
}

// knownRegionIDs calls ECS/DescribeRegions to enumerate the regions reachable
// for this account. It uses a known ECS endpoint and the verifier's OpenAPI
// executor.
func (v *RegionVerifier) knownRegionIDs() ([]string, error) {
	if v == nil || v.executor == nil {
		return nil, ecerrors.Client(ErrCodeVerificationUnavailable, "region verifier is not initialised")
	}
	req := newOpenAPIRequest()
	req.Product = "Ecs"
	req.Version = "2014-05-26"
	req.ApiName = "DescribeRegions"
	req.RegionId = locationDefaultSignRegion
	req.Method = "GET"
	req.Scheme = "https"
	req.Domain = "ecs." + locationDefaultSignRegion + ".aliyuncs.com"
	decoded, err := v.executor.ExecuteOpenAPI(nil, req)
	if err != nil {
		return nil, err
	}
	return parseRegionIDsFromDescribeRegions(decoded), nil
}

func parseRegionIDsFromDescribeRegions(decoded map[string]any) []string {
	if decoded == nil {
		return nil
	}
	wrapper, _ := decoded["Regions"].(map[string]any)
	if wrapper == nil {
		return nil
	}
	entries, _ := wrapper["Region"].([]any)
	out := make([]string, 0, len(entries))
	seen := map[string]bool{}
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		id, _ := entry["RegionId"].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// buildRegionSuggestion returns a one-line suggestion that names the closest
// known region (when typo-close) and lists every valid region. The fuzzy
// threshold is scaled so short typos like "cn-beijinx" → "cn-beijing" (dist 1)
// suggest, and longer typos like "cn-hangzh111" → "cn-hangzhou" (dist 3)
// still suggest, while a totally different string lists candidates without a
// misleading "did you mean".
func buildRegionSuggestion(input string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	closest, distance := closestRegion(input, candidates)
	var b strings.Builder
	if closest != "" && distance <= regionFuzzyThreshold(input, closest) {
		b.WriteString("did you mean ")
		b.WriteString(closest)
		b.WriteString("? ")
	}
	b.WriteString("valid regions: ")
	b.WriteString(strings.Join(candidates, ", "))
	return b.String()
}

// regionFuzzyThreshold scales tolerated edit distance with input length so a
// short typo still suggests, but two unrelated names don't.
func regionFuzzyThreshold(input, closest string) int {
	longest := len(input)
	if len(closest) > longest {
		longest = len(closest)
	}
	switch {
	case longest <= 6:
		return 1
	case longest <= 10:
		return 2
	case longest <= 14:
		return 4
	default:
		return 5
	}
}

func closestRegion(input string, candidates []string) (string, int) {
	best := ""
	bestDistance := -1
	for _, candidate := range candidates {
		d := levenshteinDistance(strings.ToLower(input), strings.ToLower(candidate))
		if bestDistance < 0 || d < bestDistance {
			bestDistance = d
			best = candidate
		}
	}
	return best, bestDistance
}

// levenshteinDistance is a straightforward DP — small inputs, no need for
// fancier algorithms.
func levenshteinDistance(a, b string) int {
	if a == b {
		return 0
	}
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			curr[j] = ins
			if del < curr[j] {
				curr[j] = del
			}
			if sub < curr[j] {
				curr[j] = sub
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func locationInvalidRegionMessage(region, serviceCode string) string {
	return "region " + region + " is not recognised by Alibaba Cloud Location service (service code " + serviceCode + ")"
}

func locationResponseHasEndpoints(decoded map[string]any, region string) bool {
	entries := locationEndpointEntries(decoded)
	if len(entries) == 0 {
		return false
	}
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		if rid, _ := entry["RegionId"].(string); rid != "" && strings.EqualFold(rid, region) {
			return true
		}
		if rid, _ := entry["Id"].(string); rid != "" && strings.EqualFold(rid, region) {
			return true
		}
	}
	// Some responses omit the per-entry RegionId but still indicate the region
	// was matched (entries are non-empty only when DescribeEndpoints returns at
	// least one result for the supplied Id).
	return true
}

func locationEndpointEntries(decoded map[string]any) []any {
	if decoded == nil {
		return nil
	}
	if wrapper, ok := decoded["Endpoints"].(map[string]any); ok {
		if entries, ok := wrapper["Endpoint"].([]any); ok {
			return entries
		}
	}
	if entries, ok := decoded["Endpoints"].([]any); ok {
		return entries
	}
	return nil
}

func isInvalidRegionCloudError(err error) bool {
	if err == nil {
		return false
	}
	code, _, _ := ecerrors.ParseCloudError(err.Error())
	if code == "" {
		return false
	}
	// Location service uses several variants:
	//   InvalidRegionId, InvalidRegionId.NotFound, InvalidRegion.NotFound,
	//   InvalidParameter.RegionId, Region.NotFound
	// Anything that starts with "InvalidRegion" or matches the Region.NotFound
	// shape is a definitive "this region does not exist" signal.
	if strings.HasPrefix(code, "InvalidRegion") {
		return true
	}
	switch code {
	case "InvalidParameter.RegionId", "Region.NotFound":
		return true
	}
	return false
}
