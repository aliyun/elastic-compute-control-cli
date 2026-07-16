package ecs

import (
	"fmt"
	"testing"
)

func TestNormalizeSecurityGroupRuleFromShorthand(t *testing.T) {
	got, err := normalizeSecurityGroupRule(map[string]any{
		"value":    "tcp:80@0.0.0.0/0",
		"policy":   "Accept",
		"priority": uint64(5),
	}, 0)
	if err != nil {
		t.Fatalf("normalizeSecurityGroupRule: %v", err)
	}
	if got["direction"] != "ingress" || got["protocol"] != "tcp" || got["port"] != "80" ||
		got["port_range"] != "80/80" || got["cidr"] != "0.0.0.0/0" ||
		got["policy"] != "accept" || got["priority"] != 5 {
		t.Fatalf("normalized rule = %#v", got)
	}
}

func TestNormalizeSecurityGroupRuleUsesDefaultDirectionForShortRule(t *testing.T) {
	got, err := normalizeSecurityGroupRule(map[string]any{
		"value":     "tcp:443@0.0.0.0/0",
		"direction": "egress",
	}, 0)
	if err != nil {
		t.Fatalf("normalizeSecurityGroupRule: %v", err)
	}
	if got["direction"] != "egress" || got["port_range"] != "443/443" {
		t.Fatalf("normalized rule = %#v", got)
	}
}

func TestNormalizeSecurityGroupRuleRequest(t *testing.T) {
	got, err := normalizeSecurityGroupRuleRequest(nil, nil, map[string]any{
		"IpProtocol": "TCP",
		"PortRange":  "443",
		"Policy":     "Accept",
		"Priority":   "5",
	})
	if err != nil {
		t.Fatalf("normalizeSecurityGroupRuleRequest: %v", err)
	}
	if got["IpProtocol"] != "tcp" || got["PortRange"] != "443/443" || got["Policy"] != "accept" || got["Priority"] != 5 {
		t.Fatalf("request = %#v", got)
	}
}

func TestNormalizeSecurityGroupRuleFromFieldsAndRanges(t *testing.T) {
	got, err := normalizeSecurityGroupRule(map[string]any{
		"direction": "INGRESS",
		"protocol":  "TCP",
		"port":      "100/200",
		"cidr":      "10.0.0.0/8",
		"priority":  "7",
	}, 1)
	if err != nil {
		t.Fatalf("normalizeSecurityGroupRule: %v", err)
	}
	if got["port"] != "100-200" || got["port_range"] != "100/200" || got["priority"] != 7 {
		t.Fatalf("normalized rule = %#v", got)
	}
}

func TestNormalizeSecurityGroupRuleRejectsInvalidOrIncompleteRules(t *testing.T) {
	if _, err := normalizeSecurityGroupRule(map[string]any{"value": "bad"}, 0); err == nil {
		t.Fatal("invalid shorthand succeeded")
	}
	if _, err := normalizeSecurityGroupRule(map[string]any{"protocol": "tcp", "port": "80"}, 1); err == nil {
		t.Fatal("incomplete rule succeeded")
	}
}

func TestNormalizeSecurityGroupRuleICMP(t *testing.T) {
	got, err := normalizeSecurityGroupRule(map[string]any{
		"value": "icmp:-1@0.0.0.0/0",
	}, 0)
	if err != nil {
		t.Fatalf("normalizeSecurityGroupRule: %v", err)
	}
	if got["protocol"] != "icmp" || got["port"] != "-1" || got["port_range"] != "-1/-1" ||
		got["cidr"] != "0.0.0.0/0" || got["direction"] != "ingress" {
		t.Fatalf("normalized ICMP rule = %#v", got)
	}
}

