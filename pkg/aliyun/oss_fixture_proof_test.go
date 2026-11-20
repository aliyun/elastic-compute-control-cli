package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

func trustedFixtureProof(operation, requestID string, status int, body string) *ossFixtureReadProof {
	return &ossFixtureReadProof{
		bucket: fixtureReadTestBucket, operation: operation, attempts: 1, requestValid: true,
		responseSeen: true, status: status, requestID: requestID, body: []byte(body),
	}
}

func TestOSSFixtureExactBucketRequestContract(t *testing.T) {
	for _, tc := range []struct {
		name, operation string
		request         map[string]any
		valid           bool
	}{
		{"list float", "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": float64(1)}, true},
		{"list integer", "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": 1}, true},
		{"list JSON integer", "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": json.Number("1")}, true},
		{"list noncanonical JSON number", "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": json.Number("1.0")}, false},
		{"list malformed count", "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": "1"}, false},
		{"list extra filter", "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": 1, "Prefix": "hidden"}, false},
		{"private put", "PutBucket", map[string]any{"Bucket": fixtureReadTestBucket, "ACL": "private"}, true},
		{"public put", "PutBucket", map[string]any{"Bucket": fixtureReadTestBucket, "ACL": "public-read"}, false},
		{"get info", "GetBucketInfo", map[string]any{"Bucket": fixtureReadTestBucket}, true},
		{"scoped delete", "DeleteBucket", map[string]any{"Bucket": fixtureReadTestBucket}, true},
		{"delete extra field", "DeleteBucket", map[string]any{"Bucket": fixtureReadTestBucket, "Force": true}, false},
		{"unsupported operation", "GetObject", map[string]any{"Bucket": fixtureReadTestBucket}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ossFixtureExactRequest(tc.operation, tc.request); got != tc.valid {
				t.Fatalf("exact owned-bucket request = %t, want %t", got, tc.valid)
			}
		})
	}
}

func TestOSSFixtureProofTransportRequiresOneSignedVerifiedResponse(t *testing.T) {
	var signed *http.Request
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(request *http.Request) (*http.Response, error) {
		signed = request.Clone(context.Background())
		return fixtureReadResponse(200, "req-capture", fixtureReadEmptyXML()), nil
	}))
	if _, err := caller.CallWithArgs(context.Background(), "ListObjects", map[string]any{"Bucket": fixtureReadTestBucket, "MaxKeys": 1}, nil); err != nil || signed == nil {
		t.Fatalf("could not capture SDK signature: %v", err)
	}
	for _, tc := range []struct {
		name     string
		change   func(*http.Request)
		delegate http.RoundTripper
		valid    bool
		calls    int
	}{
		{"exact response", nil, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
			return fixtureReadResponse(200, "req-good", fixtureReadEmptyXML()), nil
		}), true, 1},
		{"missing delegate", nil, nil, false, 0},
		{"changed target", func(r *http.Request) { r.URL.Path = "/other" }, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
			t.Fatal("changed signed target reached delegate")
			return nil, nil
		}), false, 0},
		{"delegate failure", nil, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		}), false, 1},
		{"missing response", nil, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, nil
		}), false, 1},
		{"missing body", nil, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
			response := fixtureReadResponse(200, "req-good", fixtureReadEmptyXML())
			response.Body = nil
			return response, nil
		}), false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proof := trustedFixtureProof("ListObjects", "", 0, "")
			proof.attempts, proof.responseSeen, proof.requestValid = 0, false, false
			proof.accessKey = "test-ak"
			calls := 0
			delegate := tc.delegate
			if delegate != nil {
				delegate = fixtureReadRoundTripper(func(request *http.Request) (*http.Response, error) {
					calls++
					return tc.delegate.RoundTrip(request)
				})
			}
			request := signed.Clone(context.Background())
			if tc.change != nil {
				tc.change(request)
			}
			transport := &ossFixtureProofTransport{delegate: delegate, proof: proof}
			response, err := transport.RoundTrip(request)
			if (err == nil) != tc.valid || proof.complete() != tc.valid || calls != tc.calls {
				t.Fatalf("transport proof=%t response=%#v err=%v calls=%d", proof.complete(), response, err, calls)
			}
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if tc.valid {
				if _, err := transport.RoundTrip(signed.Clone(context.Background())); err == nil || proof.attempts != 2 {
					t.Fatal("second signed request was accepted")
				}
			}
		})
	}
}

