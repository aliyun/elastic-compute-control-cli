package aliyun

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	osscredentials "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/retry"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

const ossFixtureReadBodyLimit = 64 * 1024

// Match the owned bucket grammar in e2e/internal/journalfile/volume_fixture.go.
var ossFixtureBucketPattern = regexp.MustCompile(`^ecctl-e2e-[0-9]{8}t[0-9]{6}-[a-f0-9]{12}$`)

func (c *OSSUtilCaller) ossFixtureReadBucket(operation string, request map[string]any, passthrough []string) (string, bool) {
	if operation != "ListObjects" || len(passthrough) != 0 || len(request) != 2 || c.Region != "cn-hangzhou" ||
		c.Profile.RegionSource != ecconfig.RegionSourceExplicit || !c.Profile.ProfileBound ||
		c.requestedProfile == "" || c.Profile.Name != c.requestedProfile ||
		!c.Profile.PinCredentialIdentity || c.Profile.ExpectedIdentityHash == "" {
		return "", false
	}
	bucket, ok := request["Bucket"].(string)
	if !ok || !ossFixtureBucketPattern.MatchString(bucket) {
		return "", false
	}
	switch maxKeys := request["MaxKeys"].(type) {
	case float64:
		return bucket, maxKeys == 1
	case int:
		return bucket, maxKeys == 1
	case json.Number:
		return bucket, maxKeys.String() == "1"
	default:
		return "", false
	}
}

func (c *OSSUtilCaller) ossFixtureOwnedBucket(operation string, request map[string]any) (string, bool) {
	if operation != "PutBucket" && operation != "GetBucketInfo" && operation != "DeleteBucket" && operation != "ListObjects" ||
		c.Region != "cn-hangzhou" || c.Profile.RegionSource != ecconfig.RegionSourceExplicit ||
		!c.Profile.ProfileBound || c.requestedProfile == "" || c.Profile.Name != c.requestedProfile ||
		!c.Profile.PinCredentialIdentity || c.Profile.ExpectedIdentityHash == "" {
		return "", false
	}
	bucket, ok := request["Bucket"].(string)
	return bucket, ok && ossFixtureBucketPattern.MatchString(bucket)
}

func ossFixtureExactRequest(operation string, request map[string]any) bool {
	switch operation {
	case "PutBucket":
		return len(request) == 2 && request["ACL"] == "private"
	case "GetBucketInfo", "DeleteBucket":
		return len(request) == 1
	case "ListObjects":
		if len(request) != 2 {
			return false
		}
		switch value := request["MaxKeys"].(type) {
		case float64:
			return value == 1
		case int:
			return value == 1
		case json.Number:
			return value.String() == "1"
		}
	}
	return false
}

func (c *OSSUtilCaller) callOSSFixtureBucketOperation(ctx context.Context, operation, bucket string) (map[string]any, error) {
	response, err := c.callOSSFixtureOperation(ctx, operation, bucket)
	if err != nil || operation != "PutBucket" {
		return response, err
	}
	info, err := c.callOSSFixtureOperation(ctx, "GetBucketInfo", bucket)
	if err != nil {
		return nil, err
	}
	bucketInfo, ok := info["Bucket"].(map[string]any)
	if !ok {
		return nil, ecerrors.Service("CloudAPIError", i18n.NewLocalizer("en").Message("CloudAPIError"), false)
	}
	if bucketInfo["Name"] != bucket {
		return nil, ecerrors.Service("CloudAPIError", i18n.NewLocalizer("en").Message("CloudAPIError"), false)
	}
	response["Bucket"] = info["Bucket"]
	return response, nil
}

func (c *OSSUtilCaller) callOSSFixtureListObjects(ctx context.Context, bucket string) (map[string]any, error) {
	return c.callOSSFixtureOperation(ctx, "ListObjects", bucket)
}

