package aliyun

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

const fixtureReadTestBucket = "ecctl-e2e-20261109t190000-abcdef123456"

type fixtureReadRoundTripper func(*http.Request) (*http.Response, error)

func (f fixtureReadRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func fixtureReadResponse(status int, requestID, body string) *http.Response {
	header := make(http.Header)
	if requestID != "" {
		header.Set("X-Oss-Request-Id", requestID)
	}
	header.Set("Content-Type", "application/xml")
	certificate := &x509.Certificate{DNSNames: []string{fixtureReadTestBucket + ".oss-cn-hangzhou.aliyuncs.com"}}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		TLS:        &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{certificate}}, PeerCertificates: []*x509.Certificate{certificate}, ServerName: fixtureReadTestBucket + ".oss-cn-hangzhou.aliyuncs.com"},
	}
}

func fixtureReadEmptyXML() string {
	return "<ListBucketResult><Name>" + fixtureReadTestBucket + "</Name><Prefix></Prefix><MaxKeys>1</MaxKeys><IsTruncated>false</IsTruncated><KeyCount>0</KeyCount></ListBucketResult>"
}

func fixtureReadMissingXML(requestID, code string) string {
	return "<Error><Code>" + code + "</Code><Message>The specified bucket does not exist.</Message><RequestId>" + requestID + "</RequestId><BucketName>" + fixtureReadTestBucket + "</BucketName></Error>"
}

func pinnedFixtureReadCaller(t *testing.T, transport http.RoundTripper) *OSSUtilCaller {
	t.Helper()
	identity, err := canonicalIdentity(map[string]any{"AccountId": "1234567890123456", "IdentityType": "RAMUser", "UserId": "fixture-user"})
	if err != nil {
		t.Fatal(err)
	}
	old := resolveCredentialSnapshotIdentity
	t.Cleanup(func() { resolveCredentialSnapshotIdentity = old })
	resolveCredentialSnapshotIdentity = func(context.Context, resolvedOpenAPIProfile, *credentialSnapshot, string) (telemetry.Identity, error) {
		return identity, nil
	}
	return &OSSUtilCaller{
		Region:           "cn-hangzhou",
		requestedProfile: "fixture-profile",
		Profile: resolvedOpenAPIProfile{
			Name: "fixture-profile", ProfileBound: true, Mode: credentialModeAK,
			RegionID: "cn-hangzhou", RegionSource: ecconfig.RegionSourceExplicit,
			Acquirer:          &staticCredentialAcquirer{snapshot: credentialSnapshot{AccessKeyID: "test-ak", AccessKeySecret: "test-secret"}},
			ExpectedAccountID: "1234567890123456", ExpectedIdentityType: "RAMUser", ExpectedIdentityHash: identity.Hash,
			PinCredentialIdentity: true,
		},
		runner:               &fakeOSSUtilRunner{},
		getenv:               func(string) string { return "" },
		resolveRuntime:       func(func(string) string) (string, error) { return "", errors.New("ossutil must not be probed") },
		fixtureReadTransport: transport,
	}
}

