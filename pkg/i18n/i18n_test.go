package i18n

import (
	"strings"
	"sync"
	"testing"

	ecerrors "ecctl/pkg/errors"
)

func TestLocalizeFallsBackToEnglish(t *testing.T) {
	localizer := NewLocalizer("fr-FR")
	if got := localizer.Message("MissingRegion"); got != "region is required" {
		t.Fatalf("fallback message = %q", got)
	}
}

func TestLocalizeChineseMessage(t *testing.T) {
	localizer := NewLocalizer("zh-Hans")
	if got := localizer.Message("MissingRegion"); got != "必须提供地域" {
		t.Fatalf("unexpected localized message = %q", got)
	}
}

func TestLocalizeTemplateMessage(t *testing.T) {
	if got := NewLocalizer("en").MessageData("NotFoundWithResource", map[string]any{"Resource": "vsw-123"}); got != "vsw-123 not found" {
		t.Fatalf("English not found template message = %q", got)
	}
	if got := NewLocalizer("zh-CN").MessageData("NotFoundWithResource", map[string]any{"Resource": "vsw-123"}); got != "vsw-123 资源不存在" {
		t.Fatalf("Chinese not found template message = %q", got)
	}
}

func TestHelpTranslations(t *testing.T) {
	localizer := NewLocalizer("zh-CN")
	if got := localizer.CommandShort("ecctl configure set", "Set config values"); got != "设置配置值" {
		t.Fatalf("CommandShort = %q", got)
	}
	if got := localizer.CommandExample("ecctl configure", "fallback example"); got == "fallback example" || got == "" {
		t.Fatalf("CommandExample = %q", got)
	}
	if got := localizer.CommandLongData("ecctl configure set", "fallback {{.Keys}}", map[string]any{"Keys": "region"}); got == "fallback region" || got == "" {
		t.Fatalf("CommandLongData = %q", got)
	}
	if got := localizer.CommandGroupTitle("cloud-products", "Cloud Product Commands:"); got != "云产品命令:" {
		t.Fatalf("CommandGroupTitle = %q", got)
	}
	if got := localizer.FlagUsage("force JSON output"); got != "强制 JSON 输出" {
		t.Fatalf("FlagUsage = %q", got)
	}
	if got := localizer.MessageData("HelpDefaultValue", map[string]any{"Value": "5m"}); got != "(默认 5m)" {
		t.Fatalf("HelpDefaultValue = %q", got)
	}
	if got := localizer.Message("InputStyleSignedValue"); got != "+值表示分配，-值表示回收" {
		t.Fatalf("InputStyleSignedValue = %q", got)
	}
	if got := localizer.Message("InputStyleInlineObject"); got != "内联 key=value、JSON 对象或 @file" {
		t.Fatalf("InputStyleInlineObject = %q", got)
	}
}

func TestCloudAPIErrorWithActionsMessage(t *testing.T) {
	if got := NewLocalizer("en").Message("CloudAPIErrorWithActions"); got != "API call failed; see actions for details" {
		t.Fatalf("English action detail message = %q", got)
	}
	if got := NewLocalizer("zh-CN").Message("CloudAPIErrorWithActions"); got != "调用 API 报错，请查看 actions 中的具体报错" {
		t.Fatalf("Chinese action detail message = %q", got)
	}
}

func TestNotFoundMessagePreservesResourceContext(t *testing.T) {
	if got := NewLocalizer("en").NotFoundMessage("vsw-123 not found"); got != "vsw-123 not found" {
		t.Fatalf("English not found message = %q", got)
	}
	if got := NewLocalizer("zh-CN").NotFoundMessage("vsw-123 not found"); got != "vsw-123 资源不存在" {
		t.Fatalf("Chinese not found message = %q", got)
	}
	if got := NewLocalizer("zh-CN").NotFoundMessage("resource not found"); got != "资源不存在" {
		t.Fatalf("generic Chinese not found message = %q", got)
	}
}

func TestRegisteredMessage(t *testing.T) {
	RegisterMessage(MessageSpec{
		ID: "ExampleProductError",
		Text: map[string]string{
			"en":    "example product error",
			"zh-CN": "示例产品错误",
		},
	})
	if got := NewLocalizer("en").Message("ExampleProductError"); got != "example product error" {
		t.Fatalf("English registered message = %q", got)
	}
	if got := NewLocalizer("zh-CN").Message("ExampleProductError"); got != "示例产品错误" {
		t.Fatalf("Chinese registered message = %q", got)
	}
}

