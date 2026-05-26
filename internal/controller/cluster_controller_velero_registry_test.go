package controller

import (
	"context"
	"os"
	"strings"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

type captureValuesCommandExecutor struct {
	CalledWith     [][]string
	RenderedValues string
}

func parseRenderedValues(t *testing.T, content string) map[string]interface{} {
	t.Helper()

	values := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(content), &values); err != nil {
		t.Fatalf("unmarshal rendered values: %v", err)
	}
	return values
}

func (c *captureValuesCommandExecutor) Run(name string, args ...string) error {
	c.CalledWith = append(c.CalledWith, append([]string{name}, args...))
	for i := 0; i < len(args)-1; i++ {
		if args[i] != "-f" {
			continue
		}
		content, err := os.ReadFile(args[i+1])
		if err != nil {
			return err
		}
		c.RenderedValues = string(content)
	}
	return nil
}

func buildClusterVeleroRegistryTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	must := func(err error) {
		if err != nil {
			t.Fatalf("add scheme: %v", err)
		}
	}
	must(corev1.AddToScheme(scheme))
	must(appsv1.AddToScheme(scheme))
	must(rbacv1.AddToScheme(scheme))
	must(apiextensionsv1.AddToScheme(scheme))
	must(velerov1.AddToScheme(scheme))
	must(disasterv1.AddToScheme(scheme))
	return scheme
}

func TestInstallVeleroInCluster_WithRegistryCredentialSyncsTargetSecretAndUsesOverlay(t *testing.T) {
	ctx := context.Background()
	scheme := buildClusterVeleroRegistryTestScheme(t)

	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-a",
			Annotations: map[string]string{
				AnnotationTraceID: "trace-a",
			},
		},
		Spec: disasterv1.ClusterSpec{
			Token:    "token-a",
			Endpoint: "https://127.0.0.1:6443",
			VeleroInstall: &disasterv1.VeleroInstallSpec{
				ImageRegistry: "harbor.customer.local/disaster",
				RegistryCredentialSecretRef: &corev1.LocalObjectReference{
					Name: "cluster-velero-regcred-cluster-a",
				},
			},
		},
	}
	managementSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-velero-regcred-cluster-a",
			Namespace: "disaster-system",
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(`{"auths":{"harbor.customer.local":{"auth":"dGVzdA=="}}}`),
		},
	}

	localClient := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster.DeepCopy(), managementSecret).
		Build()
	targetClient := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		Build()
	executor := &captureValuesCommandExecutor{}

	reconciler := &ClusterReconciler{
		Client:          localClient,
		Scheme:          scheme,
		CommandExecutor: executor,
		ClientFactory: func(config *rest.Config, options ctrlclient.Options) (ctrlclient.Client, error) {
			return targetClient, nil
		},
	}

	if err := reconciler.InstallVeleroInCluster(ctx, cluster); err != nil {
		t.Fatalf("InstallVeleroInCluster() error = %v", err)
	}

	targetSecret := &corev1.Secret{}
	if err := targetClient.Get(ctx, types.NamespacedName{Name: "velero-regcred-cluster-a", Namespace: VeleroNamespace}, targetSecret); err != nil {
		t.Fatalf("expected target secret to be created: %v", err)
	}
	if targetSecret.Type != corev1.SecretTypeDockerConfigJson {
		t.Fatalf("unexpected target secret type: %s", targetSecret.Type)
	}
	if got := string(targetSecret.Data[corev1.DockerConfigJsonKey]); !strings.Contains(got, "harbor.customer.local") {
		t.Fatalf("expected docker config json to be synced, got %q", got)
	}

	if len(executor.CalledWith) == 0 {
		t.Fatalf("expected helm command to be executed")
	}
	if !strings.Contains(executor.RenderedValues, "harbor.customer.local/disaster/velero") {
		t.Fatalf("expected velero repository override in values, got:\n%s", executor.RenderedValues)
	}
	if !strings.Contains(executor.RenderedValues, "harbor.customer.local/disaster/kubectl") {
		t.Fatalf("expected kubectl repository override in values, got:\n%s", executor.RenderedValues)
	}
	if !strings.Contains(executor.RenderedValues, "velero-regcred-cluster-a") {
		t.Fatalf("expected imagePullSecrets override in values, got:\n%s", executor.RenderedValues)
	}
	if !strings.Contains(executor.RenderedValues, "harbor.customer.local/disaster/velero-plugin-for-aws") {
		t.Fatalf("expected init container image override in values, got:\n%s", executor.RenderedValues)
	}

	renderedValues := parseRenderedValues(t, executor.RenderedValues)
	imageValues, ok := renderedValues["image"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected image values map, got %#v", renderedValues["image"])
	}
	imagePullSecrets, ok := imageValues["imagePullSecrets"].([]interface{})
	if !ok {
		t.Fatalf("expected imagePullSecrets list, got %#v", imageValues["imagePullSecrets"])
	}
	if len(imagePullSecrets) != 1 || imagePullSecrets[0] != "velero-regcred-cluster-a" {
		t.Fatalf("expected imagePullSecrets to contain target secret name, got %#v", imagePullSecrets)
	}
}

func TestSyncVeleroRegistrySecretToTargetCluster_RemovesTargetSecretWhenCredentialRefMissing(t *testing.T) {
	ctx := context.Background()
	scheme := buildClusterVeleroRegistryTestScheme(t)

	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"},
		Spec: disasterv1.ClusterSpec{
			VeleroInstall: &disasterv1.VeleroInstallSpec{
				ImageRegistry: "harbor.customer.local/disaster",
			},
		},
	}

	localClient := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster.DeepCopy()).
		Build()
	targetClient := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: VeleroNamespace}},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "velero-regcred-cluster-b",
					Namespace: VeleroNamespace,
				},
			},
		).
		Build()

	reconciler := &ClusterReconciler{
		Client: localClient,
		Scheme: scheme,
	}

	if err := reconciler.syncVeleroRegistrySecretToTargetCluster(ctx, cluster, targetClient); err != nil {
		t.Fatalf("syncVeleroRegistrySecretToTargetCluster() error = %v", err)
	}

	if err := targetClient.Get(ctx, types.NamespacedName{Name: "velero-regcred-cluster-b", Namespace: VeleroNamespace}, &corev1.Secret{}); err == nil {
		t.Fatalf("expected target secret to be deleted")
	}
}

func TestCleanupVeleroRegistrySecretOnDelete_RemovesTargetSecret(t *testing.T) {
	ctx := context.Background()
	scheme := buildClusterVeleroRegistryTestScheme(t)

	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-c"},
		Spec: disasterv1.ClusterSpec{
			Token:    "token-c",
			Endpoint: "https://127.0.0.1:6443",
		},
	}
	targetClient := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: VeleroNamespace}},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "velero-regcred-cluster-c",
					Namespace: VeleroNamespace,
				},
			},
		).
		Build()

	reconciler := &ClusterReconciler{
		Scheme: scheme,
		ClientFactory: func(config *rest.Config, options ctrlclient.Options) (ctrlclient.Client, error) {
			return targetClient, nil
		},
	}

	if err := reconciler.cleanupVeleroRegistrySecretOnDelete(ctx, cluster); err != nil {
		t.Fatalf("cleanupVeleroRegistrySecretOnDelete() error = %v", err)
	}

	if err := targetClient.Get(ctx, types.NamespacedName{Name: "velero-regcred-cluster-c", Namespace: VeleroNamespace}, &corev1.Secret{}); err == nil {
		t.Fatalf("expected target secret to be deleted")
	}
}
