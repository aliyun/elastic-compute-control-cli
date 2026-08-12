package ack

import (
	"context"
	"reflect"
	"testing"
)

func TestDisableControlPlaneLogComponentsSetsEmptyComponents(t *testing.T) {
	request := map[string]any{
		"cluster_id":      "c-123",
		"body.components": []string{"apiserver"},
	}

	got, err := disableControlPlaneLogComponents(context.Background(), nil, request)
	if err != nil {
		t.Fatalf("disableControlPlaneLogComponents: %v", err)
	}
	if got["cluster_id"] != "c-123" {
		t.Fatalf("cluster_id = %#v", got["cluster_id"])
	}
	if !reflect.DeepEqual(got["body.components"], []string{}) {
		t.Fatalf("body.components = %#v, want empty []string", got["body.components"])
	}
	if !reflect.DeepEqual(request["body.components"], []string{"apiserver"}) {
		t.Fatalf("original request was mutated: %#v", request)
	}
}