func TestKnownErrorCodesHaveEnglishAndChineseMessages(t *testing.T) {
	codes := []string{
		"CloudAPIError",
		"CloudAPIErrorWithActions",
		"ConfigWriteFailed",
		"ConflictingParameters",
		"DependencyConflict",
		"DependencyViolation",
		"HiddenRetryTimeout",
		"InternalError",
		"InvalidConfig",
		"InvalidCount",
		"InvalidCredentials",
		"InvalidDryRunAmount",
		"InvalidFilter",
		"InvalidIDs",
		"InvalidLimit",
		"InvalidPage",
		"InvalidParameter",
		"InvalidRegion",
		"InvalidResourceSpec",
		"InvalidTag",
		"InvalidUserDataFile",
		"InvalidWaiter",
		"LiveOperationUnavailable",
		"MissingCredentials",
		"MissingParameter",
		"MissingRuleID",
		"MissingSchema",
		"MissingStatus",
		"MissingTransitionID",
		"NotFound",
		"NotFoundWithResource",
		"NoUpdateFieldsSpecified",
		"ProfileNotFound",
		"UnknownAction",
		"UnknownCommand",
		"UnknownConfigKey",
		"UnknownProbe",
		"UnknownSchema",
		"UnknownTransition",
		"UnknownWaiter",
		"UnsupportedAction",
		"UnsupportedEmit",
		"UnsupportedOperation",
		"UnsupportedOutputMode",
		"UnsupportedProduct",
		"UnsupportedRuleSelector",
		"WaitTimeout",
	}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			if got := NewLocalizer("en").Message(code); got == code {
				t.Fatalf("missing English message for %s", code)
			}
			if got := NewLocalizer("zh-CN").Message(code); got == code {
				t.Fatalf("missing Chinese message for %s", code)
			}
		})
	}
}

func TestResolveLanguageFromExplicitValue(t *testing.T) {
	if got := ResolveLanguage("zh-CN", func(string) string { return "" }); got != "zh-Hans" {
		t.Fatalf("ResolveLanguage zh-CN = %q, want zh-Hans", got)
	}
}

func TestResolveLanguageFromEnvironment(t *testing.T) {
	env := map[string]string{"LC_ALL": "zh_CN.UTF-8"}
	getenv := func(name string) string { return env[name] }
	if got := ResolveLanguage("", getenv); got != "zh-Hans" {
		t.Fatalf("ResolveLanguage env = %q, want zh-Hans", got)
	}
}

func TestResolveLanguageFallsBackToEnglishForUnsupportedLanguage(t *testing.T) {
	env := map[string]string{"LC_ALL": "fr_FR.UTF-8", "LANG": "zh_CN.UTF-8"}
	getenv := func(name string) string { return env[name] }
	if got := ResolveLanguage("", getenv); got != "en" {
		t.Fatalf("ResolveLanguage unsupported = %q, want en", got)
	}
}

func TestErrorPayloadLocalizesSuggestionsAndMessages(t *testing.T) {
	localizer := NewLocalizer("zh-CN")

	payload := localizer.ErrorPayload(ecerrorsPayload("MissingParameter", "missing required parameters: --name, <id>"), false)
	if payload.Message != "缺少必填参数: --name, <id>" {
		t.Fatalf("MissingParameter message = %q", payload.Message)
	}
	if payload.Suggestion != "使用 `--help` 查看该命令的必填参数。" {
		t.Fatalf("MissingParameter suggestion = %q", payload.Suggestion)
	}

	payload = localizer.ErrorPayload(ecerrorsPayload("NotFound", "vsw-123 not found"), false)
	if payload.Message != "vsw-123 资源不存在" {
		t.Fatalf("NotFound message = %q", payload.Message)
	}

	payload = localizer.ErrorPayload(ecerrorsPayload("CloudAPIError", "raw cloud error"), true)
	if payload.Message != "调用 API 报错，请查看 actions 中的具体报错" {
		t.Fatalf("CloudAPIError action message = %q", payload.Message)
	}

	payload = NewLocalizer("en").ErrorPayload(ecerrorsPayload("MissingRegion", "region is required"), false)
	if payload.Message != "region is required" || payload.Suggestion != "Pass `--region <region>` or run `ecctl configure set region <region>`." {
		t.Fatalf("English payload = %#v", payload)
	}
}