func TestOSSFixtureReadSelectorRequiresExactPinnedRequest(t *testing.T) {
	caller := pinnedFixtureReadCaller(t, nil)
	request := map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}
	if bucket, ok := caller.ossFixtureReadBucket("ListObjects", request, nil); !ok || bucket != fixtureReadTestBucket {
		t.Fatalf("exact pinned request was not selected: %q, %v", bucket, ok)
	}
	for _, maxKeys := range []any{1, json.Number("1")} {
		if bucket, ok := caller.ossFixtureReadBucket("ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": maxKeys}, nil); !ok || bucket != fixtureReadTestBucket {
			t.Fatalf("exact integer max-keys was not selected: %T %#v", maxKeys, maxKeys)
		}
	}
	for _, tc := range []struct {
		name        string
		operation   string
		request     map[string]any
		passthrough []string
		change      func(*OSSUtilCaller)
	}{
		{name: "different operation", operation: "GetBucketInfo", request: request},
		{name: "different bucket", operation: "ListObjects", request: map[string]any{"Bucket": "ordinary", "MaxKeys": float64(1)}},
		{name: "string max keys", operation: "ListObjects", request: map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": "1"}},
		{name: "noncanonical number", operation: "ListObjects", request: map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": json.Number("1.0")}},
		{name: "extra parameter", operation: "ListObjects", request: map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1), "Prefix": ""}},
		{name: "passthrough", operation: "ListObjects", request: request, passthrough: []string{"--profile", "other"}},
		{name: "wrong region", operation: "ListObjects", request: request, change: func(c *OSSUtilCaller) { c.Region = "cn-shanghai" }},
		{name: "implicit region", operation: "ListObjects", request: request, change: func(c *OSSUtilCaller) { c.Profile.RegionSource = ecconfig.RegionSourceProfile }},
		{name: "unbound profile", operation: "ListObjects", request: request, change: func(c *OSSUtilCaller) { c.Profile.ProfileBound = false }},
		{name: "unselected profile", operation: "ListObjects", request: request, change: func(c *OSSUtilCaller) { c.requestedProfile = "" }},
		{name: "missing pin", operation: "ListObjects", request: request, change: func(c *OSSUtilCaller) { c.Profile.ExpectedIdentityHash = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := *caller
			if tc.change != nil {
				tc.change(&copy)
			}
			if bucket, ok := copy.ossFixtureReadBucket(tc.operation, tc.request, tc.passthrough); ok {
				t.Fatalf("unscoped request selected: %q", bucket)
			}
		})
	}
}

func TestOSSFixtureReadValidatesSingleObjectPagination(t *testing.T) {
	base := "<ListBucketResult><Name>" + fixtureReadTestBucket + "</Name><Prefix></Prefix><MaxKeys>1</MaxKeys>" +
		"<EncodingType>url</EncodingType><IsTruncated>true</IsTruncated><KeyCount>1</KeyCount>" +
		"<NextContinuationToken>cursor-2</NextContinuationToken><Contents><Key>object%20one</Key><Size>1</Size></Contents></ListBucketResult>"
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"one object and continuation", base, true},
		{"wrong max keys", strings.Replace(base, "<MaxKeys>1</MaxKeys>", "<MaxKeys>2</MaxKeys>", 1), false},
		{"wrong encoding", strings.Replace(base, "<EncodingType>url</EncodingType>", "<EncodingType>xml</EncodingType>", 1), false},
		{"duplicate continuation", strings.Replace(base, "</NextContinuationToken>", "</NextContinuationToken><NextContinuationToken>cursor-3</NextContinuationToken>", 1), false},
		{"noncanonical count", strings.Replace(base, "<KeyCount>1</KeyCount>", "<KeyCount>01</KeyCount>", 1), false},
		{"count exceeds limit", strings.Replace(base, "<KeyCount>1</KeyCount>", "<KeyCount>2</KeyCount>", 1), false},
		{"missing key", strings.Replace(base, "<Key>object%20one</Key>", "", 1), false},
		{"empty key", strings.Replace(base, "<Key>object%20one</Key>", "<Key></Key>", 1), false},
		{"invalid truncated flag", strings.Replace(base, "<IsTruncated>true</IsTruncated>", "<IsTruncated>maybe</IsTruncated>", 1), false},
		{"unexpected start after", strings.Replace(base, "<Prefix></Prefix>", "<Prefix></Prefix><StartAfter>hidden</StartAfter>", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
				requests++
				return fixtureReadResponse(200, "req-page", tc.body), nil
			}))
			response, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": 1}, nil)
			if requests != 1 || len(caller.runner.(*fakeOSSUtilRunner).calls) != 0 {
				t.Fatalf("page did not use one pinned SDK request: requests=%d", requests)
			}
			if !tc.valid {
				if err == nil || response != nil || ecerrors.HasVerifiedProviderResponse(err) {
					t.Fatalf("inconsistent page proved a result: %#v %v", response, err)
				}
				return
			}
			contents, ok := response["Contents"].([]map[string]any)
			if err != nil || !ok || len(contents) != 1 || contents[0]["Key"] != "object one" || response["Name"] != fixtureReadTestBucket ||
				response["KeyCount"] != 1 || response["IsTruncated"] != true || response["NextContinuationToken"] != "cursor-2" || response["EncodingType"] != "url" {
				t.Fatalf("signed one-object page changed: %#v %v", response, err)
			}
		})
	}
}

