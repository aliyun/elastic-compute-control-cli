package ecs

import "testing"

func TestAssistantUpgradeEnabledNormalizesProviderRepresentations(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  bool
	}{
		{nil, false}, {map[string]any{}, false},
		{map[string]any{"Enabled": true}, true}, {map[string]any{"Enabled": false}, false},
		{map[string]any{"Enabled": " true "}, true}, {map[string]any{"Enabled": "invalid"}, false},
		{map[string]any{"Enabled": 1}, false},
	} {
		if got := normalizeAssistantAgentUpgradeEnabled(tc.value); got != tc.want {
			t.Fatalf("upgrade setting %v=%v", tc.value, got)
		}
	}
}