func (c *OSSUtilCaller) callOSSFixtureOperation(ctx context.Context, operation, bucket string) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = withCredentialOperationRegion(ctx, c.Region)
	operationAcquirer, err := credentialAcquirerForOperation(ctx, c.Profile.Acquirer)
	if err != nil {
		return nil, credentialResolutionError(&credentialProviderError{mode: c.Profile.Mode, err: err})
	}
	profile := c.Profile
	profile.Acquirer = operationAcquirer
	initial, err := operationAcquirer.Acquire(ctx)
	if err != nil {
		return nil, credentialResolutionError(&credentialProviderError{mode: c.Profile.Mode, err: err})
	}
	if err := validateCredentialLifetime(ctx, operationAcquirer, initial); err != nil {
		return nil, credentialResolutionError(err)
	}
	identityGuard, err := newOperationIdentityGuard(ctx, profile, initial)
	if err != nil {
		return nil, credentialResolutionError(&credentialProviderError{mode: c.Profile.Mode, err: err})
	}

	session := telemetry.FromContext(ctx)
	session.RegisterIdentity(credentialIdentityCacheKey(profile, initial), identityGuard.Resolver())
	endSpan := session.StartAPI(telemetry.APIRequest{
		Service: "Oss", API: operation, APIVersion: "oss-sdk-v2", Transport: "oss-sdk-v2",
		OperationID: session.NextOperationID(), Attempt: 1, RetryObservable: false,
	})
	proof := &ossFixtureReadProof{bucket: bucket, operation: operation}
	provider := osscredentials.CredentialsProviderFunc(func(ctx context.Context) (osscredentials.Credentials, error) {
		snapshot, err := operationAcquirer.Acquire(ctx)
		if err != nil {
			return osscredentials.Credentials{}, credentialResolutionError(&credentialProviderError{mode: c.Profile.Mode, err: err})
		}
		if err := validateCredentialLifetime(ctx, operationAcquirer, snapshot); err != nil {
			return osscredentials.Credentials{}, credentialResolutionError(err)
		}
		if err := identityGuard.Validate(ctx, snapshot); err != nil {
			return osscredentials.Credentials{}, credentialResolutionError(&credentialProviderError{mode: c.Profile.Mode, err: err})
		}
		if snapshot.AccessKeyID == "" || snapshot.AccessKeySecret == "" || credentialAcquirerIsRenewable(operationAcquirer) && snapshot.SecurityToken == "" && !credentialStaticExternalSnapshot(profile.Mode, snapshot) {
			return osscredentials.Credentials{}, ecerrors.Client("InvalidCredentials", i18n.NewLocalizer("en").Message("InvalidCredentials"))
		}
		proof.accessKey, proof.securityToken = snapshot.AccessKeyID, snapshot.SecurityToken
		credential := osscredentials.Credentials{
			AccessKeyID: snapshot.AccessKeyID, AccessKeySecret: snapshot.AccessKeySecret, SecurityToken: snapshot.SecurityToken,
		}
		if !snapshot.ExpiresAt.IsZero() {
			credential.Expires = &snapshot.ExpiresAt
		}
		return credential, nil
	})

	transport := c.fixtureReadTransport
	if transport == nil {
		standard := &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  30 * time.Second,
			MaxResponseHeaderBytes: 16 * 1024,
		}
		defer standard.CloseIdleConnections()
		transport = standard
	}
	client := &http.Client{
		Transport:     &ossFixtureProofTransport{delegate: transport, proof: proof},
		Timeout:       45 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	config := oss.NewConfig().
		WithRegion("cn-hangzhou").
		WithEndpoint("https://oss-cn-hangzhou.aliyuncs.com").
		WithSignatureVersion(oss.SignatureVersionV4).
		WithUsePathStyle(false).
		WithDisableSSL(false).
		WithInsecureSkipVerify(false).
		WithEnabledRedirect(false).
		WithProxyFromEnvironment(false).
		WithRetryer(retry.NopRetryer{}).
		WithRetryMaxAttempts(1).
		WithLogLevel(oss.LogOff).
		WithCredentialsProvider(provider).
		WithHttpClient(client)
	providerClient := oss.NewClient(config)
	var result any
	var callErr error
	switch operation {
	case "ListObjects":
		result, callErr = providerClient.ListObjectsV2(ctx, &oss.ListObjectsV2Request{Bucket: &bucket, MaxKeys: 1})
	case "PutBucket":
		result, callErr = providerClient.PutBucket(ctx, &oss.PutBucketRequest{Bucket: &bucket, Acl: oss.BucketACLPrivate})
	case "GetBucketInfo":
		result, callErr = providerClient.GetBucketInfo(ctx, &oss.GetBucketInfoRequest{Bucket: &bucket})
	case "DeleteBucket":
		result, callErr = providerClient.DeleteBucket(ctx, &oss.DeleteBucketRequest{Bucket: &bucket})
	default:
		return nil, ecerrors.Client("InvalidParameter", "unsupported owned OSS fixture operation")
	}
	if callErr != nil {
		mapped := ossFixtureReadError(callErr, proof)
		endSpan(telemetry.APIResult{Err: mapped, RequestID: proof.requestID})
		return nil, mapped
	}
	var response map[string]any
	var responseErr error
	switch value := result.(type) {
	case *oss.ListObjectsV2Result:
		response, responseErr = ossFixtureListResponse(value, proof)
	case *oss.PutBucketResult:
		response, responseErr = ossFixtureMutationResponse(value.ResultCommon, proof)
	case *oss.GetBucketInfoResult:
		response, responseErr = ossFixtureBucketInfoResponse(value, proof, c.Profile.ExpectedAccountID)
	case *oss.DeleteBucketResult:
		response, responseErr = ossFixtureMutationResponse(value.ResultCommon, proof)
	default:
		responseErr = ecerrors.Service("CloudAPIError", i18n.NewLocalizer("en").Message("CloudAPIError"), false)
	}
	endSpan(telemetry.APIResult{Err: responseErr, RequestID: proof.requestID})
	return response, responseErr
}

