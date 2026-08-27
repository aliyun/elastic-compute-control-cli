package aliyun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"

	credentialslib "github.com/aliyun/credentials-go/credentials"
	credentialproviders "github.com/aliyun/credentials-go/credentials/providers"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

const (
	credentialModeAK                  = "AK"
	credentialModeStsToken            = "StsToken"
	credentialModeRamRoleArn          = "RamRoleArn"
	credentialModeEcsRamRole          = "EcsRamRole"
	credentialModeChainableRamRoleArn = "ChainableRamRoleArn"
	credentialModeOIDC                = "OIDC"
	credentialModeCloudSSO            = "CloudSSO"
	credentialModeOAuth               = "OAuth"
	credentialModeExternal            = "External"
	credentialModeCredentialsURI      = "CredentialsURI"
	credentialModeBearerToken         = "BearerToken"
	credentialExternalDisabledEnv     = "ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS"
	defaultBearerTokenHeaderKey       = "x-acs-bearer-token"
	defaultExternalCredentialTimeout  = 60 * time.Second
	externalCredentialWaitDelay       = 2 * time.Second
	credentialRefreshWindow           = 180 * time.Second
)

var daraDebugEnabledAtPackageInit = credentialDebugFlagEnabled(os.Getenv("DEBUG"), "dara")

func rejectUnsafeCredentialDebug(getenv func(string) string) error {
	if daraDebugEnabledAtPackageInit || credentialDebugFlagEnabled(os.Getenv("DEBUG"), "dara") || getenv != nil && credentialDebugFlagEnabled(getenv("DEBUG"), "dara") {
		return ecerrors.Client("UnsafeCredentialDebug", "Dara request debugging is disabled for credential-bearing commands")
	}
	return nil
}

func credentialDebugFlagEnabled(value, target string) bool {
	for _, flag := range strings.Split(value, ",") {
		if flag == target {
			return true
		}
	}
	return false
}

var supportedCredentialModes = map[string]string{
	strings.ToLower(credentialModeAK):                  credentialModeAK,
	strings.ToLower(credentialModeStsToken):            credentialModeStsToken,
	strings.ToLower(credentialModeRamRoleArn):          credentialModeRamRoleArn,
	strings.ToLower(credentialModeEcsRamRole):          credentialModeEcsRamRole,
	strings.ToLower(credentialModeChainableRamRoleArn): credentialModeChainableRamRoleArn,
	strings.ToLower(credentialModeOIDC):                credentialModeOIDC,
	strings.ToLower(credentialModeCloudSSO):            credentialModeCloudSSO,
	strings.ToLower(credentialModeOAuth):               credentialModeOAuth,
	strings.ToLower(credentialModeExternal):            credentialModeExternal,
	strings.ToLower(credentialModeCredentialsURI):      credentialModeCredentialsURI,
	strings.ToLower(credentialModeBearerToken):         credentialModeBearerToken,
}

var supportedCredentialModeValues = []string{
	credentialModeOAuth,
	credentialModeEcsRamRole,
	credentialModeRamRoleArn,
	credentialModeChainableRamRoleArn,
	credentialModeOIDC,
	credentialModeCloudSSO,
	credentialModeExternal,
	credentialModeCredentialsURI,
	credentialModeStsToken,
	credentialModeBearerToken,
	credentialModeAK,
}

type resolvedCredential struct {
	Acquirer             credentialAcquirer
	Mode                 string
	AuthType             string
	BearerTokenHeaderKey string
	Principal            string
	ExpectedAccountID    string
	ExpectedIdentityType string
	IdentityPolicy       credentialIdentityPolicy
}

// credentialSnapshot is one complete, internally consistent read from a
// credential source. Renewable sources may return a newer snapshot before a
// later signed request in the same ecctl operation.
type credentialSnapshot struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	BearerToken     string
	ProviderName    string
	Type            string
	ExpiresAt       time.Time
	IdentityProof   *credentialIdentityProof
}

type credentialAcquirer interface {
	Acquire(context.Context) (*credentialSnapshot, error)
}

type operationScopedCredentialAcquirer interface {
	ForOperation(context.Context) (credentialAcquirer, error)
}

func credentialAcquirerForOperation(ctx context.Context, acquirer credentialAcquirer) (credentialAcquirer, error) {
	if acquirer == nil {
		return nil, errors.New("credential source is unavailable")
	}
	if scoped, ok := acquirer.(operationScopedCredentialAcquirer); ok {
		return scoped.ForOperation(ctx)
	}
	return acquirer, nil
}

type renewableCredentialAcquirer interface {
	Renewable() bool
}

func credentialAcquirerIsRenewable(acquirer credentialAcquirer) bool {
	renewable, ok := acquirer.(renewableCredentialAcquirer)
	return ok && renewable.Renewable()
}

func validateCredentialLifetime(ctx context.Context, acquirer credentialAcquirer, snapshot *credentialSnapshot) error {
	if snapshotExpired(snapshot, time.Now()) {
		return ErrCredentialLeaseExpired
	}
	if credentialAcquirerIsRenewable(acquirer) || snapshot == nil || snapshot.ExpiresAt.IsZero() || ctx == nil {
		return nil
	}
	if deadline, ok := ctx.Deadline(); ok && !snapshot.ExpiresAt.After(deadline) {
		return ErrCredentialLeaseExpired
	}
	return nil
}

type credentialOperationRegionKey struct{}

func withCredentialOperationRegion(ctx context.Context, region string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(region) == "" {
		return ctx
	}
	return context.WithValue(ctx, credentialOperationRegionKey{}, strings.TrimSpace(region))
}

func credentialOperationRegion(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	region, _ := ctx.Value(credentialOperationRegionKey{}).(string)
	return region
}

type credentialAcquirerFunc func(context.Context) (*credentialSnapshot, error)

func (f credentialAcquirerFunc) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	return f(ctx)
}

type staticCredentialAcquirer struct {
	snapshot credentialSnapshot
}

func (a *staticCredentialAcquirer) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	copy := a.snapshot
	return &copy, nil
}

type credentialsProviderAcquirer struct {
	provider credentialproviders.CredentialsProvider
	mode     string
}

func (*credentialsProviderAcquirer) Renewable() bool { return true }

func credentialProviderHTTPOptions(ctx context.Context, connectDefault, readDefault time.Duration) *credentialproviders.HttpOptions {
	connectTimeout := connectDefault
	readTimeout := readDefault
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining < 0 {
				remaining = 0
			}
			if readTimeout <= 0 || remaining < readTimeout {
				readTimeout = remaining
			}
			if connectTimeout <= 0 || remaining < connectTimeout {
				connectTimeout = remaining
			}
		}
	}
	toMilliseconds := func(value time.Duration) int {
		if value <= 0 {
			return 1
		}
		milliseconds := value.Milliseconds()
		if milliseconds < 1 {
			milliseconds = 1
		}
		if milliseconds > int64(^uint(0)>>1) {
			return int(^uint(0) >> 1)
		}
		return int(milliseconds)
	}
	return &credentialproviders.HttpOptions{
		ConnectTimeout: toMilliseconds(connectTimeout),
		ReadTimeout:    toMilliseconds(readTimeout),
	}
}

type ecsCredentialAcquirer struct {
	roleName      string
	disableIMDSv1 bool
	disabled      bool
	client        credentialHTTPClient

	gate   contextGate
	cached *credentialSnapshot
}

func (*ecsCredentialAcquirer) Renewable() bool { return true }

func (a *ecsCredentialAcquirer) ForOperation(context.Context) (credentialAcquirer, error) {
	if a == nil {
		return nil, errors.New("ECS credential source is unavailable")
	}
	return &ecsCredentialAcquirer{roleName: a.roleName, disableIMDSv1: a.disableIMDSv1, disabled: a.disabled, client: a.client}, nil
}

func (a *ecsCredentialAcquirer) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.gate.Lock(ctx); err != nil {
		return nil, err
	}
	defer a.gate.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if a.disabled {
		return nil, &credentialProviderError{mode: credentialModeEcsRamRole, err: errors.New("IMDS credentials are disabled")}
	}
	if a.cached != nil && (a.cached.ExpiresAt.IsZero() || time.Until(a.cached.ExpiresAt) > credentialRefreshWindow) {
		copy := *a.cached
		return &copy, nil
	}
	snapshot, err := acquireECSMetadataCredential(ctx, a.roleName, a.disableIMDSv1, a.client)
	if err != nil {
		return nil, &credentialProviderError{mode: credentialModeEcsRamRole, err: err}
	}
	a.cached = snapshot
	copy := *snapshot
	return &copy, nil
}