func TestOSSFixturePutBucketRequiresTrustedReceipt(t *testing.T) {
	const serverDate = "Mon, 16 Nov 2026 13:00:00 GMT"
	for _, tc := range []struct {
		name, body, date string
		status           int
		followup         bool
	}{
		{"missing date", "", "", 200, false},
		{"invalid date", "", "not-an-http-date", 200, false},
		{"unexpected response body", "<Bucket/>", serverDate, 200, false},
		{"wrong success status", "", serverDate, 204, false},
		{"foreign bucket after PUT", "", serverDate, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(request *http.Request) (*http.Response, error) {
				requests++
				if request.URL.Query().Has("bucketInfo") {
					body := "<BucketInfo><Bucket><Name>" + fixtureReadTestBucket + "</Name><Location>oss-cn-hangzhou</Location>" +
						"<ExtranetEndpoint>oss-cn-hangzhou.aliyuncs.com</ExtranetEndpoint><CreationDate>2026-11-16T13:00:00Z</CreationDate>" +
						"<Owner><ID>another-account</ID></Owner></Bucket></BucketInfo>"
					return fixtureReadResponse(200, "req-info", body), nil
				}
				if request.Method != http.MethodPut {
					t.Fatalf("unexpected mutation: %s", request.Method)
				}
				response := fixtureReadResponse(tc.status, "req-put", tc.body)
				if tc.date != "" {
					response.Header.Set("Date", tc.date)
				}
				return response, nil
			}))
			response, err := caller.CallWithArgs(context.Background(), "PutBucket", map[string]any{"Bucket": fixtureReadTestBucket, "ACL": "private"}, nil)
			wantRequests := 1
			if tc.followup {
				wantRequests = 2
			}
			if err == nil || response != nil || requests != wantRequests || len(caller.runner.(*fakeOSSUtilRunner).calls) != 0 {
				t.Fatalf("unverified PUT yielded an owned bucket: response=%#v err=%v requests=%d", response, err, requests)
			}
		})
	}
}

func TestOSSFixtureReadUsesSignedV2RequestAndReturnsExplicitZero(t *testing.T) {
	requests := 0
	transport := fixtureReadRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Method != "GET" || request.URL.Scheme != "https" || request.URL.Host != fixtureReadTestBucket+".oss-cn-hangzhou.aliyuncs.com" || request.URL.Path != "/" || request.URL.Query().Get("list-type") != "2" || request.URL.Query().Get("encoding-type") != "url" || request.URL.Query().Get("max-keys") != "1" || request.Header.Get("Authorization") == "" {
			t.Fatalf("unexpected signed V2 request: %s %s", request.Method, request.URL.String())
		}
		return fixtureReadResponse(200, "req-empty", fixtureReadEmptyXML()), nil
	})
	caller := pinnedFixtureReadCaller(t, transport)
	result, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil)
	if err != nil || requests != 1 || len(caller.runner.(*fakeOSSUtilRunner).calls) != 0 {
		t.Fatalf("typed read failed or used ossutil: result=%#v err=%v requests=%d", result, err, requests)
	}
	if result["Name"] != fixtureReadTestBucket || result["KeyCount"] != 0 || result["IsTruncated"] != false || result["Contents"] != nil {
		t.Fatalf("empty response changed: %#v", result)
	}
}