type ossFixtureReadProof struct {
	bucket        string
	operation     string
	accessKey     string
	securityToken string
	target        string
	attempts      int
	requestValid  bool
	responseSeen  bool
	status        int
	requestID     string
	serverDate    string
	body          []byte
}

func (p *ossFixtureReadProof) complete() bool {
	return p != nil && p.attempts == 1 && p.requestValid && p.responseSeen && p.requestID != ""
}

type ossFixtureProofTransport struct {
	delegate http.RoundTripper
	proof    *ossFixtureReadProof
}

func (t *ossFixtureProofTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	p := t.proof
	if p == nil || t.delegate == nil {
		return nil, errors.New("OSS fixture transport is unavailable")
	}
	p.attempts++
	if p.attempts != 1 || !validOSSFixtureSignedOperation(request, p.operation, p.bucket, p.accessKey, p.securityToken) {
		return nil, errors.New("OSS fixture signed request changed")
	}
	p.requestValid = true
	p.target = request.Method + " " + request.URL.String()
	response, err := t.delegate.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, errors.New("OSS fixture response is missing")
	}
	response.Request = request
	expectedHost := p.bucket + ".oss-cn-hangzhou.aliyuncs.com"
	if response.TLS == nil || response.TLS.ServerName != expectedHost || len(response.TLS.PeerCertificates) == 0 ||
		response.TLS.PeerCertificates[0].VerifyHostname(expectedHost) != nil ||
		len(response.TLS.VerifiedChains) == 0 || len(response.TLS.VerifiedChains[0]) == 0 {
		response.Body.Close()
		return nil, errors.New("OSS fixture TLS response is unverified")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, ossFixtureReadBodyLimit+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(body) > ossFixtureReadBodyLimit {
		return nil, errors.New("OSS fixture response body is unavailable")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	p.responseSeen = true
	p.status = response.StatusCode
	p.body = body
	if requestID, ok := ossFixtureSingleHeader(response.Header, "X-Oss-Request-Id"); ok {
		p.requestID = validTelemetryRequestID(requestID)
	}
	if date, ok := ossFixtureSingleHeader(response.Header, "Date"); ok {
		p.serverDate = date
	}
	return response, nil
}

func validOSSFixtureSignedRequest(request *http.Request, bucket, accessKey, securityToken string) bool {
	return validOSSFixtureSignedOperation(request, "ListObjects", bucket, accessKey, securityToken)
}

func validOSSFixtureSignedOperation(request *http.Request, operation, bucket, accessKey, securityToken string) bool {
	if operation == "" {
		operation = "ListObjects"
	}
	if request == nil || request.URL == nil || accessKey == "" {
		return false
	}
	expectedHost := bucket + ".oss-cn-hangzhou.aliyuncs.com"
	u := request.URL
	if u.Scheme != "https" || u.Host != expectedHost || request.Host != expectedHost || u.Path != "/" ||
		u.RawPath != "" || u.EscapedPath() != "/" || u.Opaque != "" || u.User != nil || u.Fragment != "" ||
		u.RawFragment != "" || u.ForceQuery || u.Port() != "" {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	switch operation {
	case "ListObjects":
		if request.Method != http.MethodGet || len(query) != 3 || len(query["list-type"]) != 1 || query.Get("list-type") != "2" ||
			len(query["encoding-type"]) != 1 || query.Get("encoding-type") != "url" ||
			len(query["max-keys"]) != 1 || query.Get("max-keys") != "1" {
			return false
		}
	case "GetBucketInfo":
		if request.Method != http.MethodGet || len(query) != 1 || len(query["bucketInfo"]) != 1 || query.Get("bucketInfo") != "" {
			return false
		}
	case "PutBucket":
		acl, ok := ossFixtureSingleHeader(request.Header, "X-Oss-Acl")
		if request.Method != http.MethodPut || len(query) != 0 || !ok || acl != "private" || request.ContentLength > 0 {
			return false
		}
	case "DeleteBucket":
		if request.Method != http.MethodDelete || len(query) != 0 || request.ContentLength > 0 {
			return false
		}
	default:
		return false
	}
	authorization, ok := ossFixtureSingleHeader(request.Header, "Authorization")
	if !ok {
		return false
	}
	prefix := "OSS4-HMAC-SHA256 Credential=" + accessKey + "/"
	if !strings.HasPrefix(authorization, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(authorization, prefix), ",Signature=")
	if len(parts) != 2 || len(parts[1]) != 64 {
		return false
	}
	scope := strings.Split(parts[0], "/")
	if len(scope) != 4 || len(scope[0]) != 8 || scope[1] != "cn-hangzhou" || scope[2] != "oss" || scope[3] != "aliyun_v4_request" {
		return false
	}
	if signature, err := hex.DecodeString(parts[1]); err != nil || len(signature) != 32 {
		return false
	}
	date, ok := ossFixtureSingleHeader(request.Header, "X-Oss-Date")
	if !ok || len(date) != 16 || !strings.HasPrefix(date, scope[0]+"T") || !strings.HasSuffix(date, "Z") {
		return false
	}
	token, hasToken := ossFixtureSingleHeader(request.Header, "X-Oss-Security-Token")
	if securityToken == "" {
		return !hasToken && !ossFixtureHasHeader(request.Header, "X-Oss-Security-Token")
	}
	return hasToken && token == securityToken
}

func ossFixtureHasHeader(header http.Header, name string) bool {
	for key := range header {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func ossFixtureSingleHeader(header http.Header, name string) (string, bool) {
	values := []string{}
	for key, found := range header {
		if strings.EqualFold(key, name) {
			values = append(values, found...)
		}
	}
	if len(values) != 1 || values[0] == "" {
		return "", false
	}
	return values[0], true
}

func ossFixtureReadError(source error, proof *ossFixtureReadProof) error {
	var credentialErr *ecerrors.AppError
	if errors.As(source, &credentialErr) {
		return credentialErr
	}
	var serviceErr *oss.ServiceError
	if errors.As(source, &serviceErr) && ossFixtureVerifiedAbsence(serviceErr, proof) {
		return ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").MessageData("NotFoundWithResource", map[string]any{"Resource": proof.bucket}),
			ecerrors.WithRawCause("NoSuchBucket", i18n.NewLocalizer("en").Message("NotFound")),
			ecerrors.WithRequestID(proof.requestID), ecerrors.WithVerifiedProviderResponse())
	}
	return ecerrors.Service("CloudAPIError", i18n.NewLocalizer("en").Message("CloudAPIError"), false)
}

type ossFixtureErrorBody struct {
	XMLName    xml.Name `xml:"Error"`
	Code       []string `xml:"Code"`
	RequestID  []string `xml:"RequestId"`
	BucketName []string `xml:"BucketName"`
}

func ossFixtureSingleXMLDocument(body []byte, value any) bool {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	prolog := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		switch item := token.(type) {
		case xml.CharData:
			if len(bytes.TrimSpace(item)) != 0 {
				return false
			}
		case xml.ProcInst:
			if prolog || item.Target != "xml" {
				return false
			}
			prolog = true
		case xml.StartElement:
			if err := decoder.DecodeElement(value, &item); err != nil {
				return false
			}
			goto suffix
		default:
			return false
		}
	}
suffix:
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
		space, ok := token.(xml.CharData)
		if !ok || len(bytes.TrimSpace(space)) != 0 {
			return false
		}
	}
}

func ossFixtureVerifiedAbsence(serviceErr *oss.ServiceError, proof *ossFixtureReadProof) bool {
	if serviceErr == nil || !proof.complete() || proof.status != http.StatusNotFound ||
		serviceErr.StatusCode != http.StatusNotFound || serviceErr.Code != "NoSuchBucket" ||
		serviceErr.RequestID != proof.requestID || serviceErr.RequestTarget != proof.target {
		return false
	}
	if requestID, ok := ossFixtureSingleHeader(serviceErr.Headers, "X-Oss-Request-Id"); !ok || requestID != proof.requestID {
		return false
	}
	var body ossFixtureErrorBody
	if !ossFixtureSingleXMLDocument(proof.body, &body) || body.XMLName.Local != "Error" ||
		len(body.Code) != 1 || body.Code[0] != "NoSuchBucket" || len(body.RequestID) > 1 || len(body.BucketName) > 1 {
		return false
	}
	if len(body.RequestID) == 1 && body.RequestID[0] != proof.requestID ||
		len(body.BucketName) == 1 && body.BucketName[0] != proof.bucket {
		return false
	}
	return true
}

type ossFixtureListBody struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	Name                  []string `xml:"Name"`
	Prefix                []string `xml:"Prefix"`
	Delimiter             []string `xml:"Delimiter"`
	StartAfter            []string `xml:"StartAfter"`
	ContinuationToken     []string `xml:"ContinuationToken"`
	NextContinuationToken []string `xml:"NextContinuationToken"`
	MaxKeys               []string `xml:"MaxKeys"`
	EncodingType          []string `xml:"EncodingType"`
	IsTruncated           []string `xml:"IsTruncated"`
	KeyCount              []string `xml:"KeyCount"`
	Contents              []struct {
		Key []string `xml:"Key"`
	} `xml:"Contents"`
	CommonPrefixes []struct {
		Prefix string `xml:"Prefix"`
	} `xml:"CommonPrefixes"`
}

