package aliyun

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

var (
	accountIDPattern        = regexp.MustCompile(`^[0-9]{16}$`)
	ramRoleNamePattern      = regexp.MustCompile(`^[A-Za-z0-9.-]{1,64}$`)
	oidcProviderNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

type credentialIdentityPolicy struct {
	stsRegion string
	enableVPC bool
}

func identityPolicyFromProfile(profile map[string]any, getenv func(string) string) credentialIdentityPolicy {
	_, region, enableVPC := credentialSTSSettings(profile, getenv)
	return credentialIdentityPolicy{stsRegion: strings.TrimSpace(region), enableVPC: enableVPC}
}

func (p credentialIdentityPolicy) endpoint(operationRegion string) (string, error) {
	return effectiveOfficialSTSEndpoint(p.stsRegion, p.enableVPC, operationRegion)
}

func effectiveOfficialSTSEndpoint(explicitRegion string, enableVPC bool, operationRegion string) (string, error) {
	region := strings.TrimSpace(explicitRegion)
	if region == "" && enableVPC {
		region = strings.TrimSpace(operationRegion)
	}
	if region == "" {
		if enableVPC {
			return "", errors.New("STS region is required when the VPC endpoint is enabled")
		}
		return "sts.aliyuncs.com", nil
	}
	if !ecconfig.ValidRegion(region) {
		return "", fmt.Errorf("invalid STS region %s", region)
	}
	prefix := "sts"
	if enableVPC {
		prefix = "sts-vpc"
	}
	return prefix + "." + region + ".aliyuncs.com", nil
}

func parseRAMRoleARN(raw string) (string, string, error) {
	accountID, resource, err := parseRAMResourceARN(raw)
	if err != nil || !strings.HasPrefix(resource, "role/") {
		return "", "", errors.New("RAM role ARN must use acs:ram::<16-digit-account-id>:role/<role-name>")
	}
	roleName := strings.TrimPrefix(resource, "role/")
	if !ramRoleNamePattern.MatchString(roleName) {
		return "", "", errors.New("RAM role ARN contains an invalid role name")
	}
	return accountID, roleName, nil
}

func parseOIDCProviderARN(raw string) (string, string, error) {
	accountID, resource, err := parseRAMResourceARN(raw)
	if err != nil || !strings.HasPrefix(resource, "oidc-provider/") {
		return "", "", errors.New("OIDC provider ARN must use acs:ram::<16-digit-account-id>:oidc-provider/<provider-name>")
	}
	providerName := strings.TrimPrefix(resource, "oidc-provider/")
	if !oidcProviderNamePattern.MatchString(providerName) {
		return "", "", errors.New("OIDC provider ARN contains an invalid provider name")
	}
	return accountID, providerName, nil
}

func parseRAMResourceARN(raw string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 5 || parts[0] != "acs" || parts[1] != "ram" || parts[2] != "" || !accountIDPattern.MatchString(parts[3]) || parts[4] == "" {
		return "", "", errors.New("invalid RAM ARN")
	}
	return parts[3], parts[4], nil
}