func TestOSSFixtureBucketLifecycleUsesPinnedSDK(t *testing.T) {
	const serverDate = "Mon, 16 Nov 2026 13:00:00 GMT"
	info := "<BucketInfo><Bucket><Name>" + fixtureReadTestBucket + "</Name><Location>oss-cn-hangzhou</Location>" +
		"<ExtranetEndpoint>oss-cn-hangzhou.aliyuncs.com</ExtranetEndpoint><CreationDate>2026-11-16T13:00:00Z</CreationDate>" +
		"<Owner><ID>1234567890123456</ID></Owner></Bucket></BucketInfo>"
	operations := []string{}
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(request *http.Request) (*http.Response, error) {
		operation := request.Method
		if request.URL.Query().Has("bucketInfo") {
			operation = "GetBucketInfo"
		}
		operations = append(operations, operation)
		var response *http.Response
		switch operation {
		case "PUT":
			response = fixtureReadResponse(200, "req-put", "")
		case "GetBucketInfo":
			response = fixtureReadResponse(200, "req-info", info)
		case "DELETE":
			response = fixtureReadResponse(204, "req-delete", "")
		default:
			t.Fatalf("unexpected OSS fixture operation: %s", request.URL)
		}
		response.Header.Set("Date", serverDate)
		return response, nil
	}))
	created, err := caller.CallWithArgs(context.Background(), "PutBucket", map[string]any{"Bucket": fixtureReadTestBucket, "ACL": "private"}, nil)
	if err != nil || created["RequestId"] != "req-put" || created["ServerDate"] != serverDate {
		t.Fatalf("trusted PutBucket receipt missing: %#v %v", created, err)
	}
	bucket, ok := created["Bucket"].(map[string]any)
	if !ok || bucket["CreationDate"] != "2026-11-16T13:00:00Z" {
		t.Fatalf("PutBucket did not bind its first trusted instance: %#v", created)
	}
	read, err := caller.CallWithArgs(context.Background(), "GetBucketInfo", map[string]any{"Bucket": fixtureReadTestBucket}, nil)
	if err != nil || read["RequestId"] != "req-info" {
		t.Fatalf("trusted GetBucketInfo failed: %#v %v", read, err)
	}
	deleted, err := caller.CallWithArgs(context.Background(), "DeleteBucket", map[string]any{"Bucket": fixtureReadTestBucket}, nil)
	if err != nil || deleted["RequestId"] != "req-delete" || strings.Join(operations, ",") != "PUT,GetBucketInfo,GetBucketInfo,DELETE" || len(caller.runner.(*fakeOSSUtilRunner).calls) != 0 {
		t.Fatalf("fixture escaped signed SDK path: deleted=%#v err=%v operations=%v", deleted, err, operations)
	}
}

func TestOSSFixtureBucketRejectsUntrustedInstanceAndRequest(t *testing.T) {
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
		body := "<BucketInfo><Bucket><Name>" + fixtureReadTestBucket + "</Name><Location>oss-cn-hangzhou</Location>" +
			"<ExtranetEndpoint>oss-cn-hangzhou.aliyuncs.com</ExtranetEndpoint><CreationDate>2026-11-16T13:00:00Z</CreationDate>" +
			"<Owner><ID>another-account</ID></Owner></Bucket></BucketInfo>"
		return fixtureReadResponse(200, "req-info", body), nil
	}))
	if response, err := caller.CallWithArgs(context.Background(), "GetBucketInfo", map[string]any{"Bucket": fixtureReadTestBucket}, nil); err == nil || response != nil {
		t.Fatalf("foreign bucket passed trusted lookup: %#v %v", response, err)
	}
	if response, err := caller.CallWithArgs(context.Background(), "DeleteBucket", map[string]any{"Bucket": fixtureReadTestBucket}, []string{"--profile", "other"}); err == nil || response != nil || len(caller.runner.(*fakeOSSUtilRunner).calls) != 0 {
		t.Fatalf("owned bucket command escaped SDK validation: %#v %v", response, err)
	}
}

