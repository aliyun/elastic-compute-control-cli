package aliyun

import (
	"errors"
	"os"
	"strings"
	"testing"

	ecerrors "ecctl/pkg/errors"
)

func TestRegionVerifierAcceptsKnownRegion(t *testing.T) {
	processor := &fakeOpenAPIExecutor{response: `{"Endpoints":{"Endpoint":[{"RegionId":"cn-hangzhou","Endpoint":"ecs.cn-hangzhou.aliyuncs.com","Type":"openAPI","SerivceCode":"ecs"}]},"RequestId":"req-1"}`}
	verifier := newRegionVerifierWithExecutor(processor)
	if err := verifier.Verify("cn-hangzhou", ""); err != nil {
		t.Fatalf("Verify(cn-hangzhou) = %v, want nil", err)
	}
	if len(processor.requests) != 1 {
		t.Fatalf("expected exactly one OpenAPI call, got %d", len(processor.requests))
	}
	requireLocationRequestShape(t, processor.requests[0], "cn-hangzhou", "ecs")
}

func TestRegionVerifierDefaultsServiceCodeToECS(t *testing.T) {
	processor := &fakeOpenAPIExecutor{response: `{"Endpoints":{"Endpoint":[{"RegionId":"cn-hangzhou"}]}}`}
	verifier := newRegionVerifierWithExecutor(processor)
	if err := verifier.Verify("cn-hangzhou", ""); err != nil {
		t.Fatalf("Verify default service code = %v", err)
	}
	if processor.requests[0].QueryParams["ServiceCode"] != "ecs" {
		t.Fatalf("ServiceCode default = %q, want ecs", processor.requests[0].QueryParams["ServiceCode"])
	}
}

func TestRegionVerifierRespectsExplicitServiceCode(t *testing.T) {
	processor := &fakeOpenAPIExecutor{response: `{"Endpoints":{"Endpoint":[{"RegionId":"cn-hangzhou"}]}}`}
	verifier := newRegionVerifierWithExecutor(processor)
	if err := verifier.Verify("cn-hangzhou", "ack"); err != nil {
		t.Fatalf("Verify ack = %v", err)
	}
	if processor.requests[0].QueryParams["ServiceCode"] != "ack" {
		t.Fatalf("ServiceCode override = %q, want ack", processor.requests[0].QueryParams["ServiceCode"])
	}
}

func TestRegionVerifierRejectsEmptyEndpoints(t *testing.T) {
	processor := &fakeOpenAPIExecutor{response: `{"Endpoints":{"Endpoint":[]}}`}
	verifier := newRegionVerifierWithExecutor(processor)
	err := verifier.Verify("cn-hangzho", "")
	if err == nil {
		t.Fatalf("Verify cn-hangzho should fail")
	}
	requireAppErrorCode(t, err, ErrCodeInvalidRegion)
}

func TestRegionVerifierMapsInvalidRegionCloudError(t *testing.T) {
	processor := &fakeOpenAPIExecutor{callError: errors.New("SDK.ServerError\nErrorCode: InvalidRegionId.NotFound\nMessage: The specified Region does not exist.")}
	verifier := newRegionVerifierWithExecutor(processor)
	err := verifier.Verify("cn-hangzho", "")
	if err == nil {
		t.Fatalf("Verify should fail on InvalidRegionId.NotFound")
	}
	requireAppErrorCode(t, err, ErrCodeInvalidRegion)
}

func TestRegionVerifierMapsNetworkErrorToUnavailable(t *testing.T) {
	processor := &fakeOpenAPIExecutor{callError: errors.New("dial tcp: lookup location-readonly.aliyuncs.com: no such host")}
	verifier := newRegionVerifierWithExecutor(processor)
	err := verifier.Verify("cn-hangzhou", "")
	if err == nil {
		t.Fatalf("Verify should report unavailable on network failure")
	}
	requireAppErrorCode(t, err, ErrCodeVerificationUnavailable)
}

