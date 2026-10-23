package journalcmd

import "testing"

func TestRawQuotaMutationCandidate(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{`ecctl call fcsandbox UpdateQuota --request '{"tagValue":"foreign"}'`, true},
		{`ecctl --region cn-hangzhou call -o json FCSandbox DeleteQuota --request '{"tagValue":"foreign"}'`, true},
		{`ecctl call --profile quota-test sandbox --request '{}' updatequota`, true},
		{`ecctl --future-flag value call fcsandbox --future-flag value DeleteQuota`, true},
		{`ecctl call sbx DeleteQuota --request '{broken`, true},
		{`ecctl call fcsandbox DescribeQuota --request '{"action":"DeleteQuota"}'`, false},
		{`ecctl call fcsandbox DeleteTeam --request '{}'`, false},
		{`ecctl call sts GetCallerIdentity`, false},
		{`ecctl sandbox quota update team-owned --cpu-cores 2`, false},
	} {
		if got := RawQuotaMutationCandidate(tc.command); got != tc.want {
			t.Errorf("RawQuotaMutationCandidate(%q) = %t, want %t", tc.command, got, tc.want)
		}
	}
}
