package regionselect

import (
	"errors"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestClassifyRetriesOnlyRegionUnavailableFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		run    *report.Run
		err    error
		want   bool
		reason string
	}{
		{
			name: "structured region error",
			run:  &report.Run{Cases: []report.Case{{Status: report.StatusFail, Steps: []report.Step{{Status: report.StatusFail, Error: `{"error":{"code":"InvalidRegionId"}}`}}}}},
			want: true,
		},
		{
			name: "forbidden region error",
			run:  &report.Run{Cases: []report.Case{{Status: report.StatusFail, Error: `{"error":{"code":"Forbidden.Region"}}`}}},
			want: true,
		},
		{
			name: "no compatible stock",
			err:  errors.New("no compatible ECS test combination found in region cn-hangzhou"),
			want: true,
		},
		{
			name: "no VSwitch-capable zone",
			err:  errors.New("no ECS zone supporting VSwitch creation found in region cn-hangzhou"),
			want: true,
		},
		{
			name: "explicit zone has no compatible stock",
			err:  errors.New("no compatible ECS test combination found in explicit zone cn-hangzhou-a"),
			want: true,
		},
		{
			name: "ACK inventory unavailable",
			err:  errors.New("no compatible creatable ACK cluster version found in region cn-hangzhou"),
			want: true,
		},
		{
			name: "Lingjun inventory unavailable",
			err:  errors.New("no compatible Lingjun node profile found in region cn-hangzhou"),
			want: true,
		},
		{
			name: "Lingjun cluster type unavailable",
			err:  errors.New("no compatible Lingjun cluster type \"Lite\" found in region cn-hangzhou"),
			want: true,
		},
		{
			name: "permission failure",
			run:  &report.Run{Cases: []report.Case{{Status: report.StatusError, Error: `{"error":{"code":"AccessDenied"}}`}}},
			want: false,
		},
		{
			name: "region marker with quota failure",
			err:  errors.New("InvalidRegionId; QuotaExceeded"),
			want: false,
		},
		{
			name: "region marker with access failure",
			err:  errors.New("Forbidden.Region; AccessDenied"),
			want: false,
		},
		{
			name: "assertion failure",
			run:  &report.Run{Cases: []report.Case{{Status: report.StatusFail, Steps: []report.Step{{Status: report.StatusFail, Error: "expected id to exist"}}}}},
			want: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := Classify(tt.run, tt.err)
			if got != tt.want {
				t.Fatalf("retry = %v, want %v (reason=%q)", got, tt.want, reason)
			}
			if tt.want && reason == "" {
				t.Fatal("retry classification must include a reason")
			}
		})
	}
}