func TestRegionVerifierRequiresRegion(t *testing.T) {
	verifier := newRegionVerifierWithExecutor(&fakeOpenAPIExecutor{})
	err := verifier.Verify("", "")
	if err == nil {
		t.Fatalf("Verify empty region should fail")
	}
	requireAppErrorCode(t, err, "MissingRegion")
}

func TestRegionVerifierUninitialisedReturnsUnavailable(t *testing.T) {
	var verifier *RegionVerifier
	err := verifier.Verify("cn-hangzhou", "")
	if err == nil {
		t.Fatalf("nil verifier Verify should fail")
	}
	requireAppErrorCode(t, err, ErrCodeVerificationUnavailable)
}

func TestVerifyRegionFailsWhenNoCredentials(t *testing.T) {
	t.Setenv("ALIBABA_CLOUD_ACCESS_KEY_ID", "")
	t.Setenv("ALIBABACLOUD_ACCESS_KEY_ID", "")
	t.Setenv("ALICLOUD_ACCESS_KEY_ID", "")
	t.Setenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "")
	t.Setenv("ALIBABACLOUD_ACCESS_KEY_SECRET", "")
	t.Setenv("ALICLOUD_ACCESS_KEY_SECRET", "")
	t.Setenv("ECCTL_CONFIG_PATH", "/non-existent-config-for-region-verify-test")
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", "/non-existent-aliyun-config-for-region-verify-test")
	err := VerifyRegion("", "/non-existent-config-for-region-verify-test", "cn-hangzhou", "", os.Getenv)
	if err == nil {
		t.Fatalf("VerifyRegion without credentials should fail")
	}
	requireAppErrorCode(t, err, ErrCodeVerificationUnavailable)
}

func requireLocationRequestShape(t *testing.T, req *openAPIRequest, region, serviceCode string) {
	t.Helper()
	if req == nil {
		t.Fatalf("request is nil")
	}
	if req.Domain != locationDomain {
		t.Fatalf("Domain = %q, want %q", req.Domain, locationDomain)
	}
	if req.Product != locationProduct {
		t.Fatalf("Product = %q, want %q", req.Product, locationProduct)
	}
	if req.Version != locationVersion {
		t.Fatalf("Version = %q, want %q", req.Version, locationVersion)
	}
	if req.ApiName != locationAPIName {
		t.Fatalf("ApiName = %q, want %q", req.ApiName, locationAPIName)
	}
	if req.QueryParams["Id"] != region {
		t.Fatalf("QueryParams.Id = %q, want %q", req.QueryParams["Id"], region)
	}
	if req.QueryParams["ServiceCode"] != serviceCode {
		t.Fatalf("QueryParams.ServiceCode = %q, want %q", req.QueryParams["ServiceCode"], serviceCode)
	}
	if req.QueryParams["Type"] != "openAPI" {
		t.Fatalf("QueryParams.Type = %q, want openAPI", req.QueryParams["Type"])
	}
}

func requireAppErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := err.(*ecerrors.AppError)
	if !ok {
		t.Fatalf("error is not *AppError: %v", err)
	}
	if got := appErr.Payload().Code; got != want {
		t.Fatalf("error code = %q, want %q (message=%q)", got, want, appErr.Payload().Message)
	}
}

