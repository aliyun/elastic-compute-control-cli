package journalcmd

import (
	"strings"

	"github.com/google/shlex"
)

// RawQuotaMutationCandidate rejects unsupported raw Quota writes in E2E cases.
// Flag values are skipped so request JSON cannot masquerade as command path.
func RawQuotaMutationCandidate(command string) bool {
	tokens, err := shlex.Split(command)
	if err != nil {
		// An unparseable command cannot safely establish a read-only raw call.
		lower := strings.ToLower(command)
		return strings.Contains(lower, "call") &&
			(strings.Contains(lower, "fcsandbox") || strings.Contains(lower, "fc-sandbox") || strings.Contains(lower, "sandbox") || strings.Contains(lower, "sbx")) &&
			(strings.Contains(lower, "updatequota") || strings.Contains(lower, "deletequota"))
	}
	if len(tokens) == 0 || tokens[0] != "ecctl" {
		return false
	}
	path := make([]string, 0, 3)
	for i := 1; i < len(tokens); i++ {
		token := tokens[i]
		if strings.HasPrefix(token, "-") {
			name, _, inline := strings.Cut(token, "=")
			if !inline && rawQuotaValueFlag(name) && i+1 < len(tokens) {
				i++
			}
			continue
		}
		path = append(path, strings.ToLower(token))
	}
	for start := range path {
		if path[start] != "call" {
			continue
		}
		for product := start + 1; product < len(path); product++ {
			if !rawQuotaProduct(path[product]) {
				continue
			}
			for action := product + 1; action < len(path); action++ {
				if path[action] == "updatequota" || path[action] == "deletequota" {
					return true
				}
			}
		}
	}
	return false
}

func rawQuotaProduct(value string) bool {
	return value == "fcsandbox" || value == "fc-sandbox" || value == "sandbox" || value == "sbx"
}

func rawQuotaValueFlag(name string) bool {
	switch name {
	case "--region", "--profile", "-p", "--lang", "--output", "-o", "--request", "--api-param", "--aliyun-arg", "--filter", "--limit", "--fields":
		return true
	}
	return false
}
