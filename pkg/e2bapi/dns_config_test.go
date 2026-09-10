package e2bapi

import (
	"reflect"
	"testing"
)

func TestSystemDNSConfiguration(t *testing.T) {
	servers, err := resolvConfServers("# system config\nnameserver 127.0.0.53\nnameserver ::1\nsearch example.com\n")
	if err != nil || !reflect.DeepEqual(servers, []string{"127.0.0.53:53", "[::1]:53"}) {
		t.Fatalf("servers=%v err=%v", servers, err)
	}
	for _, bad := range []string{"", "nameserver attacker.example", "nameserver 999.0.0.1"} {
		if _, err := resolvConfServers(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	data := `DNS configuration
resolver #1
  nameserver[0] : 192.0.2.1
resolver #2
  domain : corp.example
  nameserver[0] : 192.0.2.2
  order : 200
resolver #3
  domain : corp.example
  nameserver[0] : 192.0.2.3
  nameserver[1] : fe80::1%en0
  port : 5353
  order : 100
resolver #4
  domain : nested.corp.example
  nameserver[0] : 192.0.2.4
resolver #5
  domain : local
  options : mdns
DNS configuration (for scoped queries)
resolver #1
  domain : nested.corp.example
  nameserver[0] : 192.0.2.99
`
	for _, tt := range []struct {
		host    string
		servers []string
	}{
		{"public.example", []string{"192.0.2.1:53"}},
		{"notcorp.example", []string{"192.0.2.1:53"}},
		{"API.CORP.EXAMPLE.", []string{"192.0.2.3:5353", "[fe80::1%en0]:5353"}},
		{"api.nested.corp.example", []string{"192.0.2.4:53"}},
	} {
		servers, err := scutilServers(data, tt.host)
		if err != nil || !reflect.DeepEqual(servers, tt.servers) {
			t.Fatalf("%s servers=%v err=%v", tt.host, servers, err)
		}
	}
	if _, err := scutilServers(data, "api.local"); err == nil {
		t.Fatal("mDNS silently fell through to default resolver")
	}
	if _, err := scutilServers("", "api.example"); err == nil {
		t.Fatal("empty system configuration accepted")
	}
}
