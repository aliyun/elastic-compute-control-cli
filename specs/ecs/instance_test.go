package ecs

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

type fakeOperationCaller struct {
	responses []map[string]any
	err       error
	calls     []fakeOperationCall
}

type fakeOperationCall struct {
	operation string
	request   map[string]any
}

func (f *fakeOperationCaller) CallRaw(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, fakeOperationCall{operation: operation, request: request})
	if f.err != nil {
		return nil, f.err
	}
	if len(f.responses) == 0 {
		return map[string]any{}, nil
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestValidateRunInstancesDryRunAmount(t *testing.T) {
	tests := []struct {
		name    string
		request map[string]any
		wantErr bool
	}{
		{name: "dry-run single instance", request: map[string]any{"DryRun": true, "Amount": 1}},
		{name: "dry-run defaults to single instance", request: map[string]any{"DryRun": true}},
		{name: "regular batch create", request: map[string]any{"DryRun": false, "Amount": 2}},
		{name: "dry-run batch create", request: map[string]any{"DryRun": true, "Amount": 2}, wantErr: true},
		{name: "dry-run zero amount", request: map[string]any{"DryRun": true, "Amount": 0}, wantErr: true},
		{name: "dry-run negative amount", request: map[string]any{"DryRun": true, "Amount": -1}, wantErr: true},
		{name: "dry-run invalid raw amount", request: map[string]any{"DryRun": true, "Amount": "not-an-integer"}, wantErr: true},
		{name: "dry-run batch minimum", request: map[string]any{"DryRun": true, "MinAmount": 2}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateRunInstancesDryRunAmount(context.Background(), &fakeOperationCaller{}, tt.request)
			if !tt.wantErr {
				if err != nil || got["Amount"] != tt.request["Amount"] {
					t.Fatalf("validateRunInstancesDryRunAmount() = (%#v, %v)", got, err)
				}
				return
			}
			var appErr *ecerrors.AppError
			if !stderrors.As(err, &appErr) || appErr.Payload().Code != "InvalidDryRunAmount" {
				t.Fatalf("error = %T %v, want InvalidDryRunAmount", err, err)
			}
			payload := appErr.Payload()
			wantField := "amount"
			if _, ok := tt.request["MinAmount"]; ok {
				wantField = "min_amount"
			}
			if payload.Field != wantField || len(payload.AcceptedValues) != 1 || payload.AcceptedValues[0] != "1" {
				t.Fatalf("payload = %#v", payload)
			}
		})
	}
}

func TestResolveRunInstancesImageSkipsExplicitImageID(t *testing.T) {
	caller := &fakeOperationCaller{}
	request := map[string]any{"ImageId": "aliyun_3.vhd"}
	got, err := resolveRunInstancesImage(context.Background(), caller, request)
	if err != nil {
		t.Fatalf("resolveRunInstancesImage: %v", err)
	}
	if got["ImageId"] != "aliyun_3.vhd" || len(caller.calls) != 0 {
		t.Fatalf("request=%#v calls=%#v", got, caller.calls)
	}
}

func TestResolveRunInstancesImageLooksUpNameWithInstanceType(t *testing.T) {
	caller := &fakeOperationCaller{responses: []map[string]any{{
		"Images": map[string]any{"Image": []any{map[string]any{"ImageId": "img-1"}}},
	}}}
	request := map[string]any{
		"RegionId":     "cn-beijing",
		"ImageId":      "alinux3",
		"InstanceType": "ecs.u1-c1m2.large",
	}
	got, err := resolveRunInstancesImage(context.Background(), caller, request)
	if err != nil {
		t.Fatalf("resolveRunInstancesImage: %v", err)
	}
	if got["ImageId"] != "img-1" || request["ImageId"] != "alinux3" {
		t.Fatalf("got=%#v original=%#v", got, request)
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "DescribeImages" {
		t.Fatalf("calls=%#v", caller.calls)
	}
	if caller.calls[0].request["ImageName"] != "*alinux3*" || caller.calls[0].request["InstanceType"] != "ecs.u1-c1m2.large" {
		t.Fatalf("lookup request=%#v", caller.calls[0].request)
	}
}

func TestDecodeInvocationOutputText(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "valid base64", value: "aGVsbG8K", want: "hello\n"},
		{name: "empty string", value: "", want: ""},
		{name: "invalid base64", value: "not base64", want: ""},
		{name: "non string", value: 42, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeInvocationOutputText(tt.value); got != tt.want {
				t.Fatalf("decodeInvocationOutputText(%#v) = %#v, want %#v", tt.value, got, tt.want)
			}
		})
	}
}