func TestOSSFixtureReadVerifiesOnlyExactNoSuchBucket(t *testing.T) {
	for _, tc := range []struct {
		name, requestID, body string
		status                int
		change                func(*http.Response)
		verified              bool
	}{
		{name: "verified", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket"), verified: true},
		{name: "xml prolog", status: 404, requestID: "req-missing", body: "<?xml version=\"1.0\"?>\n" + fixtureReadMissingXML("req-missing", "NoSuchBucket"), verified: true},
		{name: "leading text", status: 404, requestID: "req-missing", body: "prefix" + fixtureReadMissingXML("req-missing", "NoSuchBucket")},
		{name: "trailing whitespace", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket") + " \n\t", verified: true},
		{name: "second error root", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket") + fixtureReadMissingXML("req-missing", "AccessDenied")},
		{name: "trailing garbage", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket") + "garbage"},
		{name: "truncated suffix", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket") + "<partial"},
		{name: "trailing processing instruction", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket") + "<?other value?>"},
		{name: "missing header id", status: 404, body: fixtureReadMissingXML("req-missing", "NoSuchBucket")},
		{name: "wrong body id", status: 404, requestID: "req-other", body: fixtureReadMissingXML("req-missing", "NoSuchBucket")},
		{name: "wrong status", status: 403, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket")},
		{name: "wrong code", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "AccessDenied")},
		{name: "redirect", status: 301, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket"), change: func(r *http.Response) { r.Header.Set("Location", "https://other.example/") }},
		{name: "server failure", status: 500, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket")},
		{name: "wrong bucket", status: 404, requestID: "req-missing", body: strings.Replace(fixtureReadMissingXML("req-missing", "NoSuchBucket"), fixtureReadTestBucket, "another-bucket", 1)},
		{name: "no verified TLS", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket"), change: func(r *http.Response) { r.TLS = nil }},
		{name: "wrong TLS host", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket"), change: func(r *http.Response) { r.TLS.ServerName = "other.example" }},
		{name: "duplicate header id", status: 404, requestID: "req-missing", body: fixtureReadMissingXML("req-missing", "NoSuchBucket"), change: func(r *http.Response) { r.Header.Add("X-Oss-Request-Id", "req-other") }},
		{name: "oversize body", status: 404, requestID: "req-missing", body: strings.Repeat("x", 64*1024+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
				requests++
				response := fixtureReadResponse(tc.status, tc.requestID, tc.body)
				if tc.change != nil {
					tc.change(response)
				}
				return response, nil
			}))
			_, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil)
			var appErr *ecerrors.AppError
			if requests != 1 || !errors.As(err, &appErr) {
				t.Fatalf("read error or attempt count: %v, %d", err, requests)
			}
			verified := appErr.ExitCode() == 4 && ecerrors.HasVerifiedProviderResponse(err) && ecerrors.ActionFromError("ListObjects", err).Code == "NoSuchBucket"
			if verified != tc.verified {
				t.Fatalf("provider proof = %v, want %v: %#v", verified, tc.verified, appErr.Payload())
			}
		})
	}
}

func TestOSSFixtureReadRejectsMissingKeyCountAndAmbiguousEmptyResult(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantError  bool
		wantCount  int
	}{
		{name: "explicit empty", body: fixtureReadEmptyXML(), wantCount: 0},
		{name: "xml prolog", body: "<?xml version=\"1.0\"?>\n" + fixtureReadEmptyXML(), wantCount: 0},
		{name: "leading text", body: "prefix" + fixtureReadEmptyXML(), wantError: true},
		{name: "empty with trailing whitespace", body: fixtureReadEmptyXML() + " \n\t", wantCount: 0},
		{name: "empty then error", body: fixtureReadEmptyXML() + fixtureReadMissingXML("req-list", "AccessDenied"), wantError: true},
		{name: "empty then garbage", body: fixtureReadEmptyXML() + "garbage", wantError: true},
		{name: "empty then truncated suffix", body: fixtureReadEmptyXML() + "<partial", wantError: true},
		{name: "empty then processing instruction", body: fixtureReadEmptyXML() + "<?other value?>", wantError: true},
		{name: "missing key count", body: strings.Replace(fixtureReadEmptyXML(), "<KeyCount>0</KeyCount>", "", 1), wantError: true},
		{name: "duplicate key count", body: strings.Replace(fixtureReadEmptyXML(), "<KeyCount>0</KeyCount>", "<KeyCount>0</KeyCount><KeyCount>0</KeyCount>", 1), wantError: true},
		{name: "missing truncated", body: strings.Replace(fixtureReadEmptyXML(), "<IsTruncated>false</IsTruncated>", "", 1), wantError: true},
		{name: "wrong name", body: strings.Replace(fixtureReadEmptyXML(), fixtureReadTestBucket, "another-bucket", 1), wantError: true},
		{name: "truncated zero", body: strings.Replace(fixtureReadEmptyXML(), "<IsTruncated>false</IsTruncated>", "<IsTruncated>true</IsTruncated>", 1), wantError: true},
		{name: "one object", body: strings.Replace(fixtureReadEmptyXML(), "<KeyCount>0</KeyCount>", "<KeyCount>1</KeyCount><Contents><Key>object</Key><Size>1</Size></Contents>", 1), wantCount: 1},
		{name: "count disagrees", body: strings.Replace(fixtureReadEmptyXML(), "<KeyCount>0</KeyCount>", "<KeyCount>1</KeyCount>", 1), wantError: true},
		{name: "unexpected prefix", body: strings.Replace(fixtureReadEmptyXML(), "<Prefix></Prefix>", "<Prefix>hidden/</Prefix>", 1), wantError: true},
		{name: "unexpected common prefixes", body: strings.Replace(fixtureReadEmptyXML(), "<KeyCount>0</KeyCount>", "<KeyCount>0</KeyCount><CommonPrefixes><Prefix>hidden/</Prefix></CommonPrefixes>", 1), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
				return fixtureReadResponse(200, "req-list", tc.body), nil
			}))
			response, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil)
			if (err != nil) != tc.wantError {
				t.Fatalf("response=%#v error=%v, wantError=%v", response, err, tc.wantError)
			}
			if !tc.wantError && response["KeyCount"] != tc.wantCount {
				t.Fatalf("KeyCount=%#v, want %d", response["KeyCount"], tc.wantCount)
			}
		})
	}
}