func TestLocationResponseHasEndpointsMatchesRegionId(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		region  string
		want    bool
	}{
		{name: "wrapped Endpoint array", payload: map[string]any{"Endpoints": map[string]any{"Endpoint": []any{map[string]any{"RegionId": "cn-hangzhou"}}}}, region: "cn-hangzhou", want: true},
		{name: "wrapped via Id field", payload: map[string]any{"Endpoints": map[string]any{"Endpoint": []any{map[string]any{"Id": "cn-hangzhou"}}}}, region: "cn-hangzhou", want: true},
		{name: "case insensitive", payload: map[string]any{"Endpoints": map[string]any{"Endpoint": []any{map[string]any{"RegionId": "CN-HANGZHOU"}}}}, region: "cn-hangzhou", want: true},
		{name: "empty wrapper", payload: map[string]any{"Endpoints": map[string]any{"Endpoint": []any{}}}, region: "cn-hangzhou", want: false},
		{name: "flat array", payload: map[string]any{"Endpoints": []any{map[string]any{"RegionId": "cn-hangzhou"}}}, region: "cn-hangzhou", want: true},
		{name: "missing wrapper", payload: map[string]any{"RequestId": "req"}, region: "cn-hangzhou", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := locationResponseHasEndpoints(tc.payload, tc.region); got != tc.want {
				t.Fatalf("locationResponseHasEndpoints = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsInvalidRegionCloudError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"InvalidRegionId (bare, Location 2026)", errors.New("ErrorCode: InvalidRegionId\nMessage: The specified region does not exist."), true},
		{"InvalidRegionId.NotFound", errors.New("ErrorCode: InvalidRegionId.NotFound\nMessage: foo"), true},
		{"InvalidRegion.NotFound", errors.New("ErrorCode: InvalidRegion.NotFound\nMessage: foo"), true},
		{"InvalidParameter.RegionId", errors.New("ErrorCode: InvalidParameter.RegionId\nMessage: foo"), true},
		{"Region.NotFound", errors.New("ErrorCode: Region.NotFound\nMessage: foo"), true},
		{"unrelated", errors.New("ErrorCode: Throttling\nMessage: rate"), false},
		{"plain text", errors.New("dial tcp: i/o timeout"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isInvalidRegionCloudError(tc.err); got != tc.want {
				t.Fatalf("isInvalidRegionCloudError = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInvalidRegionMessageMentionsServiceCode(t *testing.T) {
	msg := locationInvalidRegionMessage("cn-hangzho", "ecs")
	if !strings.Contains(msg, "cn-hangzho") || !strings.Contains(msg, "ecs") {
		t.Fatalf("locationInvalidRegionMessage = %q", msg)
	}
}

func TestRegionVerifierAttachesValidRegionsSuggestion(t *testing.T) {
	processor := &fakeOpenAPIExecutor{responses: []string{
		`{"Endpoints":{"Endpoint":[]}}`,
		`{"Regions":{"Region":[{"RegionId":"cn-hangzhou"},{"RegionId":"cn-beijing"},{"RegionId":"cn-shanghai"}]},"RequestId":"req-regions"}`,
	}}
	verifier := newRegionVerifierWithExecutor(processor)
	err := verifier.Verify("cn-hangzh111", "")
	if err == nil {
		t.Fatalf("Verify cn-hangzh111 should fail")
	}
	requireAppErrorCode(t, err, ErrCodeInvalidRegion)
	suggestion := err.(*ecerrors.AppError).Payload().Suggestion
	if !strings.Contains(suggestion, "cn-hangzhou") || !strings.Contains(suggestion, "cn-beijing") {
		t.Fatalf("suggestion must include candidate list: %q", suggestion)
	}
	if !strings.Contains(suggestion, "did you mean cn-hangzhou") {
		t.Fatalf("suggestion must propose closest match: %q", suggestion)
	}
	// Two OpenAPI calls: DescribeEndpoints + DescribeRegions.
	if len(processor.requests) != 2 {
		t.Fatalf("expected 2 OpenAPI calls (DescribeEndpoints+DescribeRegions), got %d", len(processor.requests))
	}
	if processor.requests[1].ApiName != "DescribeRegions" || processor.requests[1].Product != "Ecs" {
		t.Fatalf("second call should be Ecs/DescribeRegions: %#v", processor.requests[1])
	}
}

func TestRegionVerifierSuggestionAlsoOnInvalidRegionCloudError(t *testing.T) {
	// DescribeEndpoints returns a cloud error; DescribeRegions still succeeds —
	// the suggestion should still appear.
	processor := &fakeOpenAPIExecutor{
		callErrors: []error{
			errors.New("ErrorCode: InvalidRegionId\nMessage: The specified region does not exist."),
			nil,
		},
		responses: []string{
			"",
			`{"Regions":{"Region":[{"RegionId":"cn-hangzhou"},{"RegionId":"cn-beijing"}]}}`,
		},
	}
	verifier := newRegionVerifierWithExecutor(processor)
	err := verifier.Verify("cn-hangzh222", "")
	if err == nil {
		t.Fatalf("Verify should fail on InvalidRegionId")
	}
	requireAppErrorCode(t, err, ErrCodeInvalidRegion)
	suggestion := err.(*ecerrors.AppError).Payload().Suggestion
	if !strings.Contains(suggestion, "cn-hangzhou") {
		t.Fatalf("suggestion missing cn-hangzhou: %q", suggestion)
	}
}

func TestRegionVerifierSuggestionDegradesWhenListingFails(t *testing.T) {
	processor := &fakeOpenAPIExecutor{
		responses:  []string{`{"Endpoints":{"Endpoint":[]}}`},
		callErrors: []error{nil, errors.New("dial tcp: i/o timeout")},
	}
	verifier := newRegionVerifierWithExecutor(processor)
	err := verifier.Verify("cn-hangzh111", "")
	if err == nil {
		t.Fatalf("Verify should fail")
	}
	requireAppErrorCode(t, err, ErrCodeInvalidRegion)
	if suggestion := err.(*ecerrors.AppError).Payload().Suggestion; suggestion != "" {
		t.Fatalf("suggestion should be empty when listing fails, got %q", suggestion)
	}
}

func TestBuildRegionSuggestionDoesNotProposeFarMatches(t *testing.T) {
	suggestion := buildRegionSuggestion("totally-bogus", []string{"cn-hangzhou", "cn-beijing"})
	if strings.Contains(suggestion, "did you mean") {
		t.Fatalf("suggestion should not propose for far match: %q", suggestion)
	}
	if !strings.Contains(suggestion, "valid regions") {
		t.Fatalf("suggestion must still list candidates: %q", suggestion)
	}
}

func TestBuildRegionSuggestionEmptyCandidatesYieldsEmpty(t *testing.T) {
	if got := buildRegionSuggestion("cn-hangzh111", nil); got != "" {
		t.Fatalf("buildRegionSuggestion empty candidates = %q, want empty", got)
	}
}

func TestLevenshteinDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "ab", 2},
		{"cn-hangzhou", "cn-hangzhou", 0},
		{"cn-hangzho", "cn-hangzhou", 1},
		{"cn-beijinx", "cn-beijing", 1},
		{"cn-hangzh222", "cn-hangzhou", 3},
		{"totally-bogus", "cn-hangzhou", 11},
	}
	for _, tc := range cases {
		if got := levenshteinDistance(tc.a, tc.b); got != tc.want {
			t.Fatalf("levenshteinDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParseRegionIDsFromDescribeRegions(t *testing.T) {
	decoded := map[string]any{
		"Regions": map[string]any{
			"Region": []any{
				map[string]any{"RegionId": "cn-hangzhou"},
				map[string]any{"RegionId": "cn-beijing"},
				map[string]any{"RegionId": "cn-hangzhou"}, // duplicate
				map[string]any{"RegionId": ""},
				map[string]any{"Other": "ignored"},
			},
		},
	}
	got := parseRegionIDsFromDescribeRegions(decoded)
	if len(got) != 2 || got[0] != "cn-beijing" || got[1] != "cn-hangzhou" {
		t.Fatalf("parseRegionIDsFromDescribeRegions = %#v, want sorted unique [cn-beijing cn-hangzhou]", got)
	}
}
