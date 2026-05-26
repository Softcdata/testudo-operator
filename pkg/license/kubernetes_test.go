package license

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKubernetesStoreEvaluateClassifiesContentBeforeFingerprint(t *testing.T) {
	ctx := context.Background()
	store := KubernetesStore{
		Client: newLicenseRuntimeClient(t, BuildLicenseSecret(DefaultLicenseNamespace, []byte(`{}`))),
		CAPath: "/path/that/does/not/exist",
	}

	entitlement := store.Evaluate(ctx, NewDefaultVerifier())

	if entitlement.State != StateMalformed {
		t.Fatalf("expected malformed state, got state=%s reason=%s message=%s", entitlement.State, entitlement.Reason, entitlement.Message)
	}
	if entitlement.Reason != ReasonLicenseInvalid {
		t.Fatalf("expected license invalid reason, got %q", entitlement.Reason)
	}
	if strings.Contains(entitlement.Message, "read API server CA") {
		t.Fatalf("license content error was masked by fingerprint error: %s", entitlement.Message)
	}
}

func TestKubernetesStoreEvaluateUnknownKeyBeforeFingerprint(t *testing.T) {
	ctx := context.Background()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	store := KubernetesStore{
		Client: newLicenseRuntimeClient(t, BuildLicenseSecret(DefaultLicenseNamespace, signedTestLicense(t, privateKey, "unknown-key", func(claims map[string]any) {}))),
		CAPath: "/path/that/does/not/exist",
	}

	entitlement := store.Evaluate(ctx, NewDefaultVerifier())

	if entitlement.State != StateUnknownKey {
		t.Fatalf("expected unknown key state, got state=%s reason=%s message=%s", entitlement.State, entitlement.Reason, entitlement.Message)
	}
	if entitlement.Reason != ReasonLicenseUnknownKey {
		t.Fatalf("expected unknown key reason, got %q", entitlement.Reason)
	}
}

func TestKubernetesStoreEvaluateFingerprintEnvironmentInvalid(t *testing.T) {
	ctx := context.Background()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	store := KubernetesStore{
		Client: newLicenseRuntimeClient(t,
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: metav1.NamespaceSystem, UID: types.UID("kube-system-uid")}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: DefaultLicenseNamespace, UID: types.UID("platform-uid")}},
			BuildLicenseSecret(DefaultLicenseNamespace, signedTestLicense(t, privateKey, "test-key", func(claims map[string]any) {})),
		),
		CAPath: "/path/that/does/not/exist",
	}
	verifier := NewVerifier(map[string]ed25519.PublicKey{"test-key": publicKey})
	verifier.Now = func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }

	entitlement := store.Evaluate(ctx, verifier)

	if entitlement.State != StateUnknown {
		t.Fatalf("expected unknown state, got state=%s reason=%s message=%s", entitlement.State, entitlement.Reason, entitlement.Message)
	}
	if entitlement.Reason != ReasonLicenseEnvironmentInvalid {
		t.Fatalf("expected environment invalid reason, got %q", entitlement.Reason)
	}
	if !strings.Contains(entitlement.Message, "compute deployment fingerprint") {
		t.Fatalf("expected fingerprint compute message, got %q", entitlement.Message)
	}
}

func newLicenseRuntimeClient(t *testing.T, objects ...runtime.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
}