func TestOSSFixtureSingleXMLDocumentRejectsAmbiguousProof(t *testing.T) {
	base := fixtureReadMissingXML("req-xml", "NoSuchBucket")
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"one error", base, true},
		{"XML declaration", "<?xml version=\"1.0\"?>" + base, true},
		{"duplicate declaration", "<?xml version=\"1.0\"?><?xml version=\"1.0\"?>" + base, false},
		{"leading comment", "<!-- hidden -->" + base, false},
		{"trailing comment", base + "<!-- hidden -->", false},
		{"malformed XML", "<Error><Code>NoSuchBucket</Error>", false},
		{"empty body", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var decoded ossFixtureErrorBody
			if got := ossFixtureSingleXMLDocument([]byte(tc.body), &decoded); got != tc.valid {
				t.Fatalf("single XML proof = %t, want %t", got, tc.valid)
			}
		})
	}
}

func TestOSSFixtureMutationReceiptBindsStatusDateAndRequestID(t *testing.T) {
	const date = "Mon, 16 Nov 2026 13:00:00 GMT"
	for _, tc := range []struct {
		name      string
		operation string
		change    func(*oss.ResultCommon, *ossFixtureReadProof)
		valid     bool
	}{
		{"trusted PUT", "PutBucket", nil, true},
		{"PUT missing proof", "PutBucket", func(_ *oss.ResultCommon, p *ossFixtureReadProof) { p.responseSeen = false }, false},
		{"PUT body present", "PutBucket", func(_ *oss.ResultCommon, p *ossFixtureReadProof) { p.body = []byte("<Bucket/>") }, false},
		{"PUT request mismatch", "PutBucket", func(r *oss.ResultCommon, _ *ossFixtureReadProof) { r.Headers.Set("X-Oss-Request-Id", "req-other") }, false},
		{"PUT missing date", "PutBucket", func(_ *oss.ResultCommon, p *ossFixtureReadProof) { p.serverDate = "" }, false},
		{"PUT date header differs", "PutBucket", func(r *oss.ResultCommon, _ *ossFixtureReadProof) {
			r.Headers.Set("Date", "Tue, 17 Nov 2026 13:00:00 GMT")
		}, false},
		{"PUT invalid date", "PutBucket", func(r *oss.ResultCommon, p *ossFixtureReadProof) {
			r.Headers.Set("Date", "yesterday")
			p.serverDate = "yesterday"
		}, false},
		{"trusted DELETE", "DeleteBucket", nil, true},
		{"DELETE wrong status", "DeleteBucket", func(r *oss.ResultCommon, p *ossFixtureReadProof) { r.StatusCode, p.status = 202, 202 }, false},
		{"unsupported mutation", "PutObject", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proof := trustedFixtureProof(tc.operation, "req-mutate", 200, "")
			proof.serverDate = date
			result := oss.ResultCommon{StatusCode: 200, Headers: http.Header{"X-Oss-Request-Id": {"req-mutate"}, "Date": {date}}}
			if tc.change != nil {
				tc.change(&result, proof)
			}
			response, err := ossFixtureMutationResponse(result, proof)
			if tc.valid {
				if err != nil || response["RequestId"] != "req-mutate" {
					t.Fatalf("trusted mutation receipt lost: %#v %v", response, err)
				}
			} else if err == nil || response != nil || ecerrors.HasVerifiedProviderResponse(err) {
				t.Fatalf("untrusted mutation receipt accepted: %#v %v", response, err)
			}
		})
	}
}