func TestOSSFixtureReadDoesNotAcceptEmptySuccessWithoutProviderID(t *testing.T) {
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
		return fixtureReadResponse(200, "", fixtureReadEmptyXML()), nil
	}))
	response, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil)
	if err == nil || response != nil {
		t.Fatalf("success without provider ID proved emptiness: response=%#v err=%v", response, err)
	}
}

func TestOSSFixtureReadRequestValidatorRejectsTargetChanges(t *testing.T) {
	var signed *http.Request
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(request *http.Request) (*http.Response, error) {
		signed = request.Clone(context.Background())
		return fixtureReadResponse(200, "req-list", fixtureReadEmptyXML()), nil
	}))
	if _, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil); err != nil || signed == nil {
		t.Fatalf("SDK did not produce a signed fixture request: %v", err)
	}
	makeRequest := func() *http.Request {
		return signed.Clone(context.Background())
	}
	if !validOSSFixtureSignedRequest(makeRequest(), fixtureReadTestBucket, "test-ak", "") {
		t.Fatal("exact signed request rejected")
	}
	for _, tc := range []struct {
		name   string
		change func(*http.Request)
	}{
		{name: "method", change: func(r *http.Request) { r.Method = "PUT" }},
		{name: "host", change: func(r *http.Request) { r.URL.Host = "other.oss-cn-hangzhou.aliyuncs.com" }},
		{name: "host header", change: func(r *http.Request) { r.Host = "other.oss-cn-hangzhou.aliyuncs.com" }},
		{name: "scheme", change: func(r *http.Request) { r.URL.Scheme = "http" }},
		{name: "port", change: func(r *http.Request) { r.URL.Host += ":443"; r.Host = r.URL.Host }},
		{name: "path", change: func(r *http.Request) { r.URL.Path = "/object" }},
		{name: "raw path", change: func(r *http.Request) { r.URL.RawPath = "/%2F" }},
		{name: "opaque URL", change: func(r *http.Request) { r.URL.Opaque = "//other.example/" }},
		{name: "force query", change: func(r *http.Request) { r.URL.ForceQuery = true }},
		{name: "malformed query", change: func(r *http.Request) { r.URL.RawQuery = "list-type=%ZZ" }},
		{name: "missing encoding", change: func(r *http.Request) { r.URL.RawQuery = "list-type=2&max-keys=1" }},
		{name: "duplicate max keys", change: func(r *http.Request) { r.URL.RawQuery += "&max-keys=1" }},
		{name: "extra query", change: func(r *http.Request) { r.URL.RawQuery += "&prefix=hidden" }},
		{name: "userinfo", change: func(r *http.Request) { r.URL.User = url.User("other") }},
		{name: "fragment", change: func(r *http.Request) { r.URL.Fragment = "other" }},
		{name: "signature missing", change: func(r *http.Request) { r.Header.Del("Authorization") }},
		{name: "duplicate signature", change: func(r *http.Request) { r.Header.Add("Authorization", r.Header.Get("Authorization")) }},
		{name: "nonhex signature", change: func(r *http.Request) {
			authorization := r.Header.Get("Authorization")
			r.Header.Set("Authorization", authorization[:len(authorization)-1]+"z")
		}},
		{name: "date missing", change: func(r *http.Request) { r.Header.Del("X-Oss-Date") }},
		{name: "date duplicated", change: func(r *http.Request) { r.Header.Add("X-Oss-Date", r.Header.Get("X-Oss-Date")) }},
		{name: "date outside signature scope", change: func(r *http.Request) { r.Header.Set("X-Oss-Date", "20000101T000000Z") }},
		{name: "signed wrong key", change: func(r *http.Request) {
			r.Header.Set("Authorization", strings.Replace(r.Header.Get("Authorization"), "test-ak", "other-ak", 1))
		}},
		{name: "unexpected token", change: func(r *http.Request) { r.Header.Set("X-Oss-Security-Token", "unexpected") }},
		{name: "empty token header", change: func(r *http.Request) { r.Header["X-Oss-Security-Token"] = []string{""} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := makeRequest()
			tc.change(request)
			if validOSSFixtureSignedRequest(request, fixtureReadTestBucket, "test-ak", "") {
				t.Fatal("changed target accepted")
			}
		})
	}
}

