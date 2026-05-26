package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClusterSpecMarshalOmitsEmptyKubeConfig(t *testing.T) {
	payload, err := json.Marshal(ClusterSpec{
		Token:      "token-1",
		Endpoint:   "https://192.0.2.170:6443",
		KubeConfig: []byte{},
	})
	if err != nil {
		t.Fatalf("marshal ClusterSpec: %v", err)
	}
	if strings.Contains(string(payload), `"kubeConfig"`) {
		t.Fatalf("expected empty kubeConfig to be omitted, got %s", payload)
	}
	if !strings.Contains(string(payload), `"token":"token-1"`) {
		t.Fatalf("expected token to be preserved, got %s", payload)
	}
	if !strings.Contains(string(payload), `"endpoint":"https://192.0.2.170:6443"`) {
		t.Fatalf("expected endpoint to be preserved, got %s", payload)
	}
}

func TestClusterSpecMarshalIncludesNonEmptyKubeConfig(t *testing.T) {
	payload, err := json.Marshal(ClusterSpec{
		KubeConfig: []byte("apiVersion: v1"),
	})
	if err != nil {
		t.Fatalf("marshal ClusterSpec: %v", err)
	}
	if !strings.Contains(string(payload), `"kubeConfig":"YXBpVmVyc2lvbjogdjE="`) {
		t.Fatalf("expected non-empty kubeConfig to be present, got %s", payload)
	}
}
