package aliyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	ecconfig "ecctl/pkg/config"
	ecerrors "ecctl/pkg/errors"
)

type CLICommandRunner interface {
	Run(ctx context.Context, name string, args []string, env []string) ([]byte, []byte, error)
}

type execCLICommandRunner struct{}

func (execCLICommandRunner) Run(ctx context.Context, name string, args []string, env []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

type CLICommandCaller struct {
	Product    string
	Region     string
	Profile    string
	ConfigPath string
	getenv     func(string) string
	runner     CLICommandRunner
}

func NewCLICommandCaller(profileName, configPath, product, region string, getenv func(string) string) (*CLICommandCaller, error) {
	return newCLICommandCaller(profileName, configPath, product, region, getenv, execCLICommandRunner{})
}

func newCLICommandCaller(profileName, configPath, product, region string, getenv func(string) string, runner CLICommandRunner) (*CLICommandCaller, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if configPath == "" {
		configPath = ecconfig.EcctlConfigPath(getenv)
	}
	if runner == nil {
		runner = execCLICommandRunner{}
	}
	return &CLICommandCaller{
		Product:    product,
		Region:     region,
		Profile:    profileName,
		ConfigPath: configPath,
		getenv:     getenv,
		runner:     runner,
	}, nil
}

func (c *CLICommandCaller) Call(ctx context.Context, operation string, request map[string]any) (map[string]any, error) {
	return c.CallWithArgs(ctx, operation, request, nil)
}

func (c *CLICommandCaller) CallWithArgs(ctx context.Context, operation string, request map[string]any, passthrough []string) (map[string]any, error) {
	args, cleanup, err := c.aliyunCLIArgs(operation, request, passthrough)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, err
	}
	stdout, stderr, err := c.runner.Run(ctx, "aliyun", args, os.Environ())
	if err != nil {
		message := cliCommandErrorMessage(err, stdout, stderr)
		wrapped := errors.New(message)
		if callerBoolMapValue(request, "DryRun") && isDryRunPassed(wrapped) {
			return map[string]any{"DryRun": true}, nil
		}
		var pathErr *exec.Error
		if errors.As(err, &pathErr) {
			return nil, ecerrors.Client("AliyunCLINotFound", "aliyun CLI executable was not found")
		}
		if isCloudNotFound(wrapped) {
			code, _, _ := ecerrors.ParseCloudError(message)
			return nil, ecerrors.NotFound("NotFound", cloudNotFoundMessage(request, code), cloudErrorOptions(wrapped)...)
		}
		if isDependencyViolation(wrapped) {
			return nil, ecerrors.Service("DependencyViolation", callerCloudErrorMessage(wrapped), false, cloudErrorOptions(wrapped)...)
		}
		return nil, ecerrors.Service("CloudAPIError", callerCloudErrorMessage(wrapped), false, cloudErrorOptions(wrapped)...)
	}
	return decodeCLICommandResponse(stdout)
}

func (c *CLICommandCaller) aliyunCLIArgs(operation string, request map[string]any, passthrough []string) ([]string, func(), error) {
	if request == nil {
		request = map[string]any{}
	}
	configPath, profile, cleanup, err := c.commandConfig(passthrough)
	if err != nil {
		return nil, nil, err
	}
	args := []string{c.Product, operation}
	if configPath != "" && !containsCLIFlag(passthrough, "config-path") {
		args = append(args, "--config-path", configPath)
	}
	if profile != "" && !containsCLIFlag(passthrough, "profile") {
		args = append(args, "--profile", profile)
	}
	if c.Region != "" && callerStringMapValue(request, "RegionId") == "" && !containsCLIFlag(passthrough, "region") {
		args = append(args, "--region", c.Region)
	}
	requestArgs, err := c.requestArgs(operation, request)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, nil, err
	}
	args = append(args, requestArgs...)
	args = append(args, passthrough...)
	return args, cleanup, nil
}

func (c *CLICommandCaller) requestArgs(operation string, request map[string]any) ([]string, error) {
	bodyKeys := map[string]bool{}
	body, err := c.requestBody(operation, request, bodyKeys)
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, len(request)*2+2)
	if body != nil {
		value, err := cliParamValue(body)
		if err != nil {
			return nil, err
		}
		args = append(args, "--body", value)
	}
	keys := make([]string, 0, len(request))
	for key := range request {
		if bodyKeys[key] {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, err := cliParamValue(request[key])
		if err != nil {
			return nil, err
		}
		if value == "" {
			continue
		}
		args = append(args, "--"+key, value)
	}
	return args, nil
}

func (c *CLICommandCaller) requestBody(operation string, request map[string]any, bodyKeys map[string]bool) (any, error) {
	product, ok := OpenAPIProductByCode(c.Product, "en")
	if !ok {
		return requestBodyWithoutMetadata(request, bodyKeys)
	}
	operationName, ok := OpenAPIOperationName(product, operation)
	if !ok {
		return requestBodyWithoutMetadata(request, bodyKeys)
	}
	api, ok := OpenAPIOperationDetailFor("en", product, operationName)
	if !ok {
		return requestBodyWithoutMetadata(request, bodyKeys)
	}
	body, used, err := openAPIRequestBody(&api, request)
	for key := range used {
		bodyKeys[key] = true
	}
	if body != nil || err != nil {
		return body, err
	}
	return requestBodyWithoutMetadata(request, bodyKeys)
}

func requestBodyWithoutMetadata(request map[string]any, bodyKeys map[string]bool) (any, error) {
	if body, ok := request["body"]; ok {
		bodyKeys["body"] = true
		if isEmptyOpenAPIBody(body) {
			return nil, nil
		}
		return body, nil
	}
	body, err := flatOpenAPIBody(request, bodyKeys)
	if err != nil || isEmptyOpenAPIBody(body) {
		return nil, err
	}
	return body, nil
}

