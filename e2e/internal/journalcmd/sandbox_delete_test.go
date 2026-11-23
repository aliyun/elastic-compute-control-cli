package journalcmd

import "testing"

func TestSandboxDeleteRouteUsesEffectiveTransportWithGlobalFlags(t *testing.T) {
	for _, tc := range []struct {
		command string
		native  bool
		region  string
		profile string
	}{
		{"ecctl --profile owner sandbox template delete tpl-1 --openapi=false --output=json", false, "", "owner"},
		{"ecctl sandbox template delete tpl-1 --profile=owner --openapi --region cn-hangzhou", true, "cn-hangzhou", "owner"},
		{"ecctl sbx tpl rm tpl-1 --openapi --region cn-hangzhou", true, "cn-hangzhou", ""},
		{"ecctl sandbox template remove tpl-1 --openapi --region cn-hangzhou", true, "cn-hangzhou", ""},
		{"ecctl --output=text sandbox sandbox delete sbx-1 --profile owner", false, "", "owner"},
	} {
		route, found, err := ParseSandboxDeleteRoute(tc.command)
		if err != nil || !found || route.Native != tc.native || route.Region != tc.region || route.Profile != tc.profile {
			t.Errorf("route for %q = %+v, %t, %v", tc.command, route, found, err)
		}
	}
	if route, found, err := ParseSandboxDeleteRoute("ecctl sandbox template delete tpl-1 --openapi=garbage"); err == nil {
		t.Fatalf("invalid transport admitted: %+v %t %v", route, found, err)
	}
}