func TestErrorPayloadPreservesCloudAndUnknownMessages(t *testing.T) {
	localizer := NewLocalizer("zh-CN")

	payload := localizer.ErrorPayload(ecerrorsPayload("CloudAPIError", "raw cloud error"), false)
	if payload.Message != "raw cloud error" {
		t.Fatalf("CloudAPIError without actions message = %q", payload.Message)
	}

	payload = localizer.ErrorPayload(ecerrorsPayload("UnknownProductError", "product said no"), false)
	if payload.Message != "product said no" {
		t.Fatalf("unknown code message = %q", payload.Message)
	}

	if !localizer.ShouldLocalizeHelp() {
		t.Fatal("Chinese localizer should localize help")
	}
	if NewLocalizer("en").ShouldLocalizeHelp() {
		t.Fatal("English localizer should not localize help")
	}
	if got := NewLocalizer("en").ErrorSuggestion("NoSuggestion"); got != "" {
		t.Fatalf("unknown suggestion = %q", got)
	}
	if got := NewLocalizer("en").ErrorSuggestion("InvalidDryRunAmount"); !strings.Contains(got, "--amount 1") {
		t.Fatalf("InvalidDryRunAmount English suggestion = %q", got)
	}
	if got := NewLocalizer("zh-CN").ErrorSuggestion("InvalidDryRunAmount"); !strings.Contains(got, "--amount 1") {
		t.Fatalf("InvalidDryRunAmount Chinese suggestion = %q", got)
	}
}

func TestMissingParameterNamesExtractsFlagsAndPlaceholders(t *testing.T) {
	got := missingParameterNames("missing required parameters: --name, <id> value")
	if len(got) != 2 || got[0] != "--name" || got[1] != "<id>" {
		t.Fatalf("missingParameterNames = %#v", got)
	}
}

func TestNewLocalizerCachesByResolvedLanguage(t *testing.T) {
	resetLocalizerCacheForTest()

	english := NewLocalizer("en")
	if got := NewLocalizer("en"); got != english {
		t.Fatalf("NewLocalizer(en) did not return cached localizer")
	}
	if got := NewLocalizer("fr-FR"); got != english {
		t.Fatalf("unsupported explicit language should resolve to cached English localizer")
	}

	chinese := NewLocalizer("zh-CN")
	if chinese == english {
		t.Fatalf("Chinese localizer reused English cache entry")
	}
	if got := NewLocalizer("zh-Hans"); got != chinese {
		t.Fatalf("zh-CN and zh-Hans should share resolved language cache entry")
	}
}

func TestRegisterMessageInvalidatesLocalizerCache(t *testing.T) {
	resetLocalizerCacheForTest()

	before := NewLocalizer("en")
	RegisterMessage(MessageSpec{
		ID: "CacheInvalidationExample",
		Text: map[string]string{
			"en":    "cache invalidated",
			"zh-CN": "缓存已失效",
		},
	})
	after := NewLocalizer("en")

	if after == before {
		t.Fatalf("RegisterMessage should invalidate cached localizer")
	}
	if got := after.Message("CacheInvalidationExample"); got != "cache invalidated" {
		t.Fatalf("registered message = %q, want cache invalidated", got)
	}
}

func TestCachedLocalizerSupportsConcurrentReads(t *testing.T) {
	resetLocalizerCacheForTest()

	localizer := NewLocalizer("zh-CN")
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if got := localizer.MessageData("HelpDefaultValue", map[string]any{"Value": "5m"}); got != "(默认 5m)" {
					t.Errorf("HelpDefaultValue = %q, want (默认 5m)", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestMessageOrDefaultReturnsDefault(t *testing.T) {
	localizer := NewLocalizer("en")
	if got := localizer.MessageOrDefault("NonExistentMessageID", "fallback-value"); got != "fallback-value" {
		t.Fatalf("MessageOrDefault = %q, want fallback-value", got)
	}
	if got := localizer.MessageOrDefault("MissingRegion", "fallback"); got == "fallback" {
		t.Fatalf("MessageOrDefault should return localized message, not fallback")
	}
}

func TestFlagUsageUnknownKeyPassesThrough(t *testing.T) {
	localizer := NewLocalizer("en")
	if got := localizer.FlagUsage("totally unknown flag usage"); got != "totally unknown flag usage" {
		t.Fatalf("FlagUsage for unknown key = %q, want passthrough", got)
	}
}

func TestCommandLongDataReturnsEmptyForMissing(t *testing.T) {
	localizer := NewLocalizer("en")
	if got := localizer.CommandLongData("nonexistent.path", "my fallback", nil); got != "my fallback" {
		t.Fatalf("CommandLongData for missing = %q, want fallback", got)
	}
}

func TestResolveLanguageFromLANG(t *testing.T) {
	env := map[string]string{"LANG": "zh_CN.UTF-8"}
	getenv := func(name string) string { return env[name] }
	if got := ResolveLanguage("", getenv); got != "zh-Hans" {
		t.Fatalf("ResolveLanguage LANG = %q, want zh-Hans", got)
	}
}

func TestResolveLanguageExplicitUnsupported(t *testing.T) {
	if got := ResolveLanguage("ja-JP", func(string) string { return "" }); got != "en" {
		t.Fatalf("ResolveLanguage unsupported explicit = %q, want en", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func ecerrorsPayload(code string, message string) ecerrors.ErrorPayload {
	return ecerrors.ErrorPayload{Code: code, Message: message}
}