func TestOSSFixtureReadRejectsRenewedIdentityBeforeOSS(t *testing.T) {
	requests := 0
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
		requests++
		return fixtureReadResponse(200, "req-list", fixtureReadEmptyXML()), nil
	}))
	first, err := canonicalIdentity(map[string]any{"AccountId": "1234567890123456", "IdentityType": "RAMUser", "UserId": "fixture-user"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonicalIdentity(map[string]any{"AccountId": "1234567890123456", "IdentityType": "RAMUser", "UserId": "other-user"})
	if err != nil {
		t.Fatal(err)
	}
	resolveCredentialSnapshotIdentity = func(_ context.Context, _ resolvedOpenAPIProfile, snapshot *credentialSnapshot, _ string) (telemetry.Identity, error) {
		if snapshot.AccessKeyID == "second-ak" {
			return second, nil
		}
		return first, nil
	}
	caller.Profile.Acquirer = &fixtureReadRotatingAcquirer{snapshots: []credentialSnapshot{
		{AccessKeyID: "first-ak", AccessKeySecret: "secret", SecurityToken: "first-token"},
		{AccessKeyID: "second-ak", AccessKeySecret: "secret", SecurityToken: "second-token"},
	}}
	_, err = caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil)
	if appErrorCode(err) != "CredentialIdentityChanged" || requests != 0 {
		t.Fatalf("renewed identity reached OSS: err=%v requests=%d", err, requests)
	}
}