func ossFixtureListResponse(result *oss.ListObjectsV2Result, proof *ossFixtureReadProof) (map[string]any, error) {
	invalid := func() (map[string]any, error) {
		return nil, ecerrors.Service("CloudAPIError", i18n.NewLocalizer("en").Message("CloudAPIError"), false)
	}
	if result == nil || !proof.complete() || proof.status != http.StatusOK || result.StatusCode != http.StatusOK {
		return invalid()
	}
	var body ossFixtureListBody
	if !ossFixtureSingleXMLDocument(proof.body, &body) || body.XMLName.Local != "ListBucketResult" ||
		len(body.Name) != 1 || body.Name[0] != proof.bucket || len(body.KeyCount) != 1 || len(body.IsTruncated) != 1 ||
		len(body.CommonPrefixes) != 0 || len(body.MaxKeys) > 1 || len(body.EncodingType) > 1 || len(body.NextContinuationToken) > 1 {
		return invalid()
	}
	for _, optional := range [][]string{body.Prefix, body.Delimiter, body.StartAfter, body.ContinuationToken} {
		if len(optional) > 1 || len(optional) == 1 && optional[0] != "" {
			return invalid()
		}
	}
	if len(body.MaxKeys) == 1 && body.MaxKeys[0] != "1" || len(body.EncodingType) == 1 && body.EncodingType[0] != "url" {
		return invalid()
	}
	count, err := strconv.Atoi(body.KeyCount[0])
	if err != nil || strconv.Itoa(count) != body.KeyCount[0] || count < 0 || count > 1 || count != len(body.Contents) ||
		count != result.KeyCount || result.Name == nil || *result.Name != proof.bucket || len(result.Contents) != count || len(result.CommonPrefixes) != 0 {
		return invalid()
	}
	var truncated bool
	switch body.IsTruncated[0] {
	case "true":
		truncated = true
	case "false":
	default:
		return invalid()
	}
	if result.IsTruncated != truncated || count == 0 && truncated ||
		count == 0 && len(body.NextContinuationToken) == 1 && body.NextContinuationToken[0] != "" {
		return invalid()
	}
	response := map[string]any{"Name": proof.bucket, "IsTruncated": truncated, "KeyCount": count}
	if len(body.MaxKeys) == 1 {
		if result.MaxKeys != 1 {
			return invalid()
		}
		response["MaxKeys"] = 1
	}
	if len(body.EncodingType) == 1 {
		response["EncodingType"] = "url"
	}
	if len(body.NextContinuationToken) == 1 && body.NextContinuationToken[0] != "" {
		if result.NextContinuationToken == nil || *result.NextContinuationToken != body.NextContinuationToken[0] {
			return invalid()
		}
		response["NextContinuationToken"] = body.NextContinuationToken[0]
	}
	if count == 1 {
		if len(body.Contents[0].Key) != 1 || body.Contents[0].Key[0] == "" ||
			result.Contents[0].Key == nil || *result.Contents[0].Key == "" {
			return invalid()
		}
		response["Contents"] = []map[string]any{{"Key": *result.Contents[0].Key}}
	}
	return response, nil
}

