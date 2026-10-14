package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const ExpectedIdentityEnv = "ECCTL_EXPECTED_CREDENTIAL_IDENTITY"

// IdentityPin contains only stable public STS identifiers, never credentials,
// session identifiers, credential hashes or the unfiltered provider response.
type IdentityPin struct {
	Version   int    `json:"version"`
	AccountID string `json:"AccountId"`
	Type      string `json:"IdentityType"`
	UserID    string `json:"UserId,omitempty"`
	RoleID    string `json:"RoleId,omitempty"`
}

func (pin IdentityPin) Validate() error {
	valid := pin.Version == 1 && len(pin.AccountID) == 16 && strings.IndexFunc(pin.AccountID, func(r rune) bool { return r < '0' || r > '9' }) < 0
	if strings.ContainsAny(pin.UserID+pin.RoleID, "\x00\t\r\n") {
		valid = false
	}
	switch pin.Type {
	case "Account":
		valid = valid && pin.UserID == "" && pin.RoleID == ""
	case "RAMUser":
		valid = valid && pin.UserID != "" && pin.RoleID == ""
	case "AssumedRoleUser":
		valid = valid && pin.RoleID != "" && pin.UserID == ""
	default:
		valid = false
	}
	if !valid {
		return fmt.Errorf("trusted credential identity is incomplete or unsupported")
	}
	return nil
}

func quoteIdentityArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func ReadIdentity(ctx context.Context, cfg Config, profile string) (IdentityPin, error) {
	args := []string{"ecctl", "call", "sts", "GetCallerIdentity", "--profile", profile, "--output=json", "--agent-envelope=false"}
	for index := range args {
		args[index] = quoteIdentityArgument(args[index])
	}
	result := Run(ctx, cfg, strings.Join(args, " "))
	root, _ := result.JSON.(map[string]any)
	response, _ := root["response"].(map[string]any)
	pin := IdentityPin{Version: 1}
	pin.AccountID, _ = response["AccountId"].(string)
	pin.Type, _ = response["IdentityType"].(string)
	if pin.Type == "RAMUser" {
		pin.UserID, _ = response["UserId"].(string)
	}
	if pin.Type == "AssumedRoleUser" {
		pin.RoleID, _ = response["RoleId"].(string)
	}
	if result.Err != nil || result.Exit != 0 {
		return IdentityPin{}, fmt.Errorf("trusted STS credential identity is unavailable")
	}
	if err := pin.Validate(); err != nil {
		return IdentityPin{}, err
	}
	if cfg.ExpectedIdentity != nil && pin != *cfg.ExpectedIdentity {
		return IdentityPin{}, fmt.Errorf("credential principal differs from creation identity")
	}
	return pin, nil
}

func identityEnvironment(pin *IdentityPin) ([]string, error) {
	if pin == nil {
		return nil, nil
	}
	if err := pin.Validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(pin)
	if err != nil {
		return nil, err
	}
	return []string{ExpectedIdentityEnv + "=" + string(body)}, nil
}
