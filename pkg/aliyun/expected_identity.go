package aliyun

import (
	"encoding/json"
	"io"
	"strings"
)

// E2E supplies only trusted STS public identifiers through this internal
// cross-process expectation. It never contains credentials or their digests.
const expectedCredentialIdentityEnv = "ECCTL_EXPECTED_CREDENTIAL_IDENTITY"

type expectedCredentialIdentity struct {
	Version   int    `json:"version"`
	AccountID string `json:"AccountId"`
	Type      string `json:"IdentityType"`
	UserID    string `json:"UserId,omitempty"`
	RoleID    string `json:"RoleId,omitempty"`
}

func parseExpectedCredentialIdentity(raw string) (*expectedCredentialIdentity, string, error) {
	if raw == "" {
		return nil, "", nil
	}
	var pin expectedCredentialIdentity
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pin) != nil || decoder.Decode(new(any)) != io.EOF || pin.Version != 1 || !accountIDPattern.MatchString(pin.AccountID) {
		return nil, "", ErrCredentialIdentityChanged
	}
	if strings.ContainsAny(pin.UserID+pin.RoleID, "\x00\t\r\n") {
		return nil, "", ErrCredentialIdentityChanged
	}
	switch pin.Type {
	case "Account":
		if pin.UserID != "" || pin.RoleID != "" {
			return nil, "", ErrCredentialIdentityChanged
		}
	case "RAMUser":
		if pin.UserID == "" || pin.RoleID != "" {
			return nil, "", ErrCredentialIdentityChanged
		}
	case "AssumedRoleUser":
		if pin.RoleID == "" || pin.UserID != "" {
			return nil, "", ErrCredentialIdentityChanged
		}
	default:
		return nil, "", ErrCredentialIdentityChanged
	}
	identity, err := canonicalIdentity(map[string]any{"AccountId": pin.AccountID, "IdentityType": pin.Type, "UserId": pin.UserID, "RoleId": pin.RoleID})
	if err != nil {
		return nil, "", ErrCredentialIdentityChanged
	}
	return &pin, identity.Hash, nil
}