func TestOSSFixtureListBindsRawXMLToSDKResult(t *testing.T) {
	base := "<ListBucketResult><Name>" + fixtureReadTestBucket + "</Name><MaxKeys>1</MaxKeys><EncodingType>url</EncodingType>" +
		"<IsTruncated>true</IsTruncated><KeyCount>1</KeyCount><NextContinuationToken>cursor-2</NextContinuationToken>" +
		"<Contents><Key>object%20one</Key></Contents></ListBucketResult>"
	for _, tc := range []struct {
		name   string
		change func(*oss.ListObjectsV2Result, *ossFixtureReadProof)
		valid  bool
	}{
		{"matching raw and decoded page", nil, true},
		{"missing proof", func(_ *oss.ListObjectsV2Result, p *ossFixtureReadProof) { p.responseSeen = false }, false},
		{"wrong status", func(r *oss.ListObjectsV2Result, _ *ossFixtureReadProof) { r.StatusCode = 202 }, false},
		{"SDK count disagrees", func(r *oss.ListObjectsV2Result, _ *ossFixtureReadProof) { r.KeyCount = 0 }, false},
		{"SDK max keys disagrees", func(r *oss.ListObjectsV2Result, _ *ossFixtureReadProof) { r.MaxKeys = 2 }, false},
		{"SDK token missing", func(r *oss.ListObjectsV2Result, _ *ossFixtureReadProof) { r.NextContinuationToken = nil }, false},
		{"SDK token differs", func(r *oss.ListObjectsV2Result, _ *ossFixtureReadProof) {
			value := "cursor-other"
			r.NextContinuationToken = &value
		}, false},
		{"SDK object key missing", func(r *oss.ListObjectsV2Result, _ *ossFixtureReadProof) { r.Contents[0].Key = nil }, false},
		{"raw invalid flag", func(_ *oss.ListObjectsV2Result, p *ossFixtureReadProof) {
			p.body = []byte(strings.Replace(base, "<IsTruncated>true</IsTruncated>", "<IsTruncated>maybe</IsTruncated>", 1))
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, key, token := fixtureReadTestBucket, "object one", "cursor-2"
			result := &oss.ListObjectsV2Result{Name: &bucket, MaxKeys: 1, IsTruncated: true, KeyCount: 1, NextContinuationToken: &token,
				Contents: []oss.ObjectProperties{{Key: &key}}, ResultCommon: oss.ResultCommon{StatusCode: 200}}
			proof := trustedFixtureProof("ListObjects", "req-page", 200, base)
			if tc.change != nil {
				tc.change(result, proof)
			}
			response, err := ossFixtureListResponse(result, proof)
			if tc.valid {
				if err != nil || response["NextContinuationToken"] != "cursor-2" || response["KeyCount"] != 1 {
					t.Fatalf("matching SDK and raw page lost: %#v %v", response, err)
				}
			} else if err == nil || response != nil || ecerrors.HasVerifiedProviderResponse(err) {
				t.Fatalf("untrusted page accepted: %#v %v", response, err)
			}
		})
	}
}