func TestResolveRunInstancesImageReportsMissingName(t *testing.T) {
	caller := &fakeOperationCaller{responses: []map[string]any{{"Images": map[string]any{"Image": []any{}}}}}
	_, err := resolveRunInstancesImage(context.Background(), caller, map[string]any{
		"ImageId":      "missing",
		"InstanceType": "ecs.u1",
	})
	if err == nil || !strings.Contains(err.Error(), `image "missing" not found`) || !strings.Contains(err.Error(), "ecs.u1") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveRunInstancesImageReportsMissingNameWithoutInstanceType(t *testing.T) {
	caller := &fakeOperationCaller{responses: []map[string]any{{"Images": map[string]any{"Image": []any{}}}}}
	_, err := resolveRunInstancesImage(context.Background(), caller, map[string]any{
		"ImageId": "missing",
	})
	if err == nil || err.Error() != `ImageNotFound: image "missing" not found` {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveRunInstancesImagePropagatesLookupError(t *testing.T) {
	caller := &fakeOperationCaller{err: ecerrors.Service("CloudAPIError", "DescribeImages failed", false)}

	_, err := resolveRunInstancesImage(context.Background(), caller, map[string]any{"ImageId": "alinux3"})
	if err == nil || !strings.Contains(err.Error(), "DescribeImages failed") {
		t.Fatalf("err=%v", err)
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "DescribeImages" {
		t.Fatalf("calls=%#v", caller.calls)
	}
}

func TestFirstECSImageIDSkipsMalformedItems(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
		want     string
	}{
		{name: "missing images object", response: map[string]any{}},
		{name: "image list not array", response: map[string]any{"Images": map[string]any{"Image": "bad"}}},
		{name: "skip malformed entries", response: map[string]any{"Images": map[string]any{"Image": []any{
			"bad",
			map[string]any{"ImageId": ""},
			map[string]any{"ImageId": "img-2"},
		}}}, want: "img-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstECSImageID(tt.response); got != tt.want {
				t.Fatalf("firstECSImageID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSuggestRunInstancesAvailabilityQueryForInstanceTypeStockError(t *testing.T) {
	source := ecerrors.Service("CloudAPIError", "stock unavailable", false,
		ecerrors.WithRequestID("req-run"),
		ecerrors.WithRawCause("InvalidResourceType.NotSupported", "instance type unsupported"),
	)
	request := map[string]any{
		"RegionId":           "cn-shanghai",
		"ZoneId":             "cn-shanghai-g",
		"InstanceType":       "ecs.g6.large",
		"InstanceChargeType": "PostPaid",
		"NetworkCategory":    "vpc",
		"IoOptimized":        "optimized",
	}

	err := suggestRunInstancesAvailabilityQuery(context.Background(), &fakeOperationCaller{}, request, source)
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatalf("err=%T %v, want AppError", err, err)
	}
	payload := appErr.Payload()
	if payload.Field != "type" {
		t.Fatalf("field = %q, want type", payload.Field)
	}
	for _, want := range []string{
		"ecctl call ecs DescribeAvailableResource",
		"--region cn-shanghai",
		"--DestinationResource InstanceType",
		"--ZoneId cn-shanghai-g",
		"--InstanceType ecs.g6.large",
		"--InstanceChargeType PostPaid",
		"--NetworkCategory vpc",
		"--IoOptimized optimized",
	} {
		if !strings.Contains(payload.SuggestedAction, want) {
			t.Fatalf("suggested_action missing %q: %q", want, payload.SuggestedAction)
		}
	}
}

func TestSuggestRunInstancesAvailabilityQueryForSystemDiskStockError(t *testing.T) {
	source := ecerrors.Service("CloudAPIError", "disk unavailable", false,
		ecerrors.WithRawCause("InvalidDiskCategory.NotSupported", "disk category unsupported"),
	)
	request := map[string]any{
		"RegionId":            "cn-shanghai",
		"SystemDisk.Category": "cloud_essd",
	}

	err := suggestRunInstancesAvailabilityQuery(context.Background(), &fakeOperationCaller{}, request, source)
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatalf("err=%T %v, want AppError", err, err)
	}
	payload := appErr.Payload()
	if payload.Field != "system_disk.category" {
		t.Fatalf("field = %q, want system_disk.category", payload.Field)
	}
	for _, want := range []string{"--DestinationResource SystemDisk", "--SystemDiskCategory cloud_essd"} {
		if !strings.Contains(payload.SuggestedAction, want) {
			t.Fatalf("suggested_action missing %q: %q", want, payload.SuggestedAction)
		}
	}
}

func TestSuggestRunInstancesAvailabilityQueryLeavesUnrelatedErrorsUntouched(t *testing.T) {
	source := ecerrors.Service("CloudAPIError", "other error", false,
		ecerrors.WithRawCause("InvalidImageId.NotFound", "image not found"),
	)

	err := suggestRunInstancesAvailabilityQuery(context.Background(), &fakeOperationCaller{}, map[string]any{}, source)
	if err != source {
		t.Fatalf("err = %#v, want original error", err)
	}
}

func TestSuggestForceReleaseWithoutForcePointsAtForceFlag(t *testing.T) {
	source := ecerrors.Service("CloudAPIError", "current status does not support delete", false,
		ecerrors.WithRawCause("IncorrectInstanceStatus", "instance is running"),
	)

	err := suggestForceRelease(context.Background(), &fakeOperationCaller{}, map[string]any{"InstanceId": "i-1"}, source)
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatalf("err=%T %v, want AppError", err, err)
	}
	if got := appErr.Payload().Suggestion; !strings.Contains(got, "--force") {
		t.Fatalf("suggestion = %q, want it to mention --force", got)
	}
}

func TestSuggestForceReleaseWithForceSuggestsRetry(t *testing.T) {
	source := ecerrors.Service("CloudAPIError", "current status does not support delete", false,
		ecerrors.WithRawCause("IncorrectInstanceStatus", "instance is initializing"),
	)

	err := suggestForceRelease(context.Background(), &fakeOperationCaller{}, map[string]any{"Force": true}, source)
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatalf("err=%T %v, want AppError", err, err)
	}
	if got := appErr.Payload().Suggestion; strings.Contains(got, "--force") {
		t.Fatalf("suggestion = %q, should not re-suggest --force when already forcing", got)
	}
}

func TestSuggestForceReleaseLeavesUnrelatedErrorsUntouched(t *testing.T) {
	source := ecerrors.Service("CloudAPIError", "other error", false,
		ecerrors.WithRawCause("InvalidInstanceId.NotFound", "instance not found"),
	)

	err := suggestForceRelease(context.Background(), &fakeOperationCaller{}, map[string]any{}, source)
	if err != source {
		t.Fatalf("err = %#v, want original error", err)
	}
}

func TestAvailabilitySuggestedActionQuotesShellSensitiveValues(t *testing.T) {
	got := availabilitySuggestedAction(map[string]any{
		"RegionId":     "cn shanghai",
		"ZoneId":       "cn-shanghai g",
		"InstanceType": "ecs.g6 large",
	}, "InstanceType")

	for _, want := range []string{
		`--region "cn shanghai"`,
		`--DestinationResource InstanceType`,
		`--ZoneId "cn-shanghai g"`,
		`--InstanceType "ecs.g6 large"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("suggested action missing %q: %q", want, got)
		}
	}
}

func TestAvailabilitySuggestedActionUsesRegionPlaceholder(t *testing.T) {
	got := availabilitySuggestedAction(map[string]any{}, "InstanceType")
	if got != "ecctl call ecs DescribeAvailableResource --region <region> --DestinationResource InstanceType" {
		t.Fatalf("suggested action = %q", got)
	}
}

func TestAvailabilityErrorTarget(t *testing.T) {
	tests := []struct {
		code            string
		wantField       string
		wantDestination string
	}{
		{code: "InvalidResourceType.NotSupported", wantField: "type", wantDestination: "InstanceType"},
		{code: "InvalidDiskCategory.NotSupported", wantField: "system_disk.category", wantDestination: "SystemDisk"},
		{code: "InvalidImageId.NotFound"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			gotField, gotDestination := availabilityErrorTarget(tt.code)
			if gotField != tt.wantField || gotDestination != tt.wantDestination {
				t.Fatalf("availabilityErrorTarget = (%q, %q), want (%q, %q)", gotField, gotDestination, tt.wantField, tt.wantDestination)
			}
		})
	}
}
