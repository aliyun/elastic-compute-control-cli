package journalcmd

import "testing"

func TestValidateDelete(t *testing.T) {
	tests := []struct {
		name    string
		command string
		valid   bool
	}{
		{name: "first class delete", command: "ecctl ecs instance delete i-1 --force", valid: true},
		{name: "delete object", command: "ecctl call oss DeleteObject --Bucket e2e-bucket --Key export.raw --region cn-hangzhou", valid: true},
		{name: "delete bucket equals", command: "ecctl call oss DeleteBucket --Bucket=e2e-bucket --region=cn-hangzhou", valid: true},
		{name: "restore associated transfer enabled", command: "ecctl rg associated-transfer update --status Enable --enable-existing-resources-transfer true", valid: true},
		{name: "restore associated transfer disabled equals", command: "ecctl rg associated-transfer update --enable-existing-resources-transfer=false --status=Disable", valid: true},
		{name: "arbitrary first class update", command: "ecctl rg group update rg-1 --name changed", valid: false},
		{name: "restore missing existing resources flag", command: "ecctl rg associated-transfer update --status Enable", valid: false},
		{name: "restore invalid status", command: "ecctl rg associated-transfer update --status Pending --enable-existing-resources-transfer true", valid: false},
		{name: "restore invalid boolean", command: "ecctl rg associated-transfer update --status Enable --enable-existing-resources-transfer yes", valid: false},
		{name: "restore extra parameter", command: "ecctl rg associated-transfer update --status Enable --enable-existing-resources-transfer true --region cn-hangzhou", valid: false},
		{name: "arbitrary call", command: "ecctl call ecs DeleteInstance --InstanceId i-1", valid: false},
		{name: "oss put", command: "ecctl call oss PutObject --Bucket e2e-bucket --Key export.raw --File /tmp/export.raw", valid: false},
		{name: "missing bucket", command: "ecctl call oss DeleteObject --Key export.raw", valid: false},
		{name: "missing key", command: "ecctl call oss DeleteObject --Bucket e2e-bucket", valid: false},
		{name: "request injection", command: "ecctl call oss DeleteBucket --Bucket e2e-bucket --request payload.json", valid: false},
		{name: "duplicate", command: "ecctl call oss DeleteBucket --Bucket one --Bucket two", valid: false},
		{name: "extra positional", command: "ecctl call oss DeleteBucket unexpected --Bucket e2e-bucket", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDelete(test.command)
			if test.valid && err != nil {
				t.Fatalf("ValidateDelete: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("ValidateDelete unexpectedly accepted command")
			}
		})
	}
}
