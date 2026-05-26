package cluster

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	platformlicense "github.com/softcdata/testudo-operator/pkg/license"
)

func TestClusterValidatorDeniesThirdFreeClusterAndIgnoresStatusConfigMap(t *testing.T) {
	scheme := newWebhookScheme(t)
	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: platformlicense.DefaultLicenseNamespace}},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      platformlicense.StatusConfigMapName,
					Namespace: platformlicense.DefaultLicenseNamespace,
				},
				Data: map[string]string{"state": "Active", "maxClusters": "-1"},
			},
			&disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"}},
			&disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"}},
		).
		Build()
	validator := NewValidator(cli, platformlicense.DefaultLicenseNamespace, "", nil)

	response := validator.Handle(context.Background(), admissionCreateRequest(t, "cluster-3"))
	if response.Allowed {
		t.Fatalf("expected third free cluster to be denied")
	}
	if !strings.Contains(response.Result.Message, platformlicense.ReasonLicenseLimitExceeded) {
		t.Fatalf("expected limit exceeded message, got %q", response.Result.Message)
	}
}

func TestClusterValidatorAllowsSecondFreeCluster(t *testing.T) {
	scheme := newWebhookScheme(t)
	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: platformlicense.DefaultLicenseNamespace}},
			&disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"}},
		).
		Build()
	validator := NewValidator(cli, platformlicense.DefaultLicenseNamespace, "", nil)

	response := validator.Handle(context.Background(), admissionCreateRequest(t, "cluster-2"))
	if !response.Allowed {
		t.Fatalf("expected second free cluster to be allowed: %s", response.Result.Message)
	}
}

func TestClusterValidatorAllowsUpdate(t *testing.T) {
	validator := NewValidator(nil, "", "", nil)
	response := validator.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Update},
	})
	if !response.Allowed {
		t.Fatalf("expected update to be skipped")
	}
}

func newWebhookScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	return scheme
}

func admissionCreateRequest(t *testing.T, name string) admission.Request {
	t.Helper()
	raw, err := json.Marshal(&disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
		},
	})
	if err != nil {
		t.Fatalf("marshal cluster: %v", err)
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}