func TestSecurityGroupRuleParsingVariants(t *testing.T) {
	for _, raw := range []string{
		"tcp:80:0.0.0.0/0",
		"ingress:tcp:80:0.0.0.0/0",
		"tcp:80@0.0.0.0/0",
	} {
		if got, ok := parseSecurityGroupRule(raw); !ok || got["protocol"] != "tcp" {
			t.Fatalf("parseSecurityGroupRule(%q) = %#v, %v", raw, got, ok)
		}
	}
	if got := securityGroupRulePortRange("100-200"); got != "100/200" {
		t.Fatalf("securityGroupRulePortRange = %q", got)
	}
	if got := sgStringValue(fmt.Stringer(testSGStringer("value"))); got != "value" {
		t.Fatalf("sgStringValue stringer = %q", got)
	}
}

func TestSecurityGroupRulePortRangeEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"80/80", "80/80"},
		{"80", "80/80"},
		{"100-200", "100/200"},
		{"-1", "-1/-1"},
		{"-1-0", "-1/0"},
	}
	for _, tt := range tests {
		if got := securityGroupRulePortRange(tt.input); got != tt.want {
			t.Errorf("securityGroupRulePortRange(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeSecurityGroupRulePortEdgeCases(t *testing.T) {
	if got := normalizeSecurityGroupRulePort("80"); got != "80" {
		t.Fatalf("normalizeSecurityGroupRulePort(80) = %q, want 80", got)
	}
	if got := normalizeSecurityGroupRulePort("80/80"); got != "80" {
		t.Fatalf("normalizeSecurityGroupRulePort(80/80) = %q, want 80", got)
	}
	if got := normalizeSecurityGroupRulePort("80/443"); got != "80-443" {
		t.Fatalf("normalizeSecurityGroupRulePort(80/443) = %q, want 80-443", got)
	}
}

func TestIntHookValueCoversAllTypes(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want int
		ok   bool
	}{
		{"int", int(42), 42, true},
		{"int64", int64(42), 42, true},
		{"uint64", uint64(42), 42, true},
		{"uint", uint(42), 42, true},
		{"float64", float64(42), 42, true},
		{"string", "42", 42, true},
		{"invalid string", "abc", 0, false},
		{"nil", nil, 0, false},
		{"bool", true, 0, false},
	}
	for _, tt := range tests {
		got, ok := intHookValue(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("intHookValue(%v) = (%d, %v), want (%d, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSgStringValueCoversAllTypes(t *testing.T) {
	if got := sgStringValue(nil); got != "" {
		t.Fatalf("sgStringValue(nil) = %q, want empty", got)
	}
	if got := sgStringValue("hello"); got != "hello" {
		t.Fatalf("sgStringValue(string) = %q, want hello", got)
	}
	if got := sgStringValue(42); got != "42" {
		t.Fatalf("sgStringValue(int) = %q, want 42", got)
	}
}

func TestSecurityGroupRuleHasExplicitDirection(t *testing.T) {
	if securityGroupRuleHasExplicitDirection("tcp:80@0.0.0.0/0") {
		t.Fatal("@ format should not have explicit direction")
	}
	if !securityGroupRuleHasExplicitDirection("ingress:tcp:80:0.0.0.0/0") {
		t.Fatal("4-part colon format should have explicit direction")
	}
	if securityGroupRuleHasExplicitDirection("tcp:80:0.0.0.0/0") {
		t.Fatal("3-part colon format should not have explicit direction")
	}
}

func TestNormalizeSecurityGroupRuleWithExplicitDirection(t *testing.T) {
	got, err := normalizeSecurityGroupRule(map[string]any{
		"value": "egress:tcp:443:10.0.0.0/8",
	}, 0)
	if err != nil {
		t.Fatalf("normalizeSecurityGroupRule: %v", err)
	}
	if got["direction"] != "egress" || got["protocol"] != "tcp" || got["port"] != "443" ||
		got["cidr"] != "10.0.0.0/8" {
		t.Fatalf("normalized rule = %#v", got)
	}
}

type testSGStringer string

func (s testSGStringer) String() string { return string(s) }
