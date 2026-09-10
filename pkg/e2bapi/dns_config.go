package e2bapi

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

func dnsServer(address, port string) (string, error) {
	ip := strings.SplitN(address, "%", 2)[0]
	if net.ParseIP(ip) == nil {
		return "", errors.New("invalid system DNS server")
	}
	if port == "" {
		port = "53"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("invalid system DNS port")
	}
	return net.JoinHostPort(address, port), nil
}

func resolvConfServers(data string) ([]string, error) {
	var servers []string
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		server, err := dnsServer(fields[1], "")
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	if len(servers) == 0 {
		return nil, errors.New("no system DNS servers")
	}
	return servers, nil
}

// scutil exposes macOS's supplemental (split DNS) resolvers, which are not
// represented in /etc/resolv.conf. Select the longest matching domain, then
// its lowest search order. Interface-scoped queries are not unscoped routes.
func scutilServers(data, host string) ([]string, error) {
	type resolver struct {
		domain, port, options string
		addresses             []string
		order                 int
	}
	var resolvers []resolver
	var current *resolver
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DNS configuration (for ") {
			break
		}
		if strings.HasPrefix(line, "resolver #") {
			resolvers = append(resolvers, resolver{})
			current = &resolvers[len(resolvers)-1]
			continue
		}
		if current == nil {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case key == "domain":
			current.domain = normalizeDNSName(value)
		case key == "port":
			current.port = value
		case key == "options":
			current.options = value
		case key == "order":
			var err error
			current.order, err = strconv.Atoi(value)
			if err != nil {
				return nil, errors.New("invalid system DNS order")
			}
		case strings.HasPrefix(key, "nameserver["):
			current.addresses = append(current.addresses, value)
		}
	}
	host = normalizeDNSName(host)
	var chosen *resolver
	for i := range resolvers {
		r := &resolvers[i]
		if r.domain != "" && !domainWithin(host, r.domain) {
			continue
		}
		if chosen == nil || len(r.domain) > len(chosen.domain) || (len(r.domain) == len(chosen.domain) && r.order < chosen.order) {
			chosen = r
		}
	}
	if chosen == nil || len(chosen.addresses) == 0 || strings.Contains(chosen.options, "mdns") {
		return nil, errors.New("no usable system DNS route")
	}
	var servers []string
	for _, address := range chosen.addresses {
		server, err := dnsServer(address, chosen.port)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, nil
}