func cliParamValue(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case bool:
		return strconv.FormatBool(typed), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case json.Number:
		return typed.String(), nil
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func (c *CLICommandCaller) commandConfig(passthrough []string) (string, string, func(), error) {
	if containsCLIFlag(passthrough, "config-path") {
		return "", c.Profile, nil, nil
	}
	aliyunConfigPath := ecconfig.AliyunConfigPath(c.getenv)
	aliyunConfig, hasAliyun, err := loadConfigObject(aliyunConfigPath)
	if err != nil {
		return "", "", nil, ecerrors.Client("InvalidConfig", err.Error())
	}
	ecctlConfig, hasEcctl, err := loadConfigObject(c.ConfigPath)
	if err != nil {
		return "", "", nil, ecerrors.Client("InvalidConfig", err.Error())
	}
	explicitProfile := c.Profile
	if passthroughProfile, ok := cliFlagValue(passthrough, "profile"); ok && passthroughProfile != "" {
		explicitProfile = passthroughProfile
	}
	profile := selectedConfigProfile(explicitProfile, ecctlConfig, hasEcctl, aliyunConfig, hasAliyun)
	if !hasEcctl {
		if hasAliyun {
			return aliyunConfigPath, profile, nil, nil
		}
		return "", profile, nil, nil
	}
	merged := cloneConfigObject(aliyunConfig)
	ensureConfigShape(merged)
	merged["current"] = profile
	if ecctlProfile, ok := configProfile(ecctlConfig, profile); ok {
		upsertConfigProfile(merged, ecctlProfile)
	} else if _, ok := configProfile(merged, profile); !ok {
		upsertConfigProfile(merged, map[string]any{"name": profile})
	}
	path, cleanup, err := writeTempConfig(merged)
	if err != nil {
		return "", "", nil, ecerrors.Client("InvalidConfig", err.Error())
	}
	return path, profile, cleanup, nil
}

func loadConfigObject(path string) (map[string]any, bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, true, nil
}

func selectedConfigProfile(explicit string, ecctlConfig map[string]any, hasEcctl bool, aliyunConfig map[string]any, hasAliyun bool) string {
	if explicit != "" {
		return explicit
	}
	if hasEcctl {
		if current, _ := ecctlConfig["current"].(string); current != "" {
			return current
		}
	}
	if hasAliyun {
		if current, _ := aliyunConfig["current"].(string); current != "" {
			return current
		}
	}
	return ecconfig.DefaultProfileName
}

func cloneConfigObject(config map[string]any) map[string]any {
	if config == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func ensureConfigShape(config map[string]any) {
	if _, ok := config["profiles"].([]any); !ok {
		config["profiles"] = []any{}
	}
	if current, _ := config["current"].(string); current == "" {
		config["current"] = ecconfig.DefaultProfileName
	}
}

func configProfile(config map[string]any, name string) (map[string]any, bool) {
	if name == "" {
		name = ecconfig.DefaultProfileName
	}
	profiles, _ := config["profiles"].([]any)
	for _, raw := range profiles {
		profile, _ := raw.(map[string]any)
		if profileName, _ := profile["name"].(string); profileName == name {
			return cloneConfigObject(profile), true
		}
	}
	return nil, false
}

func upsertConfigProfile(config map[string]any, profile map[string]any) {
	ensureConfigShape(config)
	name, _ := profile["name"].(string)
	if name == "" {
		name = ecconfig.DefaultProfileName
		profile["name"] = name
	}
	profiles, _ := config["profiles"].([]any)
	for i, raw := range profiles {
		existing, _ := raw.(map[string]any)
		if existingName, _ := existing["name"].(string); existingName == name {
			merged := cloneConfigObject(existing)
			for key, value := range profile {
				merged[key] = value
			}
			profiles[i] = merged
			config["profiles"] = profiles
			return
		}
	}
	config["profiles"] = append(profiles, cloneConfigObject(profile))
}

func writeTempConfig(config map[string]any) (string, func(), error) {
	file, err := os.CreateTemp("", "ecctl-aliyun-config-*.json")
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	cleanup := func() {
		_ = os.Remove(path)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

func containsCLIFlag(args []string, name string) bool {
	_, ok := cliFlagValue(args, name)
	return ok
}

func cliFlagValue(args []string, name string) (string, bool) {
	long := "--" + name
	prefix := long + "="
	short := ""
	if name == "profile" {
		short = "-p"
	}
	for i, arg := range args {
		if arg == long || (short != "" && arg == short) {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix), true
		}
		if short != "" && strings.HasPrefix(arg, short+"=") {
			return strings.TrimPrefix(arg, short+"="), true
		}
	}
	return "", false
}

func decodeCLICommandResponse(stdout []byte) (map[string]any, error) {
	raw := strings.TrimSpace(string(stdout))
	if raw == "" {
		return map[string]any{}, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return map[string]any{"raw": raw}, nil
	}
	switch typed := decoded.(type) {
	case map[string]any:
		return typed, nil
	case []any:
		return map[string]any{
			"items":                     typed,
			topLevelArrayResponseMarker: true,
		}, nil
	default:
		return map[string]any{"raw": raw}, nil
	}
}

func cliCommandErrorMessage(err error, stdout []byte, stderr []byte) string {
	for _, raw := range [][]byte{stderr, stdout} {
		if message := strings.TrimSpace(string(raw)); message != "" {
			return message
		}
	}
	if err != nil {
		return err.Error()
	}
	return "aliyun CLI command failed"
}