func TestOSSFixtureReadSignsRenewedCredentialWithSameIdentity(t *testing.T) {
	requests := 0
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Header.Get("X-Oss-Security-Token") != "second-token" || !strings.Contains(request.Header.Get("Authorization"), "Credential=second-ak/") {
			t.Fatal("read used the wrong credential snapshot")
		}
		return fixtureReadResponse(200, "req-list", fixtureReadEmptyXML()), nil
	}))
	caller.Profile.Acquirer = &fixtureReadRotatingAcquirer{snapshots: []credentialSnapshot{
		{AccessKeyID: "first-ak", AccessKeySecret: "secret", SecurityToken: "first-token"},
		{AccessKeyID: "second-ak", AccessKeySecret: "secret", SecurityToken: "second-token"},
	}}
	response, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil)
	if err != nil || requests != 1 || response["KeyCount"] != 0 {
		t.Fatalf("same-identity renewal failed: response=%#v err=%v requests=%d", response, err, requests)
	}
}

func TestOSSFixtureReadSignsStaticExternalCredential(t *testing.T) {
	requests := 0
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(request.Header.Get("Authorization"), "Credential=second-ak/") || request.Header.Get("X-Oss-Security-Token") != "" {
			t.Fatal("static External read used an unpinned signer or token")
		}
		return fixtureReadResponse(200, "req-list", fixtureReadEmptyXML()), nil
	}))
	caller.Profile.Mode = credentialModeExternal
	caller.Profile.Acquirer = &fixtureReadRotatingAcquirer{snapshots: []credentialSnapshot{
		{AccessKeyID: "first-ak", AccessKeySecret: "secret"},
		{AccessKeyID: "second-ak", AccessKeySecret: "secret"},
	}}
	response, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil)
	if err != nil || requests != 1 || response["KeyCount"] != 0 {
		t.Fatalf("static External read failed: response=%#v err=%v requests=%d", response, err, requests)
	}
}

func TestOSSFixtureReadRejectsRenewableCredentialWithoutSTSBeforeOSS(t *testing.T) {
	requests := 0
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
		requests++
		return fixtureReadResponse(200, "req-list", fixtureReadEmptyXML()), nil
	}))
	caller.Profile.Acquirer = &fixtureReadRotatingAcquirer{snapshots: []credentialSnapshot{
		{AccessKeyID: "first-ak", AccessKeySecret: "secret", SecurityToken: "first-token"},
		{AccessKeyID: "second-ak", AccessKeySecret: "secret"},
	}}
	_, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil)
	if appErrorCode(err) != "InvalidCredentials" || requests != 0 {
		t.Fatalf("renewable non-STS credential reached OSS: err=%v requests=%d", err, requests)
	}
}

type fixtureReadRotatingAcquirer struct {
	snapshots []credentialSnapshot
	calls     int
}

func (a *fixtureReadRotatingAcquirer) Renewable() bool { return true }

func (a *fixtureReadRotatingAcquirer) Acquire(context.Context) (*credentialSnapshot, error) {
	if a.calls >= len(a.snapshots) {
		return nil, errors.New("unexpected credential read")
	}
	snapshot := a.snapshots[a.calls]
	a.calls++
	return &snapshot, nil
}

func TestOSSFixtureReadWithoutPinKeepsOssutil(t *testing.T) {
	runner := &fakeOSSUtilRunner{results: []ossUtilRunnerResult{
		{stdout: []byte("2.3.0")},
		{stdout: []byte("ListObjects help")},
		{stdout: []byte("{\"Name\":\"ordinary\",\"KeyCount\":0,\"IsTruncated\":false}")},
	}}
	requests := 0
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
		requests++
		return fixtureReadResponse(200, "req-list", fixtureReadEmptyXML()), nil
	}))
	caller.Profile.ExpectedIdentityHash = ""
	caller.Profile.PinCredentialIdentity = false
	caller.runner = runner
	caller.resolveRuntime = func(func(string) string) (string, error) { return "/test/ossutil", nil }
	response, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, nil)
	if err != nil || requests != 0 || len(runner.calls) != 3 || response["Name"] != "ordinary" {
		t.Fatalf("ordinary OSS path changed: response=%#v err=%v requests=%d calls=%d", response, err, requests, len(runner.calls))
	}
}