func (a *credentialsProviderAcquirer) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	if a == nil || a.provider == nil {
		return nil, errors.New("credential provider is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	credentials, err := a.provider.GetCredentials()
	if err != nil {
		return nil, &credentialProviderError{mode: a.mode, err: err}
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return snapshotFromProviderCredentials(credentials)
}

func snapshotFromProviderCredentials(credentials *credentialproviders.Credentials) (*credentialSnapshot, error) {
	if credentials == nil || credentials.AccessKeyId == "" || credentials.AccessKeySecret == "" {
		return nil, errors.New("credential provider returned incomplete credentials")
	}
	credentialType := "access_key"
	if credentials.SecurityToken != "" {
		credentialType = "sts"
	}
	return &credentialSnapshot{
		AccessKeyID:     credentials.AccessKeyId,
		AccessKeySecret: credentials.AccessKeySecret,
		SecurityToken:   credentials.SecurityToken,
		ProviderName:    credentials.ProviderName,
		Type:            credentialType,
	}, nil
}

func staticCredentialForSnapshot(snapshot *credentialSnapshot) (credentialslib.Credential, error) {
	if snapshot == nil {
		return nil, errors.New("credential snapshot is unavailable")
	}
	if snapshot.BearerToken != "" {
		return credentialslib.NewCredential(&credentialslib.Config{
			Type:        stringPointer("bearer"),
			BearerToken: stringPointer(snapshot.BearerToken),
		})
	}
	if snapshot.AccessKeyID == "" || snapshot.AccessKeySecret == "" {
		return nil, errors.New("credential snapshot is incomplete")
	}
	credentialType := snapshot.Type
	if credentialType != "sts" && credentialType != "access_key" {
		if snapshot.SecurityToken != "" {
			credentialType = "sts"
		} else {
			credentialType = "access_key"
		}
	}
	return credentialslib.NewCredential(&credentialslib.Config{
		Type:            stringPointer(credentialType),
		AccessKeyId:     stringPointer(snapshot.AccessKeyID),
		AccessKeySecret: stringPointer(snapshot.AccessKeySecret),
		SecurityToken:   stringPointer(snapshot.SecurityToken),
	})
}

// operationCredential adapts ecctl's context-aware credential source to the
// Darabonba SDK credential contract. The SDK asks for one complete credential
// model immediately before each signed request, so renewable providers can
// rotate temporary material without changing the selected logical source.
type operationCredential struct {
	acquirer credentialAcquirer
	mode     string
	ctx      context.Context
	typeName string
	next     *credentialSnapshot
	validate func(context.Context, *credentialSnapshot) error
}

func newOperationCredential(acquirer credentialAcquirer, mode string, initial *credentialSnapshot, validators ...func(context.Context, *credentialSnapshot) error) (*operationCredential, error) {
	if acquirer == nil {
		return nil, errors.New("credential source is unavailable")
	}
	if initial == nil {
		return nil, errors.New("credential snapshot is unavailable")
	}
	typeName := initial.Type
	if typeName == "" {
		if initial.SecurityToken != "" {
			typeName = "sts"
		} else {
			typeName = "access_key"
		}
	}
	seed := *initial
	var validate func(context.Context, *credentialSnapshot) error
	if len(validators) > 0 {
		validate = validators[0]
	}
	return &operationCredential{acquirer: acquirer, mode: mode, ctx: context.Background(), typeName: typeName, next: &seed, validate: validate}, nil
}

func (c *operationCredential) SetContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.ctx = ctx
}

func (c *operationCredential) GetCredential() (*credentialslib.CredentialModel, error) {
	if c == nil || c.acquirer == nil {
		return nil, errors.New("credential source is unavailable")
	}
	snapshot := c.next
	c.next = nil
	if snapshot != nil && snapshotExpired(snapshot, time.Now()) && credentialAcquirerIsRenewable(c.acquirer) {
		snapshot = nil
	}
	if snapshot == nil {
		var err error
		snapshot, err = c.acquirer.Acquire(c.ctx)
		if err != nil {
			return nil, &credentialProviderError{mode: c.mode, err: err}
		}
	}
	if snapshotExpired(snapshot, time.Now()) {
		return nil, ErrCredentialLeaseExpired
	}
	if c.validate != nil {
		if err := c.validate(c.ctx, snapshot); err != nil {
			return nil, err
		}
	}
	if snapshot.AccessKeyID == "" || snapshot.AccessKeySecret == "" {
		return nil, errors.New("credential provider returned incomplete credentials")
	}
	typeName := snapshot.Type
	if typeName == "" {
		typeName = c.typeName
	}
	return &credentialslib.CredentialModel{
		AccessKeyId:     stringPointer(snapshot.AccessKeyID),
		AccessKeySecret: stringPointer(snapshot.AccessKeySecret),
		SecurityToken:   stringPointer(snapshot.SecurityToken),
		Type:            stringPointer(typeName),
		ProviderName:    stringPointer(snapshot.ProviderName),
	}, nil
}

func (c *operationCredential) GetAccessKeyId() (*string, error) {
	credential, err := c.GetCredential()
	if err != nil {
		return nil, err
	}
	return credential.AccessKeyId, nil
}

func (c *operationCredential) GetAccessKeySecret() (*string, error) {
	credential, err := c.GetCredential()
	if err != nil {
		return nil, err
	}
	return credential.AccessKeySecret, nil
}

func (c *operationCredential) GetSecurityToken() (*string, error) {
	credential, err := c.GetCredential()
	if err != nil {
		return nil, err
	}
	return credential.SecurityToken, nil
}

func (*operationCredential) GetBearerToken() *string { return stringPointer("") }

func (c *operationCredential) GetType() *string {
	if c == nil {
		return stringPointer("")
	}
	return stringPointer(c.typeName)
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func resolveOpenAPIProfile(profileName, configPath string, region ecconfig.ResolvedRegion, getenv func(string) string) (resolvedOpenAPIProfile, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if err := rejectUnsafeCredentialDebug(getenv); err != nil {
		return resolvedOpenAPIProfile{}, err
	}
	if configPath == "" {
		configPath = ecconfig.EcctlConfigPath(getenv)
	}
	if ignoreCredentialProfiles(getenv) {
		credential, err := resolveEnvironmentCredential(getenv)
		if err != nil {
			return resolvedOpenAPIProfile{}, credentialResolutionError(err)
		}
		resolvedRegion := region
		if resolvedRegion.Source == ecconfig.RegionSourceProfile {
			resolvedRegion = ecconfig.ResolvedRegion{}
		}
		if resolvedRegion.Value == "" {
			if value := strings.TrimSpace(getenv("ECCTL_REGION")); value != "" {
				resolvedRegion = ecconfig.ResolvedRegion{Value: value, Source: ecconfig.RegionSourceECCTL}
			} else if value := credentialRegionFromEnvironment(getenv); value != "" {
				resolvedRegion = ecconfig.ResolvedRegion{Value: value, Source: ecconfig.RegionSourceAlibabaEnv}
			}
		}
		setCredentialFallbackRegion(credential.Acquirer, resolvedRegion.Value)
		return resolvedOpenAPIProfile{
			Name:                  firstNonEmptyString(profileName, ecconfig.DefaultProfileName),
			Mode:                  credential.Mode,
			RegionID:              resolvedRegion.Value,
			RegionSource:          resolvedRegion.Source,
			Language:              "en",
			Acquirer:              credential.Acquirer,
			AuthType:              firstNonEmptyString(credential.AuthType, "AK"),
			BearerTokenHeaderKey:  credential.BearerTokenHeaderKey,
			CredentialPrincipal:   credential.Principal,
			ExpectedAccountID:     credential.ExpectedAccountID,
			ExpectedIdentityType:  credential.ExpectedIdentityType,
			IdentityPolicy:        credential.IdentityPolicy,
			PinCredentialIdentity: credentialAcquirerIsRenewable(credential.Acquirer),
		}, nil
	}
	aliyunConfigPath := ecconfig.AliyunConfigPath(getenv)
	privateCredentialCacheRoot := ""

	ecctlConfig, hasEcctl, err := loadConfigObject(configPath)
	if err != nil {
		return resolvedOpenAPIProfile{}, ecerrors.Client("InvalidConfig", callerSanitizeCloudError(err))
	}
	aliyunConfig, hasAliyun, err := loadConfigObject(aliyunConfigPath)
	if err != nil {
		return resolvedOpenAPIProfile{}, ecerrors.Client("InvalidConfig", callerSanitizeCloudError(err))
	}

	selected := selectedConfigProfile(profileName, ecctlConfig, hasEcctl, aliyunConfig, hasAliyun)
	selectedByStoredCurrent := profileName == "" && selectedConfigCurrent(ecctlConfig, hasEcctl, aliyunConfig, hasAliyun) != ""
	effective, _, err := ecconfig.EffectiveProfile(selected, configPath, aliyunConfigPath)
	if err != nil {
		return resolvedOpenAPIProfile{}, ecerrors.Client("InvalidConfig", callerSanitizeCloudError(err))
	}
	resolved := resolvedOpenAPIProfile{
		Name:     firstNonEmptyString(selected, effective.Name, ecconfig.DefaultProfileName),
		Language: firstNonEmptyString(effective.Language, "en"),
	}
	if region.Value != "" {
		resolved.RegionID = region.Value
		resolved.RegionSource = region.Source
	} else if effective.Region != "" {
		resolved.RegionID = effective.Region
		resolved.RegionSource = ecconfig.RegionSourceProfile
	} else if value := strings.TrimSpace(getenv("ECCTL_REGION")); value != "" {
		resolved.RegionID = value
		resolved.RegionSource = ecconfig.RegionSourceECCTL
	} else if value := credentialRegionFromEnvironment(getenv); value != "" {
		resolved.RegionID = value
		resolved.RegionSource = ecconfig.RegionSourceAlibabaEnv
	}

	var credential resolvedCredential
	ecctlProfile, hasEcctlProfile := configProfile(ecctlConfig, selected)
	aliyunProfile, hasAliyunProfile := configProfile(aliyunConfig, selected)
	switch {
	case hasEcctlProfile && profileHasStaticCredentialOverride(ecctlProfile):
		credential, err = resolveStaticCredential(effective, getenv, "ecctl-profile", selected, ecctlProfile)
	case hasAliyunProfile:
		credential, err = resolveAliyunProfileCredentialWithCache(aliyunConfig, aliyunProfile, selected, aliyunConfigPath, privateCredentialCacheRoot, getenv, map[string]bool{})
	case hasEcctlProfile:
		credential, err = resolveEnvironmentCredential(getenv)
	case profileName != "" || selectedByStoredCurrent:
		err = ecerrors.Client("ProfileNotFound", fmt.Sprintf("profile %s not found", selected))
	default:
		credential, err = resolveEnvironmentCredential(getenv)
	}
	if err != nil {
		return resolvedOpenAPIProfile{}, credentialResolutionError(err)
	}
	resolved.Acquirer = credential.Acquirer
	resolved.Mode = credential.Mode
	resolved.AuthType = firstNonEmptyString(credential.AuthType, "AK")
	resolved.BearerTokenHeaderKey = credential.BearerTokenHeaderKey
	resolved.CredentialPrincipal = credential.Principal
	resolved.ExpectedAccountID = credential.ExpectedAccountID
	resolved.ExpectedIdentityType = credential.ExpectedIdentityType
	resolved.IdentityPolicy = credential.IdentityPolicy
	resolved.PinCredentialIdentity = credentialAcquirerIsRenewable(credential.Acquirer)
	setCredentialFallbackRegion(resolved.Acquirer, resolved.RegionID)
	return resolved, nil
}

func selectedConfigCurrent(ecctlConfig map[string]any, hasEcctl bool, aliyunConfig map[string]any, hasAliyun bool) string {
	if hasEcctl {
		if current, _ := ecctlConfig["current"].(string); strings.TrimSpace(current) != "" {
			return strings.TrimSpace(current)
		}
	}
	if hasAliyun {
		if current, _ := aliyunConfig["current"].(string); strings.TrimSpace(current) != "" {
			return strings.TrimSpace(current)
		}
	}
	return ""
}

func resolveAliyunProfileCredential(config map[string]any, profile map[string]any, name, configPath string, getenv func(string) string, seen map[string]bool) (resolvedCredential, error) {
	return resolveAliyunProfileCredentialWithCache(config, profile, name, configPath, "", getenv, seen)
}

func resolveAliyunProfileCredentialWithCache(config map[string]any, profile map[string]any, name, configPath, cacheRoot string, getenv func(string) string, seen map[string]bool) (resolvedCredential, error) {
	if seen[name] {
		return resolvedCredential{}, fmt.Errorf("credential profile chain contains a cycle at %s", name)
	}
	seen[name] = true
	defer delete(seen, name)

	rawMode := stringMapField(profile, "mode")
	mode := normalizeCredentialMode(rawMode)
	if rawMode != "" && mode == "" {
		return resolvedCredential{}, ecerrors.Client("UnsupportedCredentialMode", fmt.Sprintf("credential mode %s is not supported", rawMode), ecerrors.WithAcceptedValues(supportedCredentialModeValues...))
	}
	if mode == "" {
		mode = inferCredentialMode(profile, getenv)
	}
	if mode == "" {
		return resolvedCredential{}, ecerrors.Client("MissingCredentials", "Alibaba Cloud credentials are required")
	}

	principal := credentialProfilePrincipal(name, mode, profile)
	switch mode {
	case credentialModeAK, credentialModeStsToken:
		return staticCredentialFromValues(
			firstNonEmptyString(stringMapField(profile, "access_key_id"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABACLOUD_ACCESS_KEY_ID", "ALICLOUD_ACCESS_KEY_ID", "ACCESS_KEY_ID")),
			firstNonEmptyString(stringMapField(profile, "access_key_secret"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABACLOUD_ACCESS_KEY_SECRET", "ALICLOUD_ACCESS_KEY_SECRET", "ACCESS_KEY_SECRET")),
			firstNonEmptyString(stringMapField(profile, "sts_token"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_SECURITY_TOKEN", "ALIBABACLOUD_SECURITY_TOKEN", "ALICLOUD_SECURITY_TOKEN", "SECURITY_TOKEN")),
			mode,
			principal,
			credentialProfileExpiration(profile),
		)
	case credentialModeRamRoleArn:
		source, err := staticCredentialFromValues(
			firstNonEmptyString(stringMapField(profile, "access_key_id"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABACLOUD_ACCESS_KEY_ID", "ALICLOUD_ACCESS_KEY_ID", "ACCESS_KEY_ID")),
			firstNonEmptyString(stringMapField(profile, "access_key_secret"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABACLOUD_ACCESS_KEY_SECRET", "ALICLOUD_ACCESS_KEY_SECRET", "ACCESS_KEY_SECRET")),
			firstNonEmptyString(stringMapField(profile, "sts_token"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_SECURITY_TOKEN", "ALIBABACLOUD_SECURITY_TOKEN", "ALICLOUD_SECURITY_TOKEN", "SECURITY_TOKEN")),
			credentialModeAK,
			firstNonEmptyString(stringMapField(profile, "access_key_id"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABACLOUD_ACCESS_KEY_ID", "ALICLOUD_ACCESS_KEY_ID", "ACCESS_KEY_ID")),
			credentialProfileExpiration(profile),
		)
		if err != nil {
			return resolvedCredential{}, err
		}
		return assumeRoleCredential(source.Acquirer, profile, getenv, mode)
	case credentialModeChainableRamRoleArn:
		sourceName := stringMapField(profile, "source_profile")
		if sourceName == "" {
			return resolvedCredential{}, errors.New("source_profile is required for ChainableRamRoleArn")
		}
		sourceProfile, ok := configProfile(config, sourceName)
		if !ok {
			return resolvedCredential{}, fmt.Errorf("source profile %s not found", sourceName)
		}
		source, err := resolveAliyunProfileCredentialWithCache(config, sourceProfile, sourceName, configPath, cacheRoot, getenv, seen)
		if err != nil {
			return resolvedCredential{}, err
		}
		return assumeRoleCredential(source.Acquirer, profile, getenv, mode)
	case credentialModeEcsRamRole:
		roleName := firstNonEmptyString(stringMapField(profile, "ram_role_name"), getenv("ALIBABA_CLOUD_ECS_METADATA"))
		return resolvedCredential{
			Acquirer: &ecsCredentialAcquirer{roleName: roleName, disableIMDSv1: environmentBool(getenv, "ALIBABA_CLOUD_IMDSV1_DISABLED"), disabled: environmentBool(getenv, "ALIBABA_CLOUD_ECS_METADATA_DISABLED")},
			Mode:     mode, AuthType: "AK", Principal: firstNonEmptyString(roleName, principal),
			IdentityPolicy: identityPolicyFromProfile(profile, getenv),
		}, nil
	case credentialModeOIDC:
		providerARN := firstNonEmptyString(stringMapField(profile, "oidc_provider_arn"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_OIDC_PROVIDER_ARN", "ALIBABACLOUD_OIDC_PROVIDER_ARN"))
		tokenFile := firstNonEmptyString(stringMapField(profile, "oidc_token_file"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_OIDC_TOKEN_FILE", "ALIBABACLOUD_OIDC_TOKEN_FILE"))
		roleARN := firstNonEmptyString(stringMapField(profile, "ram_role_arn"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ROLE_ARN", "ALIBABACLOUD_ROLE_ARN"))
		if providerARN == "" || tokenFile == "" || roleARN == "" {
			return resolvedCredential{}, errors.New("oidc_provider_arn, oidc_token_file, and ram_role_arn are required for OIDC credentials")
		}
		roleAccountID, _, err := parseRAMRoleARN(roleARN)
		if err != nil {
			return resolvedCredential{}, err
		}
		providerAccountID, _, err := parseOIDCProviderARN(providerARN)
		if err != nil {
			return resolvedCredential{}, err
		}
		if providerAccountID != roleAccountID {
			return resolvedCredential{}, errors.New("OIDC provider ARN and RAM role ARN must belong to the same account")
		}
		stsEndpoint, stsRegion, enableVPC := credentialSTSSettings(profile, getenv)
		return resolvedCredential{
			Acquirer: &oidcCredentialAcquirer{
				mode: mode, providerARN: providerARN, tokenFile: tokenFile, roleArn: roleARN,
				roleSessionName: firstNonEmptyString(stringMapField(profile, "ram_session_name"), getenv("ALIBABA_CLOUD_ROLE_SESSION_NAME")),
				durationSeconds: intMapField(profile, "expired_seconds"), policy: stringMapField(profile, "policy"),
				stsEndpoint: stsEndpoint, stsRegion: stsRegion, enableVPC: enableVPC,
			},
			Mode: mode, AuthType: "AK", Principal: firstNonEmptyString(roleARN, principal),
			ExpectedAccountID: roleAccountID, ExpectedIdentityType: "AssumedRoleUser",
			IdentityPolicy: credentialIdentityPolicy{stsRegion: stsRegion, enableVPC: enableVPC},
		}, nil
	case credentialModeCloudSSO:
		provider, err := newCloudSSOProfileCredentialsProvider(profile, name, configPath, nil, cacheRoot)
		if err != nil {
			return resolvedCredential{}, err
		}
		provider.identityPolicy = identityPolicyFromProfile(profile, getenv)
		return resolvedCredential{
			Acquirer: provider, Mode: mode, AuthType: "AK", Principal: principal,
			ExpectedAccountID: stringMapField(profile, "cloud_sso_account_id"),
			IdentityPolicy:    provider.identityPolicy,
		}, nil
	case credentialModeOAuth:
		provider, err := newOAuthProfileCredentialsProvider(profile, name, configPath, cacheRoot)
		if err != nil {
			return resolvedCredential{}, err
		}
		return resolvedCredential{Acquirer: provider, Mode: mode, AuthType: "AK", Principal: principal, IdentityPolicy: identityPolicyFromProfile(profile, getenv)}, nil
	case credentialModeExternal:
		if externalCredentialSourcesDisabled(getenv) {
			return resolvedCredential{}, ecerrors.Client("CredentialSourceDisabled", credentialExternalDisabledEnv+" disables External credentials")
		}
		provider, err := newSafeExternalCredentialsProvider(stringMapField(profile, "process_command"), getenv)
		if err != nil {
			return resolvedCredential{}, err
		}
		return resolvedCredential{Acquirer: provider, Mode: mode, AuthType: "AK", Principal: principal, IdentityPolicy: identityPolicyFromProfile(profile, getenv)}, nil
	case credentialModeCredentialsURI:
		if externalCredentialSourcesDisabled(getenv) {
			return resolvedCredential{}, ecerrors.Client("CredentialSourceDisabled", credentialExternalDisabledEnv+" disables CredentialsURI credentials")
		}
		credentialURI := firstNonEmptyString(stringMapField(profile, "credentials_uri"), getenv("ALIBABA_CLOUD_CREDENTIALS_URI"))
		provider, err := newSafeURLCredentialsProvider(credentialURI, nil)
		if err != nil {
			return resolvedCredential{}, err
		}
		return resolvedCredential{Acquirer: provider, Mode: mode, AuthType: "AK", Principal: principal, IdentityPolicy: identityPolicyFromProfile(profile, getenv)}, nil
	case credentialModeBearerToken:
		token := firstNonEmptyString(stringMapField(profile, "bearer_token"), getenv("ALIBABA_CLOUD_BEARER_TOKEN"))
		header := firstNonEmptyString(stringMapField(profile, "bearer_token_header_key"), getenv("ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY"))
		return bearerCredential(token, header, mode)
	default:
		return resolvedCredential{}, ecerrors.Client("UnsupportedCredentialMode", fmt.Sprintf("credential mode %s is not supported", mode), ecerrors.WithAcceptedValues(supportedCredentialModeValues...))
	}
}

func resolveStaticCredential(profile ecconfig.Profile, getenv func(string) string, source, name string, raw map[string]any) (resolvedCredential, error) {
	mode := credentialModeAK
	if profile.SecurityToken != "" {
		mode = credentialModeStsToken
	}
	return staticCredentialFromValues(
		firstNonEmptyString(profile.AccessKeyID, credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABACLOUD_ACCESS_KEY_ID", "ALICLOUD_ACCESS_KEY_ID", "ACCESS_KEY_ID")),
		firstNonEmptyString(profile.AccessKeySecret, credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABACLOUD_ACCESS_KEY_SECRET", "ALICLOUD_ACCESS_KEY_SECRET", "ACCESS_KEY_SECRET")),
		firstNonEmptyString(profile.SecurityToken, credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_SECURITY_TOKEN", "ALIBABACLOUD_SECURITY_TOKEN", "ALICLOUD_SECURITY_TOKEN", "SECURITY_TOKEN")),
		mode,
		firstNonEmptyString(profile.AccessKeyID, stringMapField(raw, "access_key_id")),
		credentialProfileExpiration(raw),
	)
}

func resolveEnvironmentCredential(getenv func(string) string) (resolvedCredential, error) {
	accessKeyID := credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABACLOUD_ACCESS_KEY_ID", "ALICLOUD_ACCESS_KEY_ID", "ACCESS_KEY_ID")
	accessKeySecret := credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABACLOUD_ACCESS_KEY_SECRET", "ALICLOUD_ACCESS_KEY_SECRET", "ACCESS_KEY_SECRET")
	securityToken := credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_SECURITY_TOKEN", "ALIBABACLOUD_SECURITY_TOKEN", "ALICLOUD_SECURITY_TOKEN", "SECURITY_TOKEN")
	roleArn := credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ROLE_ARN", "ALIBABACLOUD_ROLE_ARN")
	if accessKeyID != "" || accessKeySecret != "" {
		if accessKeyID == "" || accessKeySecret == "" {
			return resolvedCredential{}, errors.New("both ALIBABA_CLOUD_ACCESS_KEY_ID and ALIBABA_CLOUD_ACCESS_KEY_SECRET are required")
		}
		if roleArn == "" {
			mode := credentialModeAK
			if securityToken != "" {
				mode = credentialModeStsToken
			}
			return staticCredentialFromValues(accessKeyID, accessKeySecret, securityToken, mode, accessKeyID)
		}
		source, err := staticCredentialFromValues(accessKeyID, accessKeySecret, securityToken, credentialModeAK, accessKeyID)
		if err != nil {
			return resolvedCredential{}, err
		}
		profile := map[string]any{
			"ram_role_arn":     roleArn,
			"ram_session_name": getenv("ALIBABA_CLOUD_ROLE_SESSION_NAME"),
			"external_id":      credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_EXTERNAL_ID", "ALIBABACLOUD_EXTERNAL_ID"),
			"sts_endpoint":     getenv("ALIBABA_CLOUD_STS_ENDPOINT"),
		}
		return assumeRoleCredential(source.Acquirer, profile, getenv, credentialModeRamRoleArn)
	}

	oidcProviderArn := credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_OIDC_PROVIDER_ARN", "ALIBABACLOUD_OIDC_PROVIDER_ARN")
	oidcTokenFile := credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_OIDC_TOKEN_FILE", "ALIBABACLOUD_OIDC_TOKEN_FILE")
	if oidcProviderArn != "" && oidcTokenFile != "" && roleArn != "" {
		roleAccountID, _, err := parseRAMRoleARN(roleArn)
		if err != nil {
			return resolvedCredential{}, err
		}
		providerAccountID, _, err := parseOIDCProviderARN(oidcProviderArn)
		if err != nil {
			return resolvedCredential{}, err
		}
		if providerAccountID != roleAccountID {
			return resolvedCredential{}, errors.New("OIDC provider ARN and RAM role ARN must belong to the same account")
		}
		stsEndpoint, stsRegion, enableVPC := credentialSTSSettings(nil, getenv)
		return resolvedCredential{
			Acquirer: &oidcCredentialAcquirer{
				mode: credentialModeOIDC, providerARN: oidcProviderArn, tokenFile: oidcTokenFile, roleArn: roleArn,
				roleSessionName: getenv("ALIBABA_CLOUD_ROLE_SESSION_NAME"), stsEndpoint: stsEndpoint,
				stsRegion: stsRegion, enableVPC: enableVPC,
			},
			Mode: credentialModeOIDC, AuthType: "AK", Principal: roleArn,
			ExpectedAccountID: roleAccountID, ExpectedIdentityType: "AssumedRoleUser",
			IdentityPolicy: credentialIdentityPolicy{stsRegion: stsRegion, enableVPC: enableVPC},
		}, nil
	}
	roleName := getenv("ALIBABA_CLOUD_ECS_METADATA")
	if roleName != "" {
		return resolvedCredential{
			Acquirer: &ecsCredentialAcquirer{roleName: roleName, disableIMDSv1: environmentBool(getenv, "ALIBABA_CLOUD_IMDSV1_DISABLED"), disabled: environmentBool(getenv, "ALIBABA_CLOUD_ECS_METADATA_DISABLED")},
			Mode:     credentialModeEcsRamRole, AuthType: "AK", Principal: roleName,
			IdentityPolicy: identityPolicyFromProfile(nil, getenv),
		}, nil
	}
	if oidcProviderArn != "" || oidcTokenFile != "" || roleArn != "" {
		return resolvedCredential{}, errors.New("ALIBABA_CLOUD_OIDC_PROVIDER_ARN, ALIBABA_CLOUD_OIDC_TOKEN_FILE, and ALIBABA_CLOUD_ROLE_ARN are all required for OIDC credentials")
	}

	credentialURI := getenv("ALIBABA_CLOUD_CREDENTIALS_URI")
	if credentialURI != "" {
		if externalCredentialSourcesDisabled(getenv) {
			return resolvedCredential{}, ecerrors.Client("CredentialSourceDisabled", credentialExternalDisabledEnv+" disables CredentialsURI credentials")
		}
		provider, err := newSafeURLCredentialsProvider(credentialURI, nil)
		if err != nil {
			return resolvedCredential{}, err
		}
		return resolvedCredential{Acquirer: provider, Mode: credentialModeCredentialsURI, AuthType: "AK", Principal: credentialSourcePrincipal(credentialURI), IdentityPolicy: identityPolicyFromProfile(nil, getenv)}, nil
	}

	if token := getenv("ALIBABA_CLOUD_BEARER_TOKEN"); token != "" {
		return bearerCredential(token, getenv("ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY"), credentialModeBearerToken)
	}
	return resolvedCredential{}, ecerrors.Client("MissingCredentials", "Alibaba Cloud credentials are required")
}

func staticCredentialFromValues(accessKeyID, accessKeySecret, securityToken, mode, principal string, expirations ...time.Time) (resolvedCredential, error) {
	if accessKeyID == "" || accessKeySecret == "" {
		return resolvedCredential{}, ecerrors.Client("MissingCredentials", "Alibaba Cloud access key ID and secret are required")
	}
	typeName := "access_key"
	if mode == credentialModeStsToken || securityToken != "" {
		if securityToken == "" {
			return resolvedCredential{}, ecerrors.Client("MissingCredentials", "Alibaba Cloud STS security token is required")
		}
		mode = credentialModeStsToken
		typeName = "sts"
	}
	expiresAt := time.Time{}
	if typeName == "sts" && len(expirations) > 0 {
		expiresAt = expirations[0]
	}
	return resolvedCredential{
		Acquirer: &staticCredentialAcquirer{snapshot: credentialSnapshot{
			AccessKeyID: accessKeyID, AccessKeySecret: accessKeySecret,
			SecurityToken: securityToken, ProviderName: "static", Type: typeName, ExpiresAt: expiresAt,
		}},
		Mode: mode, AuthType: "AK", Principal: firstNonEmptyString(principal, accessKeyID),
	}, nil
}

func credentialProfileExpiration(profile map[string]any) time.Time {
	expiration := int64MapField(profile, "sts_expiration")
	if expiration <= 0 {
		return time.Time{}
	}
	return time.Unix(expiration, 0)
}

func assumeRoleCredential(source credentialAcquirer, profile map[string]any, getenv func(string) string, mode string) (resolvedCredential, error) {
	if source == nil {
		return resolvedCredential{}, errors.New("source credential is required for RAM role assumption")
	}
	roleArn := firstNonEmptyString(stringMapField(profile, "ram_role_arn"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ROLE_ARN", "ALIBABACLOUD_ROLE_ARN"))
	if roleArn == "" {
		return resolvedCredential{}, errors.New("ram_role_arn is required")
	}
	accountID, _, err := parseRAMRoleARN(roleArn)
	if err != nil {
		return resolvedCredential{}, err
	}
	stsEndpoint, stsRegion, enableVPC := credentialSTSSettings(profile, getenv)
	return resolvedCredential{
		Acquirer: &ramRoleCredentialAcquirer{
			source: source, mode: mode, roleArn: roleArn,
			roleSessionName: firstNonEmptyString(stringMapField(profile, "ram_session_name"), getenv("ALIBABA_CLOUD_ROLE_SESSION_NAME")),
			durationSeconds: intMapField(profile, "expired_seconds"), policy: stringMapField(profile, "policy"),
			externalID:  firstNonEmptyString(stringMapField(profile, "external_id"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_EXTERNAL_ID", "ALIBABACLOUD_EXTERNAL_ID")),
			stsEndpoint: stsEndpoint, stsRegion: stsRegion, enableVPC: enableVPC,
		},
		Mode: mode, AuthType: "AK", Principal: roleArn,
		ExpectedAccountID: accountID, ExpectedIdentityType: "AssumedRoleUser",
		IdentityPolicy: credentialIdentityPolicy{stsRegion: stsRegion, enableVPC: enableVPC},
	}, nil
}

func credentialSTSSettings(profile map[string]any, getenv func(string) string) (endpoint, region string, enableVPC bool) {
	endpoint = stringMapField(profile, "sts_endpoint")
	region = stringMapField(profile, "sts_region")
	enableVPC = boolMapField(profile, "enable_vpc")
	if getenv == nil {
		return endpoint, region, enableVPC
	}
	endpoint = firstNonEmptyString(endpoint, getenv("ALIBABA_CLOUD_STS_ENDPOINT"))
	region = firstNonEmptyString(region, getenv("ALIBABA_CLOUD_STS_REGION"))
	enableVPC = enableVPC || environmentBool(getenv, "ALIBABA_CLOUD_VPC_ENDPOINT_ENABLED")
	return endpoint, region, enableVPC
}

type ramRoleCredentialAcquirer struct {
	source          credentialAcquirer
	mode            string
	roleArn         string
	roleSessionName string
	durationSeconds int
	policy          string
	externalID      string
	stsEndpoint     string
	stsRegion       string
	fallbackRegion  string
	enableVPC       bool
	proxy           string

	gate           contextGate
	provider       credentialproviders.CredentialsProvider
	sourceProvider *credentialAcquirerProvider
}

func (*ramRoleCredentialAcquirer) Renewable() bool { return true }

func (a *ramRoleCredentialAcquirer) ForOperation(ctx context.Context) (credentialAcquirer, error) {
	if a == nil {
		return nil, errors.New("RAM role credential source is unavailable")
	}
	source, err := credentialAcquirerForOperation(ctx, a.source)
	if err != nil {
		return nil, err
	}
	return &ramRoleCredentialAcquirer{
		source: source, mode: a.mode, roleArn: a.roleArn, roleSessionName: a.roleSessionName,
		durationSeconds: a.durationSeconds, policy: a.policy, externalID: a.externalID,
		stsEndpoint: a.stsEndpoint, stsRegion: a.stsRegion, fallbackRegion: a.fallbackRegion,
		enableVPC: a.enableVPC, proxy: a.proxy,
	}, nil
}

type credentialAcquirerProvider struct {
	acquirer credentialAcquirer
	ctx      context.Context
	name     string
}

func (p *credentialAcquirerProvider) GetCredentials() (*credentialproviders.Credentials, error) {
	if p == nil || p.acquirer == nil {
		return nil, errors.New("source credential provider is unavailable")
	}
	snapshot, err := p.acquirer.Acquire(p.ctx)
	if err != nil {
		return nil, err
	}
	if err := validateCredentialLifetime(p.ctx, p.acquirer, snapshot); err != nil {
		return nil, err
	}
	return providerCredentialsFromSnapshot(snapshot), nil
}

func (p *credentialAcquirerProvider) GetProviderName() string {
	if p == nil || p.name == "" {
		return "ecctl_source"
	}
	return p.name
}

func (a *ramRoleCredentialAcquirer) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	if err := a.gate.Lock(ctx); err != nil {
		return nil, err
	}
	defer a.gate.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if !credentialAcquirerIsRenewable(a.source) {
		source, err := a.source.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		if err := validateCredentialLifetime(ctx, a.source, source); err != nil {
			return nil, err
		}
	}
	if a.provider == nil {
		stsEndpoint, stsRegion, endpointErr := effectiveSTSEndpoint(a.stsEndpoint, a.stsRegion, a.enableVPC, firstNonEmptyString(credentialOperationRegion(ctx), a.fallbackRegion))
		if endpointErr != nil {
			return nil, &credentialProviderError{mode: a.mode, err: endpointErr}
		}
		httpOptions := credentialProviderHTTPOptions(ctx, 5*time.Second, 10*time.Second)
		httpOptions.Proxy = a.proxy
		a.sourceProvider = &credentialAcquirerProvider{acquirer: a.source, name: "ecctl_source"}
		provider, err := credentialproviders.NewRAMRoleARNCredentialsProviderBuilder().
			WithCredentialsProvider(a.sourceProvider).
			WithRoleArn(a.roleArn).
			WithRoleSessionName(a.roleSessionName).
			WithDurationSeconds(a.durationSeconds).
			WithPolicy(a.policy).
			WithExternalId(a.externalID).
			WithStsRegionId(stsRegion).
			WithEnableVpc(a.enableVPC).
			WithStsEndpoint(stsEndpoint).
			WithHttpOptions(httpOptions).
			Build()
		if err != nil {
			return nil, &credentialProviderError{mode: a.mode, err: err}
		}
		a.provider = provider
	}
	a.sourceProvider.ctx = ctx
	return (&credentialsProviderAcquirer{provider: a.provider, mode: a.mode}).Acquire(ctx)
}

type oidcCredentialAcquirer struct {
	mode            string
	providerARN     string
	tokenFile       string
	roleArn         string
	roleSessionName string
	durationSeconds int
	policy          string
	stsEndpoint     string
	stsRegion       string
	fallbackRegion  string
	enableVPC       bool
	proxy           string

	gate     contextGate
	provider credentialproviders.CredentialsProvider
}

func (*oidcCredentialAcquirer) Renewable() bool { return true }

func (a *oidcCredentialAcquirer) ForOperation(context.Context) (credentialAcquirer, error) {
	if a == nil {
		return nil, errors.New("OIDC credential source is unavailable")
	}
	return &oidcCredentialAcquirer{
		mode: a.mode, providerARN: a.providerARN, tokenFile: a.tokenFile, roleArn: a.roleArn,
		roleSessionName: a.roleSessionName, durationSeconds: a.durationSeconds, policy: a.policy,
		stsEndpoint: a.stsEndpoint, stsRegion: a.stsRegion, fallbackRegion: a.fallbackRegion,
		enableVPC: a.enableVPC, proxy: a.proxy,
	}, nil
}

func (a *oidcCredentialAcquirer) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	if err := a.gate.Lock(ctx); err != nil {
		return nil, err
	}
	defer a.gate.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if a.provider == nil {
		stsEndpoint, stsRegion, endpointErr := effectiveSTSEndpoint(a.stsEndpoint, a.stsRegion, a.enableVPC, firstNonEmptyString(credentialOperationRegion(ctx), a.fallbackRegion))
		if endpointErr != nil {
			return nil, &credentialProviderError{mode: a.mode, err: endpointErr}
		}
		httpOptions := credentialProviderHTTPOptions(ctx, 5*time.Second, 10*time.Second)
		httpOptions.Proxy = a.proxy
		provider, err := credentialproviders.NewOIDCCredentialsProviderBuilder().
			WithOIDCProviderARN(a.providerARN).
			WithOIDCTokenFilePath(a.tokenFile).
			WithRoleArn(a.roleArn).
			WithRoleSessionName(a.roleSessionName).
			WithDurationSeconds(a.durationSeconds).
			WithPolicy(a.policy).
			WithStsRegionId(stsRegion).
			WithEnableVpc(a.enableVPC).
			WithSTSEndpoint(stsEndpoint).
			WithHttpOptions(httpOptions).
			Build()
		if err != nil {
			return nil, &credentialProviderError{mode: a.mode, err: err}
		}
		a.provider = provider
	}
	return (&credentialsProviderAcquirer{provider: a.provider, mode: a.mode}).Acquire(ctx)
}

func effectiveSTSEndpoint(explicitEndpoint, explicitRegion string, enableVPC bool, operationRegion string) (string, string, error) {
	region := strings.TrimSpace(explicitRegion)
	if region != "" && !ecconfig.ValidRegion(region) {
		return "", "", fmt.Errorf("invalid STS region %s", region)
	}
	if strings.TrimSpace(explicitEndpoint) != "" {
		endpoint, err := validateExplicitSTSEndpoint(explicitEndpoint)
		if err != nil {
			return "", "", err
		}
		return endpoint, region, nil
	}
	if region == "" && enableVPC {
		region = strings.TrimSpace(operationRegion)
	}
	if region == "" {
		if enableVPC {
			return "", "", errors.New("STS region is required when the VPC endpoint is enabled")
		}
		// Always materialize the public global endpoint. Passing an empty target
		// lets credentials-go re-read process environment outside ecctl's
		// validation boundary.
		return "sts.aliyuncs.com", "", nil
	}
	if !ecconfig.ValidRegion(region) {
		return "", "", fmt.Errorf("invalid STS region %s", region)
	}
	prefix := "sts"
	if enableVPC {
		prefix = "sts-vpc"
	}
	return prefix + "." + region + ".aliyuncs.com", region, nil
}

func validateExplicitSTSEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("STS endpoint is required")
	}
	parsedRaw := raw
	if !strings.Contains(parsedRaw, "://") {
		parsedRaw = "https://" + parsedRaw
	}
	parsed, err := url.Parse(parsedRaw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("STS endpoint must be an HTTPS host without user information, path, query, or fragment")
	}
	if parsed.Hostname() == "" {
		return "", errors.New("STS endpoint host is required")
	}
	return parsed.Host, nil
}

func setCredentialFallbackRegion(acquirer credentialAcquirer, region string) {
	switch typed := acquirer.(type) {
	case *ramRoleCredentialAcquirer:
		typed.fallbackRegion = region
	case *oidcCredentialAcquirer:
		typed.fallbackRegion = region
	}
}

func bearerCredential(token, header, mode string) (resolvedCredential, error) {
	if token == "" {
		return resolvedCredential{}, ecerrors.Client("MissingCredentials", "Alibaba Cloud bearer token is required")
	}
	if strings.ContainsAny(token, "\r\n\x00") {
		return resolvedCredential{}, errors.New("bearer token contains invalid control characters")
	}
	if header == "" {
		header = defaultBearerTokenHeaderKey
	}
	if err := validateBearerTokenHeaderKey(header); err != nil {
		return resolvedCredential{}, err
	}
	return resolvedCredential{
		Acquirer: &staticCredentialAcquirer{snapshot: credentialSnapshot{BearerToken: token, ProviderName: "bearer", Type: "bearer"}},
		Mode:     mode, AuthType: "Anonymous", BearerTokenHeaderKey: header,
	}, nil
}

func validateBearerTokenHeaderKey(header string) error {
	if header == "" {
		return errors.New("bearer token header key is required")
	}
	for _, r := range header {
		if r > unicode.MaxASCII || !isHTTPTokenRune(r) {
			return errors.New("bearer token header key must be a valid HTTP field name")
		}
	}
	return nil
}

func isHTTPTokenRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)
}

type credentialProviderError struct {
	mode string
	err  error
}

func (e *credentialProviderError) Error() string { return e.err.Error() }
func (e *credentialProviderError) Unwrap() error { return e.err }

type credentialHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func newCredentialHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func rejectCredentialRedirects(client credentialHTTPClient) credentialHTTPClient {
	if httpClient, ok := client.(*http.Client); ok {
		clone := *httpClient
		clone.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		return &clone
	}
	return client
}

type safeURLCredentialsProvider struct {
	uri    string
	client credentialHTTPClient

	gate      contextGate
	cached    *credentialproviders.Credentials
	expiresAt time.Time
}

func (*safeURLCredentialsProvider) Renewable() bool { return true }

type urlCredentialPayload struct {
	Code            string `json:"Code"`
	AccessKeyID     string `json:"AccessKeyId"`
	AccessKeySecret string `json:"AccessKeySecret"`
	SecurityToken   string `json:"SecurityToken"`
	Expiration      string `json:"Expiration"`
}

func newSafeURLCredentialsProvider(rawURI string, client credentialHTTPClient) (*safeURLCredentialsProvider, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("credentials URI must be an HTTP(S) URL")
	}
	if parsed.Scheme == "http" {
		ip := net.ParseIP(parsed.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("credentials URI requires HTTPS unless it uses a literal loopback address")
		}
	}
	if client == nil {
		client = newCredentialHTTPClient(15 * time.Second)
	} else {
		client = rejectCredentialRedirects(client)
	}
	return &safeURLCredentialsProvider{uri: rawURI, client: client}, nil
}

func (p *safeURLCredentialsProvider) GetCredentials() (*credentialproviders.Credentials, error) {
	credentials, _, err := p.getCredentials(context.Background())
	return credentials, err
}

func (p *safeURLCredentialsProvider) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	credentials, expiresAt, err := p.getCredentials(ctx)
	if err != nil {
		return nil, &credentialProviderError{mode: credentialModeCredentialsURI, err: err}
	}
	return snapshotFromProviderCredentialsWithExpiration(credentials, expiresAt)
}

func (p *safeURLCredentialsProvider) getCredentials(ctx context.Context) (*credentialproviders.Credentials, time.Time, error) {
	if err := p.gate.Lock(ctx); err != nil {
		return nil, time.Time{}, err
	}
	defer p.gate.Unlock()
	if p.cached != nil && !p.expiresAt.IsZero() && time.Until(p.expiresAt) > credentialRefreshWindow {
		copy := *p.cached
		return &copy, p.expiresAt, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.uri, nil)
	if err != nil {
		return nil, time.Time{}, errors.New("credentials URI request is invalid")
	}
	response, err := p.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, time.Time{}, ctx.Err()
		}
		return nil, time.Time{}, fmt.Errorf("credential source %s failed", sanitizedCredentialSource(p.uri))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("credential source %s returned an unreadable response", sanitizedCredentialSource(p.uri))
	}
	if response.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("credential source %s returned HTTP %d", sanitizedCredentialSource(p.uri), response.StatusCode)
	}
	var payload urlCredentialPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, time.Time{}, fmt.Errorf("credential source %s returned invalid JSON", sanitizedCredentialSource(p.uri))
	}
	if payload.Code != "Success" || payload.AccessKeyID == "" || payload.AccessKeySecret == "" || payload.SecurityToken == "" {
		return nil, time.Time{}, fmt.Errorf("credential source %s returned incomplete credentials", sanitizedCredentialSource(p.uri))
	}
	credentials := &credentialproviders.Credentials{
		AccessKeyId:     payload.AccessKeyID,
		AccessKeySecret: payload.AccessKeySecret,
		SecurityToken:   payload.SecurityToken,
		ProviderName:    p.GetProviderName(),
	}
	if payload.Expiration == "" {
		return nil, time.Time{}, fmt.Errorf("credential source %s returned an invalid expiration", sanitizedCredentialSource(p.uri))
	}
	expiresAt, parseErr := time.Parse(time.RFC3339, payload.Expiration)
	if parseErr != nil {
		return nil, time.Time{}, fmt.Errorf("credential source %s returned an invalid expiration", sanitizedCredentialSource(p.uri))
	}
	if !expiresAt.After(time.Now()) {
		return nil, time.Time{}, fmt.Errorf("credential source %s returned expired credentials", sanitizedCredentialSource(p.uri))
	}
	p.cached = credentials
	p.expiresAt = expiresAt
	copy := *credentials
	return &copy, expiresAt, nil
}

func (p *safeURLCredentialsProvider) GetProviderName() string { return "credential_uri" }

type safeExternalCredentialsProvider struct {
	processCommand string
	getenv         func(string) string

	gate      contextGate
	cached    *credentialproviders.Credentials
	expiresAt time.Time
}

func (*safeExternalCredentialsProvider) Renewable() bool { return true }

type externalCredentialPayload struct {
	Mode            string `json:"mode"`
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	SecurityToken   string `json:"sts_token"`
	Expiration      string `json:"expiration"`
}

func newSafeExternalCredentialsProvider(processCommand string, getenv func(string) string) (*safeExternalCredentialsProvider, error) {
	if strings.TrimSpace(processCommand) == "" {
		return nil, errors.New("process_command is required for External credentials")
	}
	if _, err := splitCredentialProcessCommand(processCommand, runtime.GOOS); err != nil {
		return nil, err
	}
	return &safeExternalCredentialsProvider{processCommand: processCommand, getenv: getenv}, nil
}

func (p *safeExternalCredentialsProvider) GetCredentials() (*credentialproviders.Credentials, error) {
	credentials, _, err := p.getCredentials(context.Background())
	return credentials, err
}

func (p *safeExternalCredentialsProvider) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	credentials, expiresAt, err := p.getCredentials(ctx)
	if err != nil {
		return nil, &credentialProviderError{mode: credentialModeExternal, err: err}
	}
	return snapshotFromProviderCredentialsWithExpiration(credentials, expiresAt)
}

func (p *safeExternalCredentialsProvider) getCredentials(ctx context.Context) (*credentialproviders.Credentials, time.Time, error) {
	if err := p.gate.Lock(ctx); err != nil {
		return nil, time.Time{}, err
	}
	defer p.gate.Unlock()
	if p.cached != nil && !p.expiresAt.IsZero() && time.Until(p.expiresAt) > credentialRefreshWindow {
		copy := *p.cached
		return &copy, p.expiresAt, nil
	}
	if externalCredentialSourcesDisabled(p.getenv) {
		return nil, time.Time{}, errors.New("external credential source is disabled")
	}
	args, err := splitCredentialProcessCommand(p.processCommand, runtime.GOOS)
	if err != nil {
		return nil, time.Time{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultExternalCredentialTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	stdout := &limitedCredentialBuffer{limit: 1 << 20}
	cmd.Stdout = stdout
	err = runExternalCredentialCommand(cmd)
	if ctx.Err() != nil {
		return nil, time.Time{}, ctx.Err()
	}
	if err != nil {
		return nil, time.Time{}, errors.New("external credential command failed")
	}
	var payload externalCredentialPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return nil, time.Time{}, errors.New("external credential command returned invalid JSON")
	}
	mode := normalizeCredentialMode(payload.Mode)
	if mode != credentialModeAK && mode != credentialModeStsToken {
		return nil, time.Time{}, errors.New("external credential command returned an unsupported mode")
	}
	if payload.AccessKeyID == "" || payload.AccessKeySecret == "" || (mode == credentialModeStsToken && payload.SecurityToken == "") {
		return nil, time.Time{}, errors.New("external credential command returned incomplete credentials")
	}
	credentials := &credentialproviders.Credentials{
		AccessKeyId:     payload.AccessKeyID,
		AccessKeySecret: payload.AccessKeySecret,
		SecurityToken:   payload.SecurityToken,
		ProviderName:    p.GetProviderName(),
	}
	expiresAt := time.Time{}
	if payload.Expiration != "" {
		expiration, parseErr := time.Parse(time.RFC3339, payload.Expiration)
		if parseErr != nil {
			return nil, time.Time{}, errors.New("external credential command returned an invalid expiration")
		}
		if !expiration.After(time.Now()) {
			return nil, time.Time{}, errors.New("external credential command returned expired credentials")
		}
		expiresAt = expiration
	}
	p.cached = credentials
	p.expiresAt = expiresAt
	copy := *credentials
	return &copy, expiresAt, nil
}

func (p *safeExternalCredentialsProvider) GetProviderName() string { return "external" }

type limitedCredentialBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedCredentialBuffer) Write(value []byte) (int, error) {
	if len(value) > b.limit-b.Len() {
		return 0, errors.New("external credential output exceeds size limit")
	}
	return b.Buffer.Write(value)
}

func splitCredentialProcessCommand(command, goos string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("process_command is empty")
	}
	if goos == "windows" {
		return splitWindowsCredentialProcessCommand(command)
	}
	var args []string
	var current strings.Builder
	inSingle, inDouble, hasToken := false, false, false
	flush := func() {
		if hasToken {
			args = append(args, current.String())
			current.Reset()
			hasToken = false
		}
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
			} else {
				current.WriteRune(r)
			}
		case inDouble:
			if r == '"' {
				inDouble = false
				continue
			}
			if r == '\\' && i+1 < len(runes) {
				next := runes[i+1]
				if next == '"' || next == '\\' || next == '$' || next == '`' {
					current.WriteRune(next)
					i++
					continue
				}
				if next == '\n' {
					i++
					continue
				}
			}
			current.WriteRune(r)
		default:
			if r == '\\' {
				if i+1 >= len(runes) {
					return nil, errors.New("invalid process_command: trailing backslash")
				}
				if runes[i+1] == '\n' {
					i++
					continue
				}
				hasToken = true
				current.WriteRune(runes[i+1])
				i++
				continue
			}
			if r == '\'' {
				inSingle, hasToken = true, true
				continue
			}
			if r == '"' {
				inDouble, hasToken = true, true
				continue
			}
			if unicode.IsSpace(r) {
				flush()
				continue
			}
			hasToken = true
			current.WriteRune(r)
		}
	}
	if inSingle || inDouble {
		return nil, errors.New("invalid process_command: unclosed quote")
	}
	flush()
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("process_command is empty")
	}
	return args, nil
}

// splitWindowsCredentialProcessCommand follows the CommandLineToArgvW
// backslash/double-quote rules. Single quotes have no special meaning on
// Windows, so names such as O'Brien are preserved verbatim. The resulting argv
// is passed directly to exec.CommandContext; shell metacharacters stay data.
func splitWindowsCredentialProcessCommand(command string) ([]string, error) {
	var args []string
	runes := []rune(command)
	for i := 0; i < len(runes); {
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i == len(runes) {
			break
		}
		var current strings.Builder
		inQuotes := false
		hasToken := false
		for i < len(runes) {
			if unicode.IsSpace(runes[i]) && !inQuotes {
				break
			}
			if runes[i] != '\\' {
				if runes[i] == '"' {
					inQuotes = !inQuotes
					hasToken = true
					i++
					continue
				}
				current.WriteRune(runes[i])
				hasToken = true
				i++
				continue
			}

			backslashes := 0
			for i < len(runes) && runes[i] == '\\' {
				backslashes++
				i++
			}
			if i < len(runes) && runes[i] == '"' {
				current.WriteString(strings.Repeat("\\", backslashes/2))
				if backslashes%2 == 0 {
					inQuotes = !inQuotes
				} else {
					current.WriteRune('"')
				}
				hasToken = true
				i++
				continue
			}
			current.WriteString(strings.Repeat("\\", backslashes))
			hasToken = true
		}
		if hasToken {
			args = append(args, current.String())
		}
	}
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("process_command is empty")
	}
	return args, nil
}

func credentialResolutionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ecerrors.Timeout("WaitTimeout", "credential acquisition timed out")
	}
	var appErr *ecerrors.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	if errors.Is(err, ErrCredentialLeaseExpired) {
		return ecerrors.Client("CredentialLeaseExpired", "credential lease expired during operation")
	}
	if errors.Is(err, ErrCredentialIdentityChanged) {
		return ecerrors.Client("CredentialIdentityChanged", "credential identity changed during operation")
	}
	if errors.Is(err, ErrCredentialAccountMismatch) {
		return ecerrors.Client("CredentialAccountMismatch", "credential account does not match the configured account")
	}
	if errors.Is(err, ErrCredentialStatePersistenceFailed) {
		return ecerrors.Client("CredentialStatePersistenceFailed", "renewed credential state could not be persisted safely")
	}
	rawLowerMessage := strings.ToLower(err.Error())
	message := callerSanitizeCloudError(err)
	var providerErr *credentialProviderError
	providerMode := ""
	if errors.As(err, &providerErr) {
		providerMode = providerErr.mode
	}
	interactiveTokenFailure := (providerMode == credentialModeOAuth || providerMode == credentialModeCloudSSO) &&
		(strings.Contains(rawLowerMessage, "re-login") || strings.Contains(rawLowerMessage, "access token") || strings.Contains(rawLowerMessage, "refresh token"))
	if interactiveTokenFailure {
		return ecerrors.Client("CredentialReauthenticationRequired", message)
	}
	return ecerrors.Client("InvalidCredentials", message)
}

func normalizeCredentialMode(mode string) string {
	return supportedCredentialModes[strings.ToLower(strings.TrimSpace(mode))]
}

func inferCredentialMode(profile map[string]any, getenv func(string) string) string {
	accessKeyID := firstNonEmptyString(stringMapField(profile, "access_key_id"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABACLOUD_ACCESS_KEY_ID", "ALICLOUD_ACCESS_KEY_ID", "ACCESS_KEY_ID"))
	accessKeySecret := firstNonEmptyString(stringMapField(profile, "access_key_secret"), credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABACLOUD_ACCESS_KEY_SECRET", "ALICLOUD_ACCESS_KEY_SECRET", "ACCESS_KEY_SECRET"))
	if accessKeyID != "" && accessKeySecret != "" {
		if firstNonEmptyString(stringMapField(profile, "sts_token"), getenv("ALIBABA_CLOUD_SECURITY_TOKEN")) != "" {
			return credentialModeStsToken
		}
		if firstNonEmptyString(stringMapField(profile, "ram_role_arn"), getenv("ALIBABA_CLOUD_ROLE_ARN")) != "" {
			return credentialModeRamRoleArn
		}
		return credentialModeAK
	}
	if stringMapField(profile, "ram_role_name") != "" {
		return credentialModeEcsRamRole
	}
	if stringMapField(profile, "process_command") != "" {
		return credentialModeExternal
	}
	if stringMapField(profile, "credentials_uri") != "" {
		return credentialModeCredentialsURI
	}
	if stringMapField(profile, "oidc_provider_arn") != "" {
		return credentialModeOIDC
	}
	if stringMapField(profile, "cloud_sso_sign_in_url") != "" {
		return credentialModeCloudSSO
	}
	if stringMapField(profile, "oauth_site_type") != "" {
		return credentialModeOAuth
	}
	if stringMapField(profile, "bearer_token") != "" {
		return credentialModeBearerToken
	}
	return ""
}

func profileHasStaticCredentialOverride(profile map[string]any) bool {
	return stringMapField(profile, "access_key_id") != "" || stringMapField(profile, "access_key_secret") != "" || stringMapField(profile, "sts_token") != ""
}

func ignoreCredentialProfiles(getenv func(string) string) bool {
	return strings.EqualFold(credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_IGNORE_PROFILE", "ALIBABACLOUD_IGNORE_PROFILE"), "true")
}

func externalCredentialSourcesDisabled(getenv func(string) string) bool {
	value := strings.TrimSpace(getenv(credentialExternalDisabledEnv))
	return value == "1" || strings.EqualFold(value, "true")
}

func credentialEnvironmentValue(getenv func(string) string, names ...string) string {
	if getenv == nil {
		return ""
	}
	for _, name := range names {
		if value := strings.TrimSpace(getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func credentialRegionFromEnvironment(getenv func(string) string) string {
	return credentialEnvironmentValue(getenv, "ALIBABA_CLOUD_REGION_ID", "ALIBABACLOUD_REGION_ID", "ALICLOUD_REGION_ID", "REGION_ID", "REGION")
}

func credentialProfilePrincipal(name, mode string, profile map[string]any) string {
	switch mode {
	case credentialModeAK, credentialModeStsToken:
		return stringMapField(profile, "access_key_id")
	case credentialModeRamRoleArn, credentialModeChainableRamRoleArn, credentialModeOIDC:
		return stringMapField(profile, "ram_role_arn")
	case credentialModeEcsRamRole:
		return stringMapField(profile, "ram_role_name")
	case credentialModeCloudSSO:
		return firstNonEmptyString(
			strings.Join([]string{stringMapField(profile, "cloud_sso_account_id"), stringMapField(profile, "cloud_sso_access_config")}, "/"),
			name,
		)
	case credentialModeOAuth:
		return strings.Join([]string{name, strings.ToUpper(stringMapField(profile, "oauth_site_type"))}, "/")
	case credentialModeCredentialsURI:
		return credentialSourcePrincipal(stringMapField(profile, "credentials_uri"))
	case credentialModeExternal:
		// process_command may embed secrets. The acquired AccessKey ID is added
		// to the final identity key instead of hashing command text.
		return name
	default:
		return ""
	}
}

func credentialSourceCacheKey(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func credentialIdentityCacheKey(profile resolvedOpenAPIProfile, snapshot *credentialSnapshot) string {
	if snapshot == nil || snapshot.BearerToken != "" {
		return ""
	}
	principal := profile.CredentialPrincipal
	// Dynamic sources can change principals without changing their profile
	// configuration. Include the non-secret AccessKey ID so one principal can
	// never inherit another principal's cached telemetry identity.
	if profile.Mode == credentialModeExternal || profile.Mode == credentialModeCredentialsURI || profile.Mode == credentialModeOAuth {
		principal = strings.Join([]string{principal, snapshot.AccessKeyID}, "\x00")
	} else if profile.Mode == credentialModeEcsRamRole {
		accessKeyDigest := sha256.Sum256([]byte(snapshot.AccessKeyID))
		principal = strings.Join([]string{principal, hex.EncodeToString(accessKeyDigest[:])}, "\x00")
	}
	if principal == "" {
		principal = snapshot.AccessKeyID
	}
	if principal == "" {
		return ""
	}
	return credentialSourceCacheKey(profile.Mode, principal)
}

func credentialSourcePrincipal(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func sanitizedCredentialSource(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "[REDACTED]"
	}
	parsed.User = nil
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimSuffix(parsed.String(), "/")
}

func stringMapField(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func intMapField(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func int64MapField(values map[string]any, key string) int64 {
	switch value := values[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case json.Number:
		parsed, _ := value.Int64()
		return parsed
	default:
		return 0
	}
}

func boolMapField(values map[string]any, key string) bool {
	switch value := values[key].(type) {
	case bool:
		return value
	case string:
		return value == "1" || strings.EqualFold(value, "true")
	default:
		return false
	}
}

func environmentBool(getenv func(string) string, name string) bool {
	value := strings.TrimSpace(getenv(name))
	return value == "1" || strings.EqualFold(value, "true")
}

func stringPointer(value string) *string { return &value }
func intPointer(value int) *int          { return &value }
func boolPointer(value bool) *bool       { return &value }

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
