package aliyun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

func identityResolver(profile resolvedOpenAPIProfile, executor openAPIExecutor) telemetry.IdentityResolver {
	return func(ctx context.Context) (telemetry.Identity, error) {
		if executor == nil {
			var err error
			executor, err = newDarabonbaExecutor(profile)
			if err != nil {
				return telemetry.Identity{}, err
			}
		}
		req := newOpenAPIRequest()
		req.Product = "Sts"
		req.Version = "2015-04-01"
		req.ApiName = "GetCallerIdentity"
		req.Domain = "sts.aliyuncs.com"
		req.Scheme = "https"
		req.Method = "GET"
		response, err := executor.ExecuteOpenAPI(ctx, req)
		if err != nil {
			return telemetry.Identity{}, err
		}
		return canonicalIdentity(response)
	}
}

func canonicalIdentity(response map[string]any) (telemetry.Identity, error) {
	identityType, _ := response["IdentityType"].(string)
	var canonical string
	switch identityType {
	case "Account":
		accountID, _ := response["AccountId"].(string)
		if accountID == "" {
			return telemetry.Identity{}, errors.New("caller account identity is incomplete")
		}
		canonical = "v1\x00account\x00" + accountID
	case "RAMUser":
		userID, _ := response["UserId"].(string)
		if userID == "" {
			return telemetry.Identity{}, errors.New("caller RAM user identity is incomplete")
		}
		canonical = "v1\x00ram-user\x00" + userID
	case "AssumedRoleUser":
		roleID, _ := response["RoleId"].(string)
		if roleID == "" {
			return telemetry.Identity{}, errors.New("caller RAM role identity is incomplete")
		}
		canonical = "v1\x00ram-role\x00" + roleID
	default:
		return telemetry.Identity{}, errors.New("caller identity type is unsupported")
	}
	digest := sha256.Sum256([]byte(canonical))
	return telemetry.Identity{Hash: hex.EncodeToString(digest[:]), Type: identityType}, nil
}