func ossFixtureMutationResponse(result oss.ResultCommon, proof *ossFixtureReadProof) (map[string]any, error) {
	invalid := func() (map[string]any, error) {
		return nil, ecerrors.Service("CloudAPIError", i18n.NewLocalizer("en").Message("CloudAPIError"), false)
	}
	if !proof.complete() || result.StatusCode != proof.status || len(proof.body) != 0 {
		return invalid()
	}
	requestID, ok := ossFixtureSingleHeader(result.Headers, "X-Oss-Request-Id")
	if !ok || requestID != proof.requestID {
		return invalid()
	}
	switch proof.operation {
	case "PutBucket":
		if result.StatusCode != http.StatusOK || proof.serverDate == "" {
			return invalid()
		}
		date, ok := ossFixtureSingleHeader(result.Headers, "Date")
		if !ok || date != proof.serverDate {
			return invalid()
		}
		if _, err := time.Parse(http.TimeFormat, date); err != nil {
			return invalid()
		}
	case "DeleteBucket":
		if result.StatusCode != http.StatusNoContent && result.StatusCode != http.StatusOK {
			return invalid()
		}
	default:
		return invalid()
	}
	response := map[string]any{"RequestId": proof.requestID}
	if proof.operation == "PutBucket" {
		response["ServerDate"] = proof.serverDate
	}
	return response, nil
}

