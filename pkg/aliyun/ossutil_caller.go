package aliyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

var (
	ossUtilVersionPattern = regexp.MustCompile(`\b([0-9]+)\.[0-9]+(?:\.[0-9]+)?\b`)
	ossUtilElapsedPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?\(s\) elapsed$`)
)

// OSSUtilCaller adapts the OSSUtil API commands exposed by aliyun CLI to the
// generic ecctl call contract. Credentials are resolved by ecctl and passed to
// the child process through environment variables, never command arguments.
type OSSUtilCaller struct {
	Region         string
	Profile        resolvedOpenAPIProfile
	runner         CLICommandRunner
	getenv         func(string) string
	resolveRuntime func(func(string) string) (string, error)
}

func NewOSSUtilCaller(profileName, configPath, region string, getenv func(string) string) (*OSSUtilCaller, error) {
	return newOSSUtilCaller(profileName, configPath, region, getenv, execCLICommandRunner{})
}

func newOSSUtilCaller(profileName, configPath, region string, getenv func(string) string, runner CLICommandRunner) (*OSSUtilCaller, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if configPath == "" {
		configPath = ecconfig.EcctlConfigPath(getenv)
	}
	profile, _, err := ecconfig.EffectiveProfile(profileName, configPath, ecconfig.AliyunConfigPath(getenv))
	if err != nil {
		return nil, ecerrors.Client("InvalidConfig", err.Error())
	}
	resolved := toOpenAPIProfile(profile, region, getenv)
	if resolved.AccessKeyID == "" || resolved.AccessKeySecret == "" {
		return nil, ecerrors.Client("MissingCredentials", "Alibaba Cloud access key is required")
	}
	resolvedRegion, regionErr := ecconfig.ResolveRegion(resolved.RegionID, nil)
	if regionErr != nil {
		return nil, regionErr
	}
	if runner == nil {
		runner = execCLICommandRunner{}
	}
	return &OSSUtilCaller{
		Region:         resolvedRegion,
		Profile:        resolved,
		runner:         runner,
		getenv:         getenv,
		resolveRuntime: resolveInstalledOSSUtilRuntime,
	}, nil
}

func (c *OSSUtilCaller) Call(ctx context.Context, operation string, request map[string]any) (map[string]any, error) {
	return c.CallWithArgs(ctx, operation, request, nil)
}

func (c *OSSUtilCaller) CallWithArgs(ctx context.Context, operation string, request map[string]any, passthrough []string) (map[string]any, error) {
	args, metadata, err := c.commandArgs(operation, request, passthrough)
	if err != nil {
		return nil, err
	}
	runtimePath, err := c.resolveRuntime(c.getenv)
	if err != nil {
		return nil, err
	}
	env := c.commandEnv()
	if err := c.requireVersion2(ctx, runtimePath, env); err != nil {
		return nil, err
	}
	if err := c.requireOperation(ctx, runtimePath, metadata, env); err != nil {
		return nil, err
	}
	stdout, stderr, runErr := c.runner.Run(ctx, "aliyun", args, env)
	if runErr != nil {
		return nil, c.commandError(runErr, stdout, stderr, request)
	}
	if metadata.transfer != "" {
		return map[string]any{
			"Bucket": callerStringMapValue(request, "Bucket"),
			"Key":    callerStringMapValue(request, "Key"),
			"File":   callerStringMapValue(request, "File"),
		}, nil
	}
	return decodeOSSUtilResponse(stdout, metadata.mutation)
}

func (c *OSSUtilCaller) requireVersion2(ctx context.Context, runtimePath string, env []string) error {
	stdout, stderr, err := c.runner.Run(ctx, runtimePath, []string{"version"}, env)
	if err != nil {
		return ossUtilRuntimeError(err, stdout, stderr)
	}
	match := ossUtilVersionPattern.FindSubmatch(bytes.TrimSpace(stdout))
	if len(match) != 2 {
		return ecerrors.Client("OSSUtilVersionUnsupported", "OSS call runtime version is unsupported")
	}
	major, err := strconv.Atoi(string(match[1]))
	if err != nil || major != 2 {
		return ecerrors.Client("OSSUtilVersionUnsupported", "OSS call runtime version is unsupported")
	}
	return nil
}

func (c *OSSUtilCaller) requireOperation(ctx context.Context, runtimePath string, metadata ossUtilOperationMetadata, env []string) error {
	helpArgs := []string{"api", metadata.command, "--help"}
	if metadata.transfer != "" {
		helpArgs = []string{metadata.command, "--help"}
	}
	stdout, stderr, err := c.runner.Run(ctx, runtimePath, helpArgs, env)
	if err == nil {
		return nil
	}
	var pathErr *exec.Error
	if errors.As(err, &pathErr) {
		return ecerrors.Client("OSSUtilUnavailable", "OSS call runtime is unavailable")
	}
	message := cliCommandErrorMessage(err, stdout, stderr)
	return ecerrors.Client("OSSUtilAPIUnavailable", "OSS call operation is unavailable", ecerrors.WithDetail(callerSanitizeCloudError(errors.New(message))))
}

func ossUtilRuntimeError(err error, stdout, stderr []byte) error {
	var pathErr *exec.Error
	if errors.As(err, &pathErr) {
		return ecerrors.Client("OSSUtilUnavailable", "OSS call runtime is unavailable")
	}
	message := cliCommandErrorMessage(err, stdout, stderr)
	return ecerrors.Client("OSSUtilUnavailable", "OSS call runtime is unavailable", ecerrors.WithDetail(callerSanitizeCloudError(errors.New(message))))
}

func (c *OSSUtilCaller) commandArgs(operation string, request map[string]any, passthrough []string) ([]string, ossUtilOperationMetadata, error) {
	metadata, ok := ossUtilOperations[operation]
	if !ok {
		return nil, ossUtilOperationMetadata{}, ecerrors.Client("UnsupportedOperation", "operation is not supported", ecerrors.WithField("operation"))
	}
	if request == nil {
		request = map[string]any{}
	}
	allowed := make(map[string]bool, len(metadata.parameters)+1)
	allowed["RegionId"] = true
	for _, parameter := range metadata.parameters {
		allowed[parameter.Name] = true
	}
	for key := range request {
		if !allowed[key] {
			return nil, ossUtilOperationMetadata{}, ecerrors.Client("UnsupportedOSSParameter", "parameter is not supported for OSS calls", ecerrors.WithField(key))
		}
	}
	if metadata.transfer != "" {
		return c.transferCommandArgs(metadata, request, passthrough)
	}

	args := []string{"--auto-plugin-install", "false", "ossutil", "api", metadata.command}
	for _, parameter := range metadata.parameters {
		value, exists := request[parameter.Name]
		if strings.EqualFold(parameter.Type, "Boolean") {
			if !exists {
				if parameter.Required {
					return nil, ossUtilOperationMetadata{}, ecerrors.Client("MissingParameter", "required parameter is missing: --"+parameter.Name, ecerrors.WithField(parameter.Name))
				}
				continue
			}
			enabled, ok := value.(bool)
			if !ok {
				return nil, ossUtilOperationMetadata{}, ecerrors.Client("InvalidParameter", fmt.Sprintf("%s is invalid", parameter.Name), ecerrors.WithField(parameter.Name))
			}
			if enabled {
				args = append(args, "--"+parameter.flag)
			}
			continue
		}
		encoded, err := cliParamValue(value)
		if err != nil {
			return nil, ossUtilOperationMetadata{}, ecerrors.Client("InvalidParameter", fmt.Sprintf("%s is invalid", parameter.Name), ecerrors.WithField(parameter.Name))
		}
		if !exists || strings.TrimSpace(encoded) == "" {
			if parameter.Required {
				return nil, ossUtilOperationMetadata{}, ecerrors.Client("MissingParameter", "required parameter is missing: --"+parameter.Name, ecerrors.WithField(parameter.Name))
			}
			continue
		}
		args = append(args, "--"+parameter.flag, encoded)
	}

	forwarded, err := ossUtilPassthroughArgs(passthrough, false)
	if err != nil {
		return nil, ossUtilOperationMetadata{}, err
	}
	args = append(args, forwarded...)
	args = append(args, "--region", c.Region)
	args = append(args, "--output-format", "json")
	return args, metadata, nil
}

func (c *OSSUtilCaller) transferCommandArgs(metadata ossUtilOperationMetadata, request map[string]any, passthrough []string) ([]string, ossUtilOperationMetadata, error) {
	values := make(map[string]string, len(metadata.parameters))
	force := false
	for _, parameter := range metadata.parameters {
		value, exists := request[parameter.Name]
		if strings.EqualFold(parameter.Type, "Boolean") {
			if !exists {
				if parameter.Required {
					return nil, ossUtilOperationMetadata{}, ecerrors.Client("MissingParameter", "required parameter is missing: --"+parameter.Name, ecerrors.WithField(parameter.Name))
				}
				continue
			}
			enabled, ok := value.(bool)
			if !ok {
				return nil, ossUtilOperationMetadata{}, ecerrors.Client("InvalidParameter", fmt.Sprintf("%s is invalid", parameter.Name), ecerrors.WithField(parameter.Name))
			}
			if parameter.Name == "Force" {
				force = enabled
			}
			continue
		}
		encoded, err := cliParamValue(value)
		if err != nil {
			return nil, ossUtilOperationMetadata{}, ecerrors.Client("InvalidParameter", fmt.Sprintf("%s is invalid", parameter.Name), ecerrors.WithField(parameter.Name))
		}
		encoded = strings.TrimSpace(encoded)
		if !exists || encoded == "" {
			if parameter.Required {
				return nil, ossUtilOperationMetadata{}, ecerrors.Client("MissingParameter", "required parameter is missing: --"+parameter.Name, ecerrors.WithField(parameter.Name))
			}
			continue
		}
		values[parameter.Name] = encoded
	}
	objectURI := "oss://" + values["Bucket"] + "/" + values["Key"]
	source, destination := objectURI, values["File"]
	if metadata.transfer == ossUtilUpload {
		source, destination = destination, objectURI
	}
	args := []string{"--auto-plugin-install", "false", "ossutil", metadata.command, source, destination}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--no-progress")
	forwarded, err := ossUtilPassthroughArgs(passthrough, true)
	if err != nil {
		return nil, ossUtilOperationMetadata{}, err
	}
	args = append(args, forwarded...)
	args = append(args, "--region", c.Region)
	return args, metadata, nil
}

func ossUtilPassthroughArgs(args []string, transfer bool) ([]string, error) {
	type allowedFlag struct {
		output string
		value  bool
	}
	allowed := map[string]allowedFlag{
		"--endpoint":        {output: "--endpoint", value: true},
		"-e":                {output: "--endpoint", value: true},
		"--read-timeout":    {output: "--read-timeout", value: true},
		"--connect-timeout": {output: "--connect-timeout", value: true},
		"--retry-count":     {output: "--retry-times", value: true},
		"--user-agent":      {output: "--user-agent", value: true},
	}
	if transfer {
		allowed["--parallel"] = allowedFlag{output: "--parallel", value: true}
		allowed["--part-size"] = allowedFlag{output: "--part-size", value: true}
	}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		raw := args[i]
		name, inline, hasInline := strings.Cut(raw, "=")
		flag, ok := allowed[name]
		if !ok {
			return nil, ecerrors.Client("UnsupportedOSSParameter", "parameter is not supported for OSS calls", ecerrors.WithField(strings.TrimLeft(name, "-")))
		}
		out = append(out, flag.output)
		if !flag.value {
			if hasInline {
				return nil, ecerrors.Client("InvalidParameter", name+" does not accept a value", ecerrors.WithField(strings.TrimLeft(name, "-")))
			}
			continue
		}
		value := inline
		if !hasInline {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, ecerrors.Client("MissingParameter", name+" requires a value", ecerrors.WithField(strings.TrimLeft(name, "-")))
			}
			i++
			value = args[i]
		}
		if strings.TrimSpace(value) == "" {
			return nil, ecerrors.Client("MissingParameter", name+" requires a value", ecerrors.WithField(strings.TrimLeft(name, "-")))
		}
		out = append(out, value)
	}
	return out, nil
}

func (c *OSSUtilCaller) commandEnv() []string {
	env := withoutEnvironmentKeys(os.Environ(), ossUtilIdentityEnvironmentKeys...)
	env = append(env,
		"ALIBABA_CLOUD_IGNORE_PROFILE=TRUE",
		"ALIBABA_CLOUD_ACCESS_KEY_ID="+c.Profile.AccessKeyID,
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET="+c.Profile.AccessKeySecret,
		"ALIBABA_CLOUD_REGION_ID="+c.Region,
		"OSS_ACCESS_KEY_ID="+c.Profile.AccessKeyID,
		"OSS_ACCESS_KEY_SECRET="+c.Profile.AccessKeySecret,
		"OSS_REGION="+c.Region,
	)
	if c.Profile.SecurityToken != "" {
		env = append(env,
			"ALIBABA_CLOUD_SECURITY_TOKEN="+c.Profile.SecurityToken,
			"OSS_SESSION_TOKEN="+c.Profile.SecurityToken,
		)
	}
	return env
}

var ossUtilIdentityEnvironmentKeys = []string{
	"ALIBABA_CLOUD_IGNORE_PROFILE", "ALIBABACLOUD_IGNORE_PROFILE",
	"ALIBABA_CLOUD_PROFILE", "ALIBABACLOUD_PROFILE", "ALICLOUD_PROFILE",
	"ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABACLOUD_ACCESS_KEY_ID", "ALICLOUD_ACCESS_KEY_ID", "ACCESS_KEY_ID",
	"ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABACLOUD_ACCESS_KEY_SECRET", "ALICLOUD_ACCESS_KEY_SECRET", "ACCESS_KEY_SECRET",
	"ALIBABA_CLOUD_SECURITY_TOKEN", "ALIBABACLOUD_SECURITY_TOKEN", "ALICLOUD_SECURITY_TOKEN", "SECURITY_TOKEN",
	"ALIBABA_CLOUD_REGION_ID", "ALIBABACLOUD_REGION_ID", "ALICLOUD_REGION_ID", "REGION_ID", "REGION",
	"ALIBABA_CLOUD_CREDENTIALS_URI", "ALIBABA_CLOUD_EXTERNAL_ACCOUNT_TYPE",
	"ALIBABA_CLOUD_OIDC_PROVIDER_ARN", "ALIBABACLOUD_OIDC_PROVIDER_ARN",
	"ALIBABA_CLOUD_OIDC_TOKEN_FILE", "ALIBABACLOUD_OIDC_TOKEN_FILE",
	"ALIBABA_CLOUD_ROLE_ARN", "ALIBABACLOUD_ROLE_ARN",
	"ALIBABA_CLOUD_EXTERNAL_ID", "ALIBABACLOUD_EXTERNAL_ID",
	"ALIBABA_CLOUD_BEARER_TOKEN", "ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY",
	"ALIBABA_CLOUD_ENDPOINT", "ALIBABA_CLOUD_ENDPOINT_TYPE", "ALIBABACLOUD_ENDPOINT_TYPE", "ALICLOUD_ENDPOINT_TYPE", "ENDPOINT_TYPE",
	"ALIBABA_CLOUD_CLI_PLUGIN_AUTO_INSTALL", "ALIBABA_CLOUD_CLI_PLUGIN_AUTO_INSTALL_ENABLE_PRE",
	"OSS_ACCESS_KEY_ID", "OSS_ACCESS_KEY_SECRET", "OSS_SESSION_TOKEN", "OSS_REGION",
	"OSS_ROLE_ARN", "OSS_ROLE_SESSION_NAME", "OSS_ENDPOINT",
	"OSS_CONFIG_FILE", "OSS_PROFILE", "OSSUTIL_CONFIG_VALUE", "OSSUTIL_COMPAT_MODE",
}

func resolveInstalledOSSUtilRuntime(getenv func(string) string) (string, error) {
	home := ""
	if getenv != nil {
		home = strings.TrimSpace(getenv("HOME"))
	}
	if home == "" {
		resolvedHome, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(resolvedHome) == "" {
			return "", ecerrors.Client("OSSUtilUnavailable", "OSS call runtime is unavailable")
		}
		home = resolvedHome
	}
	name := "ossutil"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(home, ".aliyun", name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return "", ecerrors.Client("OSSUtilUnavailable", "OSS call runtime is unavailable")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", ecerrors.Client("OSSUtilUnavailable", "OSS call runtime is unavailable")
	}
	return path, nil
}

func withoutEnvironmentKeys(env []string, keys ...string) []string {
	blocked := make(map[string]bool, len(keys))
	for _, key := range keys {
		blocked[key] = true
	}
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if blocked[key] {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (c *OSSUtilCaller) commandError(runErr error, stdout, stderr []byte, request map[string]any) error {
	var pathErr *exec.Error
	if errors.As(runErr, &pathErr) {
		return ecerrors.Client("OSSUtilUnavailable", "OSS call runtime is unavailable")
	}
	message := cliCommandErrorMessage(runErr, stdout, stderr)
	wrapper := errors.New(message)
	code, _, _ := ecerrors.ParseCloudError(message)
	switch {
	case strings.EqualFold(code, "NoSuchBucket") || isOSSNoSuchBucket(wrapper) || isCloudNotFound(wrapper):
		resource := callerStringMapValue(request, "Bucket")
		if resource == "" {
			resource = "resource"
		}
		return ecerrors.NotFound("NotFound", resource+" not found", cloudErrorOptions(wrapper)...)
	case strings.EqualFold(code, "BucketNotEmpty") || isDependencyViolation(wrapper) || isOSSBucketNotEmpty(wrapper):
		return ecerrors.Service("DependencyViolation", callerCloudErrorMessage(wrapper), false, cloudErrorOptions(wrapper)...)
	default:
		return ecerrors.Service("CloudAPIError", callerCloudErrorMessage(wrapper), false, cloudErrorOptions(wrapper)...)
	}
}

func isOSSNoSuchBucket(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "nosuchbucket")
}

func isOSSBucketNotEmpty(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "bucketnotempty") || strings.Contains(message, "bucket not empty")
}

func decodeOSSUtilResponse(stdout []byte, allowEmpty bool) (map[string]any, error) {
	raw := stripOSSUtilElapsedTrailer(strings.TrimSpace(string(stdout)))
	if raw == "" {
		if allowEmpty {
			return map[string]any{}, nil
		}
		return nil, ecerrors.Client("InvalidOSSUtilOutput", "OSS call returned invalid JSON output")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, ecerrors.Client("InvalidOSSUtilOutput", "OSS call returned invalid JSON output")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, ecerrors.Client("InvalidOSSUtilOutput", "OSS call returned invalid JSON output")
	}
	switch value := decoded.(type) {
	case map[string]any:
		return value, nil
	default:
		return nil, ecerrors.Client("InvalidOSSUtilOutput", "OSS call returned invalid JSON output")
	}
}

func stripOSSUtilElapsedTrailer(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if ossUtilElapsedPattern.MatchString(last) {
		return strings.TrimSpace(strings.Join(lines[:len(lines)-1], "\n"))
	}
	return raw
}
