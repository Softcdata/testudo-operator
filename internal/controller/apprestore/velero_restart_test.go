package apprestore

import (
	"context"
	"testing"
	"time"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newVeleroRestartTestClient(t *testing.T, objs ...ctrlclient.Object) ctrlclient.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add appsv1 scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func TestRestartVeleroDeployment_UpdatesRestartAnnotation(t *testing.T) {
	origCooldown := veleroRestartCooldown
	veleroRestartCooldown = 2 * time.Minute
	t.Cleanup(func() { veleroRestartCooldown = origCooldown })

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroDeploymentDefaultName,
			Namespace: controller.VeleroNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "velero"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "velero"},
				},
			},
		},
	}
	cli := newVeleroRestartTestClient(t, deploy)

	restarted, err := restartVeleroDeployment(context.Background(), cli)
	if err != nil {
		t.Fatalf("restartVeleroDeployment returned error: %v", err)
	}
	if !restarted {
		t.Fatalf("expected restarted=true")
	}

	updated := &appsv1.Deployment{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: veleroDeploymentDefaultName, Namespace: controller.VeleroNamespace}, updated); err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}
	got := updated.Spec.Template.Annotations[veleroRestartAtAnnotationKey]
	if got == "" {
		t.Fatalf("expected %s annotation to be set", veleroRestartAtAnnotationKey)
	}
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Fatalf("expected restart annotation RFC3339 timestamp, got %q: %v", got, err)
	}
}

func TestRestartVeleroDeployment_CooldownSkips(t *testing.T) {
	origCooldown := veleroRestartCooldown
	veleroRestartCooldown = 10 * time.Minute
	t.Cleanup(func() { veleroRestartCooldown = origCooldown })

	restartedAt := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      veleroDeploymentDefaultName,
			Namespace: controller.VeleroNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "velero"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "velero"},
					Annotations: map[string]string{veleroRestartAtAnnotationKey: restartedAt},
				},
			},
		},
	}
	cli := newVeleroRestartTestClient(t, deploy)

	restarted, err := restartVeleroDeployment(context.Background(), cli)
	if err != nil {
		t.Fatalf("restartVeleroDeployment returned error: %v", err)
	}
	if restarted {
		t.Fatalf("expected restarted=false during cooldown")
	}

	updated := &appsv1.Deployment{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: veleroDeploymentDefaultName, Namespace: controller.VeleroNamespace}, updated); err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}
	if got := updated.Spec.Template.Annotations[veleroRestartAtAnnotationKey]; got != restartedAt {
		t.Fatalf("expected restart annotation unchanged, got=%q want=%q", got, restartedAt)
	}
}
