package ack

import (
	"context"
	"reflect"
	"testing"
)

func TestDisableControlPlaneLogComponentsSetsEmptyComponents(t *testing.T) {
	request := map[string]any{
		"cluster_id": "c-123",
		"components": []string{"apiserver"},
	}

	got, err := disableControlPlaneLogComponents(context.Background(), nil, request)
	if err != nil {
		t.Fatalf("disableControlPlaneLogComponents: %v", err)
	}
	if got["cluster_id"] != "c-123" {
		t.Fatalf("cluster_id = %#v", got["cluster_id"])
	}
	if !reflect.DeepEqual(got["components"], []string{}) {
		t.Fatalf("components = %#v, want empty []string", got["components"])
	}
	if !reflect.DeepEqual(request["components"], []string{"apiserver"}) {
		t.Fatalf("original request was mutated: %#v", request)
	}
}
