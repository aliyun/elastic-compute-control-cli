package e2bapi

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

const fcDomain = "e2b.fc.aliyuncs.com"
const backendLookupTimeout = 3 * time.Second
const maxCNAMEHops = 8

// A lookup returns answer-section CNAME owner -> target pairs, not a final
// canonical name. FC hostnames themselves can alias to non-FC NLB hostnames.
type cnameLookup func(context.Context, string) (map[string]string, error)

type backendDetection struct {
	fc     bool
	reason string
}

func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

func domainWithin(name, suffix string) bool {
	return name == suffix || strings.HasSuffix(name, "."+suffix)
}

func (c *Caller) detectBackend(ctx context.Context) backendDetection {
	c.backendMu.Lock()
	cached := c.backendResult
	c.backendMu.Unlock()
	if cached != nil {
		return *cached
	}
	ctx, cancel := context.WithTimeout(ctx, backendLookupTimeout)
	defer cancel()
	result, err := identifyBackend(ctx, c.endpoint.Hostname(), c.lookupCNAME)
	if err == nil {
		c.backendMu.Lock()
		c.backendResult = &result
		c.backendMu.Unlock()
	}
	return result
}

func identifyBackend(ctx context.Context, host string, lookup cnameLookup) (backendDetection, error) {
	current := normalizeDNSName(host)
	if domainWithin(current, fcDomain) {
		return backendDetection{fc: true}, nil
	}
	if domainWithin(current, defaultDomain) {
		return backendDetection{reason: "e2b_domain"}, nil
	}
	if net.ParseIP(current) != nil {
		return backendDetection{reason: "ip_endpoint"}, nil
	}
	seen := map[string]bool{}
	records := map[string]string{}
	for hop := 0; hop <= maxCNAMEHops; hop++ {
		if domainWithin(current, fcDomain) {
			return backendDetection{fc: true}, nil
		}
		if seen[current] {
			return backendDetection{reason: "cname_loop"}, errors.New("CNAME loop")
		}
		seen[current] = true
		if hop == maxCNAMEHops {
			return backendDetection{reason: "cname_hop_limit"}, errors.New("CNAME hop limit")
		}
		next, ok := records[current]
		if !ok {
			if err := ctx.Err(); err != nil {
				return backendDetection{reason: "dns_timeout"}, err
			}
			answer, err := lookup(ctx, current)
			if err != nil {
				reason := "dns_lookup_failed"
				if ctx.Err() != nil {
					reason = "dns_timeout"
				}
				return backendDetection{reason: reason}, err
			}
			for owner, target := range answer {
				owner, target = normalizeDNSName(owner), normalizeDNSName(target)
				if !validDNSName(owner) || !validDNSName(target) {
					return backendDetection{reason: "invalid_cname"}, errors.New("invalid CNAME")
				}
				if old, exists := records[owner]; exists && old != target {
					return backendDetection{reason: "conflicting_cname"}, errors.New("conflicting CNAME")
				}
				records[owner] = target
			}
			next, ok = records[current]
			if !ok {
				return backendDetection{reason: "no_fc_cname"}, nil
			}
		}
		current = next
	}
	panic("unreachable")
}

func validDNSName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
				return false
			}
		}
	}
	return true
}