type ossFixtureBucketInfoBody struct {
	XMLName xml.Name `xml:"BucketInfo"`
	Bucket  []struct {
		Name             []string `xml:"Name"`
		Location         []string `xml:"Location"`
		ExtranetEndpoint []string `xml:"ExtranetEndpoint"`
		CreationDate     []string `xml:"CreationDate"`
		Owner            []struct {
			ID []string `xml:"ID"`
		} `xml:"Owner"`
	} `xml:"Bucket"`
}

func ossFixtureBucketInfoResponse(result *oss.GetBucketInfoResult, proof *ossFixtureReadProof, accountID string) (map[string]any, error) {
	invalid := func() (map[string]any, error) {
		return nil, ecerrors.Service("CloudAPIError", i18n.NewLocalizer("en").Message("CloudAPIError"), false)
	}
	if result == nil || !proof.complete() || proof.operation != "GetBucketInfo" || proof.status != http.StatusOK ||
		result.StatusCode != http.StatusOK || accountID == "" {
		return invalid()
	}
	requestID, ok := ossFixtureSingleHeader(result.Headers, "X-Oss-Request-Id")
	if !ok || requestID != proof.requestID {
		return invalid()
	}
	var body ossFixtureBucketInfoBody
	if !ossFixtureSingleXMLDocument(proof.body, &body) || body.XMLName.Local != "BucketInfo" || len(body.Bucket) != 1 {
		return invalid()
	}
	bucket := body.Bucket[0]
	if len(bucket.Name) != 1 || bucket.Name[0] != proof.bucket || len(bucket.Location) != 1 || bucket.Location[0] != "oss-cn-hangzhou" ||
		len(bucket.ExtranetEndpoint) != 1 || bucket.ExtranetEndpoint[0] != "oss-cn-hangzhou.aliyuncs.com" ||
		len(bucket.CreationDate) != 1 || len(bucket.Owner) != 1 || len(bucket.Owner[0].ID) != 1 || bucket.Owner[0].ID[0] != accountID {
		return invalid()
	}
	created, err := time.Parse(time.RFC3339Nano, bucket.CreationDate[0])
	if err != nil || created.IsZero() || result.BucketInfo.Name == nil || *result.BucketInfo.Name != proof.bucket ||
		result.BucketInfo.Location == nil || *result.BucketInfo.Location != "oss-cn-hangzhou" ||
		result.BucketInfo.ExtranetEndpoint == nil || *result.BucketInfo.ExtranetEndpoint != "oss-cn-hangzhou.aliyuncs.com" ||
		result.BucketInfo.CreationDate == nil || !result.BucketInfo.CreationDate.Equal(created) ||
		result.BucketInfo.Owner == nil || result.BucketInfo.Owner.ID == nil || *result.BucketInfo.Owner.ID != accountID {
		return invalid()
	}
	return map[string]any{"RequestId": proof.requestID, "Bucket": map[string]any{
		"Name": proof.bucket, "Location": "oss-cn-hangzhou", "ExtranetEndpoint": "oss-cn-hangzhou.aliyuncs.com",
		"CreationDate": bucket.CreationDate[0], "Owner": map[string]any{"ID": accountID},
	}}, nil
}