func TestOSSFixtureBucketInfoBindsRawXMLToSDKIdentity(t *testing.T) {
	const account = "1234567890123456"
	body := "<BucketInfo><Bucket><Name>" + fixtureReadTestBucket + "</Name><Location>oss-cn-hangzhou</Location>" +
		"<ExtranetEndpoint>oss-cn-hangzhou.aliyuncs.com</ExtranetEndpoint><CreationDate>2026-11-16T13:00:00Z</CreationDate>" +
		"<Owner><ID>" + account + "</ID></Owner></Bucket></BucketInfo>"
	created, err := time.Parse(time.RFC3339Nano, "2026-11-16T13:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*oss.GetBucketInfoResult, *ossFixtureReadProof, *string)
		valid  bool
	}{
		{"matching owner and region", nil, true},
		{"unobserved response", func(_ *oss.GetBucketInfoResult, p *ossFixtureReadProof, _ *string) { p.responseSeen = false }, false},
		{"wrong operation", func(_ *oss.GetBucketInfoResult, p *ossFixtureReadProof, _ *string) { p.operation = "ListObjects" }, false},
		{"wrong status", func(r *oss.GetBucketInfoResult, _ *ossFixtureReadProof, _ *string) { r.StatusCode = 202 }, false},
		{"missing expected account", func(_ *oss.GetBucketInfoResult, _ *ossFixtureReadProof, a *string) { *a = "" }, false},
		{"request ID differs", func(r *oss.GetBucketInfoResult, _ *ossFixtureReadProof, _ *string) {
			r.Headers.Set("X-Oss-Request-Id", "req-other")
		}, false},
		{"wrong raw root", func(_ *oss.GetBucketInfoResult, p *ossFixtureReadProof, _ *string) {
			p.body = []byte(strings.Replace(body, "BucketInfo", "OtherRoot", 2))
		}, false},
		{"wrong raw owner", func(_ *oss.GetBucketInfoResult, p *ossFixtureReadProof, _ *string) {
			p.body = []byte(strings.Replace(body, account, "another-account", 1))
		}, false},
		{"bad raw creation date", func(_ *oss.GetBucketInfoResult, p *ossFixtureReadProof, _ *string) {
			p.body = []byte(strings.Replace(body, "2026-11-16T13:00:00Z", "yesterday", 1))
		}, false},
		{"SDK bucket differs", func(r *oss.GetBucketInfoResult, _ *ossFixtureReadProof, _ *string) {
			value := "another-bucket"
			r.BucketInfo.Name = &value
		}, false},
		{"SDK owner missing", func(r *oss.GetBucketInfoResult, _ *ossFixtureReadProof, _ *string) { r.BucketInfo.Owner = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, region, endpoint, owner := fixtureReadTestBucket, "oss-cn-hangzhou", "oss-cn-hangzhou.aliyuncs.com", account
			result := &oss.GetBucketInfoResult{
				BucketInfo:   oss.BucketInfo{Name: &bucket, Location: &region, ExtranetEndpoint: &endpoint, CreationDate: &created, Owner: &oss.Owner{ID: &owner}},
				ResultCommon: oss.ResultCommon{StatusCode: 200, Headers: http.Header{"X-Oss-Request-Id": {"req-info"}}},
			}
			proof := trustedFixtureProof("GetBucketInfo", "req-info", 200, body)
			expectedAccount := account
			if tc.change != nil {
				tc.change(result, proof, &expectedAccount)
			}
			response, err := ossFixtureBucketInfoResponse(result, proof, expectedAccount)
			if tc.valid {
				info, ok := response["Bucket"].(map[string]any)
				if err != nil || !ok || info["Name"] != fixtureReadTestBucket || info["Owner"].(map[string]any)["ID"] != account {
					t.Fatalf("trusted bucket identity lost: %#v %v", response, err)
				}
			} else if err == nil || response != nil || ecerrors.HasVerifiedProviderResponse(err) {
				t.Fatalf("untrusted bucket identity accepted: %#v %v", response, err)
			}
		})
	}
}

