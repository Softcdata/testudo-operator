package disasteroperation

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

var _ = Describe("ResourceOnly Drill operation", func() {
	var (
		ctx context.Context
		s   *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		s = runtime.NewScheme()
		Expect(scheme.AddToScheme(s)).To(Succeed())
		Expect(disasterv1.AddToScheme(s)).To(Succeed())
	})

	newOperationFixture := func(mode disasterv1.RestoreMode) (*DisasterOperationReconciler, client.Client, *disasterv1.DisasterOperation) {
		instance := &disasterv1.DisasterInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
			Spec:       disasterv1.DisasterInstanceSpec{Config: "dr-config"},
			Status: disasterv1.DisasterInstanceStatus{
				PrimaryCluster: "cluster-a", SecondaryCluster: "cluster-b",
			},
		}
		config := &disasterv1.DisasterConfig{ObjectMeta: metav1.ObjectMeta{Name: "dr-config"}}
		op := &disasterv1.DisasterOperation{
			ObjectMeta: metav1.ObjectMeta{Name: "drill-op", Namespace: "default"},
			Spec: disasterv1.DisasterOperationSpec{
				InstanceName: "app", OperationType: disasterv1.OperationTypeDrill,
				DrillConfig: &disasterv1.DrillConfig{TargetCluster: "cluster-b", RestoreMode: mode},
			},
			Status: disasterv1.DisasterOperationStatus{State: disasterv1.OperationStateRunning},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(instance, config, op).
			WithStatusSubresource(op).
			Build()
		return &DisasterOperationReconciler{
			Client: fakeClient, Scheme: s, Log: ctrl.Log.WithName("test"), Recorder: record.NewFakeRecorder(100),
		}, fakeClient, op
	}

	It("initializes only RestoreResource and ScaleUp for ResourceOnly", func() {
		r, fakeClient, op := newOperationFixture(disasterv1.RestoreModeResourceOnly)

		_, err := r.handleDrill(ctx, ctrl.Log.WithName("test"), op)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterOperation{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(op), updated)).To(Succeed())
		Expect(updated.Status.Steps).To(HaveLen(2))
		Expect(updated.Status.Steps[0].Name).To(Equal(string(disasterv1.DrillOperationStepRestoreResource)))
		Expect(updated.Status.Steps[1].Name).To(Equal(string(disasterv1.DrillOperationStepScaleUp)))
		Expect(updated.Status.CurrentStep).To(Equal(string(disasterv1.DrillOperationStepRestoreResource)))

		restores := &disasterv1.AppRestoreList{}
		Expect(fakeClient.List(ctx, restores, client.InNamespace("default"))).To(Succeed())
		Expect(restores.Items).To(BeEmpty())
	})

	It("keeps the three-step flow for FullRestore and legacy empty mode", func() {
		for _, mode := range []disasterv1.RestoreMode{disasterv1.RestoreModeFullRestore, ""} {
			r, fakeClient, op := newOperationFixture(mode)

			_, err := r.handleDrill(ctx, ctrl.Log.WithName("test"), op)
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(op), updated)).To(Succeed())
			Expect(updated.Status.Steps).To(HaveLen(3))
			Expect(updated.Status.Steps[1].Name).To(Equal(string(disasterv1.DrillOperationStepRestoreData)))
		}
	})

	It("deep-copies group DrillConfig and assigns each child its own mode", func() {
		parent := &disasterv1.DrillConfig{
			NamespaceMapping: map[string]string{"app": "drill-app"},
			InstanceRestoreModes: map[string]disasterv1.RestoreMode{
				"full":          disasterv1.RestoreModeFullRestore,
				"resource-only": disasterv1.RestoreModeResourceOnly,
			},
		}

		fullConfig, err := drillConfigForGroupChild(parent, disasterv1.OperationTypeDrill, "full")
		Expect(err).NotTo(HaveOccurred())
		resourceOnlyConfig, err := drillConfigForGroupChild(parent, disasterv1.OperationTypeDrill, "resource-only")
		Expect(err).NotTo(HaveOccurred())

		Expect(fullConfig).NotTo(BeIdenticalTo(parent))
		Expect(resourceOnlyConfig).NotTo(BeIdenticalTo(parent))
		Expect(fullConfig).NotTo(BeIdenticalTo(resourceOnlyConfig))
		Expect(fullConfig.RestoreMode).To(Equal(disasterv1.RestoreModeFullRestore))
		Expect(resourceOnlyConfig.RestoreMode).To(Equal(disasterv1.RestoreModeResourceOnly))
		Expect(fullConfig.InstanceRestoreModes).To(BeNil())
		Expect(resourceOnlyConfig.InstanceRestoreModes).To(BeNil())

		fullConfig.NamespaceMapping["app"] = "changed"
		Expect(resourceOnlyConfig.NamespaceMapping["app"]).To(Equal("drill-app"))
		Expect(parent.NamespaceMapping["app"]).To(Equal("drill-app"))
	})

	It("fails closed when a new group mode map omits a child", func() {
		parent := &disasterv1.DrillConfig{
			InstanceRestoreModes: map[string]disasterv1.RestoreMode{
				"other": disasterv1.RestoreModeResourceOnly,
			},
		}

		_, err := drillConfigForGroupChild(parent, disasterv1.OperationTypeDrill, "missing")
		Expect(err).To(MatchError(ContainSubstring("缺少实例 missing")))
	})
})