func TestOSSFixtureSignedBucketOperationGuardRejectsMutations(t *testing.T) {
	info := "<BucketInfo><Bucket><Name>" + fixtureReadTestBucket + "</Name><Location>oss-cn-hangzhou</Location>" +
		"<ExtranetEndpoint>oss-cn-hangzhou.aliyuncs.com</ExtranetEndpoint><CreationDate>2026-11-16T13:00:00Z</CreationDate>" +
		"<Owner><ID>1234567890123456</ID></Owner></Bucket></BucketInfo>"
	signed := map[string]*http.Request{}
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(request *http.Request) (*http.Response, error) {
		var operation string
		var response *http.Response
		switch {
		case request.Method == http.MethodPut:
			operation, response = "PutBucket", fixtureReadResponse(200, "req-put", "")
			response.Header.Set("Date", "Mon, 16 Nov 2026 13:00:00 GMT")
		case request.URL.Query().Has("bucketInfo"):
			operation, response = "GetBucketInfo", fixtureReadResponse(200, "req-info", info)
		case request.Method == http.MethodDelete:
			operation, response = "DeleteBucket", fixtureReadResponse(204, "req-delete", "")
		default:
			t.Fatalf("unexpected signed operation: %s %s", request.Method, request.URL)
		}
		signed[operation] = request.Clone(context.Background())
		return response, nil
	}))
	for _, tc := range []struct {
		operation string
		request   map[string]any
	}{
		{"PutBucket", map[string]any{"Bucket": fixtureReadTestBucket, "ACL": "private"}},
		{"GetBucketInfo", map[string]any{"Bucket": fixtureReadTestBucket}},
		{"DeleteBucket", map[string]any{"Bucket": fixtureReadTestBucket}},
	} {
		if _, err := caller.CallWithArgs(context.Background(), tc.operation, tc.request, nil); err != nil {
			t.Fatalf("SDK %s signature capture failed: %v", tc.operation, err)
		}
	}
	for _, tc := range []struct {
		name, operation string
		change          func(*http.Request)
	}{
		{"PUT public ACL", "PutBucket", func(r *http.Request) { r.Header.Set("X-Oss-Acl", "public-read") }},
		{"PUT duplicate ACL", "PutBucket", func(r *http.Request) { r.Header.Add("X-Oss-Acl", "private") }},
		{"PUT body", "PutBucket", func(r *http.Request) { r.ContentLength = 1 }},
		{"PUT query", "PutBucket", func(r *http.Request) { r.URL.RawQuery = "unexpected=1" }},
		{"GET info missing query", "GetBucketInfo", func(r *http.Request) { r.URL.RawQuery = "" }},
		{"GET info extra query", "GetBucketInfo", func(r *http.Request) { r.URL.RawQuery += "&unexpected=1" }},
		{"DELETE body", "DeleteBucket", func(r *http.Request) { r.ContentLength = 1 }},
		{"DELETE query", "DeleteBucket", func(r *http.Request) { r.URL.RawQuery = "unexpected=1" }},
		{"DELETE wrong method", "DeleteBucket", func(r *http.Request) { r.Method = http.MethodGet }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := signed[tc.operation].Clone(context.Background())
			if !validOSSFixtureSignedOperation(request, tc.operation, fixtureReadTestBucket, "test-ak", "") {
				t.Fatal("unaltered SDK request failed syntactic guard")
			}
			tc.change(request)
			if validOSSFixtureSignedOperation(request, tc.operation, fixtureReadTestBucket, "test-ak", "") {
				t.Fatal("mutated bucket operation passed syntactic guard")
			}
		})
	}
	listRequest := signed["GetBucketInfo"].Clone(context.Background())
	if validOSSFixtureSignedOperation(nil, "GetBucketInfo", fixtureReadTestBucket, "test-ak", "") ||
		validOSSFixtureSignedOperation(listRequest, "GetBucketInfo", fixtureReadTestBucket, "", "") ||
		validOSSFixtureSignedOperation(listRequest, "GetObject", fixtureReadTestBucket, "test-ak", "") {
		t.Fatal("missing signer or unsupported operation passed the signed-request guard")
	}
	badAuthorization := listRequest.Clone(context.Background())
	badAuthorization.Header.Set("Authorization", "OSS4-HMAC-SHA256 Credential=test-ak/not-a-scope,Signature=abc")
	if validOSSFixtureSignedOperation(badAuthorization, "GetBucketInfo", fixtureReadTestBucket, "test-ak", "") {
		t.Fatal("short signature passed the signed-request guard")
	}
	badScope := listRequest.Clone(context.Background())
	badScope.Header.Set("Authorization", strings.Replace(badScope.Header.Get("Authorization"), "/cn-hangzhou/oss/", "/cn-shanghai/oss/", 1))
	if validOSSFixtureSignedOperation(badScope, "GetBucketInfo", fixtureReadTestBucket, "test-ak", "") {
		t.Fatal("foreign signature scope passed the signed-request guard")
	}
}

func TestOSSFixtureAbsenceRequiresMatchingSDKRequestHeader(t *testing.T) {
	proof := trustedFixtureProof("ListObjects", "req-absence", 404, fixtureReadMissingXML("req-absence", "NoSuchBucket"))
	proof.target = "GET https://" + fixtureReadTestBucket + ".oss-cn-hangzhou.aliyuncs.com/?list-type=2"
	provider := &oss.ServiceError{StatusCode: 404, Code: "NoSuchBucket", RequestID: "req-absence", RequestTarget: proof.target,
		Headers: http.Header{"X-Oss-Request-Id": {"req-absence"}}}
	if !ossFixtureVerifiedAbsence(provider, proof) {
		t.Fatal("matching SDK and raw absence was rejected")
	}
	provider.Headers.Set("X-Oss-Request-Id", "req-other")
	if ossFixtureVerifiedAbsence(provider, proof) {
		t.Fatal("SDK header disagreed with raw response but proved absence")
	}
}

func TestOSSFixtureCredentialErrorsStopBeforeOSS(t *testing.T) {
	valid := credentialSnapshot{AccessKeyID: "test-ak", AccessKeySecret: "test-secret"}
	for _, tc := range []struct {
		name  string
		setup func(*OSSUtilCaller)
	}{
		{"missing source", func(c *OSSUtilCaller) { c.Profile.Acquirer = nil }},
		{"initial acquisition fails", func(c *OSSUtilCaller) {
			c.Profile.Acquirer = credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) { return nil, errors.New("source unavailable") })
		}},
		{"expired initial lease", func(c *OSSUtilCaller) {
			expired := valid
			expired.ExpiresAt = time.Now().Add(-time.Minute)
			c.Profile.Acquirer = &staticCredentialAcquirer{snapshot: expired}
		}},
		{"initial identity changes", func(c *OSSUtilCaller) { c.Profile.ExpectedIdentityHash = "different" }},
		{"signing acquisition fails", func(c *OSSUtilCaller) {
			calls := 0
			c.Profile.Acquirer = credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) {
				calls++
				if calls == 1 {
					copy := valid
					return &copy, nil
				}
				return nil, errors.New("source unavailable")
			})
		}},
		{"signing lease expires", func(c *OSSUtilCaller) {
			calls := 0
			c.Profile.Acquirer = credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) {
				calls++
				copy := valid
				if calls > 1 {
					copy.ExpiresAt = time.Now().Add(-time.Minute)
				}
				return &copy, nil
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
				requests++
				return fixtureReadResponse(200, "req-unexpected", fixtureReadEmptyXML()), nil
			}))
			tc.setup(caller)
			response, err := caller.callOSSFixtureOperation(context.Background(), "ListObjects", fixtureReadTestBucket)
			if err == nil || response != nil || requests != 0 {
				t.Fatalf("credential failure reached OSS: %#v %v requests=%d", response, err, requests)
			}
		})
	}
	caller := pinnedFixtureReadCaller(t, fixtureReadRoundTripper(func(*http.Request) (*http.Response, error) {
		return fixtureReadResponse(200, "req-list", fixtureReadEmptyXML()), nil
	}))
	caller.Profile.Acquirer = &staticCredentialAcquirer{snapshot: credentialSnapshot{AccessKeyID: "test-ak", AccessKeySecret: "test-secret", ExpiresAt: time.Now().Add(time.Hour)}}
	response, err := caller.callOSSFixtureOperation(nil, "ListObjects", fixtureReadTestBucket)
	if err != nil || response["KeyCount"] != 0 {
		t.Fatalf("nil context or current lease changed signed read: %#v %v", response, err)
	}
	if response, err = caller.callOSSFixtureOperation(context.Background(), "GetObject", fixtureReadTestBucket); response != nil || appErrorCode(err) != "InvalidParameter" {
		t.Fatalf("unsupported fixture operation reached SDK: %#v %v", response, err)
	}
}
