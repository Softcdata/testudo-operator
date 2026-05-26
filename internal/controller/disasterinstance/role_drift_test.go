package disasterinstance

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

var _ = Describe("DisasterInstance role drift detection", func() {
	var (
		ctx      context.Context
		s        *runtime.Scheme
		recorder *record.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()
		s = runtime.NewScheme()
		Expect(scheme.AddToScheme(s)).To(Succeed())
		Expect(disasterv1.AddToScheme(s)).To(Succeed())
		recorder = record.NewFakeRecorder(100)
	})

	newReadyInstanceObjects := func(state, reason string) (*disasterv1.DisasterConfig, *disasterv1.DisasterInstance, *disasterv1.DataSync, *disasterv1.ResourceSync) {
		now := metav1.Now()
		config := &disasterv1.DisasterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg"},
			Spec: disasterv1.DisasterConfigSpec{
				SourceCluster:     "cluster-a",
				TargetCluster:     "cluster-b",
				StorageRepository: "repo",
			},
		}
		instance := &disasterv1.DisasterInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "inst", Namespace: "default"},
			Spec: disasterv1.DisasterInstanceSpec{
				Config:     "cfg",
				Namespaces: []string{"app"},
			},
			Status: disasterv1.DisasterInstanceStatus{
				FsmState:         state,
				Reason:           reason,
				PrimaryCluster:   "cluster-a",
				SecondaryCluster: "cluster-b",
				DataSyncName:     "dr-ds-inst",
				ResourceSyncName: "dr-rs-inst",
			},
		}
		dataSync := &disasterv1.DataSync{
			ObjectMeta: metav1.ObjectMeta{Name: "dr-ds-inst", Namespace: "default"},
			Status: disasterv1.DataSyncStatus{
				State:        disasterv1.DataSyncStateReady,
				LastSyncTime: &now,
			},
		}
		resourceSync := &disasterv1.ResourceSync{
			ObjectMeta: metav1.ObjectMeta{Name: "dr-rs-inst", Namespace: "default"},
			Status: disasterv1.ResourceSyncStatus{
				State:        disasterv1.ResourceSyncStateReady,
				LastSyncTime: &now,
			},
		}
		return config, instance, dataSync, resourceSync
	}

	newDeployment := func(name, namespace string, replicas int32) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
			},
		}
	}

	newRemoteClient := func(objects ...client.Object) client.Client {
		return fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(objects...).
			Build()
	}

	newReconciler := func(local client.Client, remotes map[string]client.Client) *DisasterInstanceReconciler {
		return &DisasterInstanceReconciler{
			Client:   local,
			Scheme:   s,
			Log:      ctrl.Log.WithName("role-drift-test"),
			Recorder: recorder,
			KubeClientGetter: func(ctx context.Context, cli client.Client, scheme *runtime.Scheme, clusterName string) (client.Client, error) {
				return remotes[clusterName], nil
			},
		}
	}

	It("fails the instance when expected primary is scaled to zero and expected secondary is active", func() {
		config, instance, dataSync, resourceSync := newReadyInstanceObjects(disasterv1.FsmStateProtected, "")
		local := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(config, instance, dataSync, resourceSync).
			WithStatusSubresource(instance, dataSync, resourceSync).
			Build()
		r := newReconciler(local, map[string]client.Client{
			"cluster-a": newRemoteClient(newDeployment("web", "app", 0)),
			"cluster-b": newRemoteClient(newDeployment("web", "app", 2)),
		})

		_, err := r.handleProtected(ctx, r.Log, instance)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterInstance{}
		Expect(local.Get(ctx, types.NamespacedName{Name: "inst", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
		Expect(updated.Status.Reason).To(Equal(instanceReasonRoleDriftDetected))
		cond := apimeta.FindStatusCondition(updated.Status.Conditions, instanceConditionRoleDrift)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(roleDriftReasonRoleReversed))
	})

	It("does not fail the instance when both sides are active", func() {
		config, instance, dataSync, resourceSync := newReadyInstanceObjects(disasterv1.FsmStateProtected, "")
		local := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(config, instance, dataSync, resourceSync).
			WithStatusSubresource(instance, dataSync, resourceSync).
			Build()
		r := newReconciler(local, map[string]client.Client{
			"cluster-a": newRemoteClient(newDeployment("web", "app", 2)),
			"cluster-b": newRemoteClient(newDeployment("web", "app", 1)),
		})

		_, err := r.handleProtected(ctx, r.Log, instance)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterInstance{}
		Expect(local.Get(ctx, types.NamespacedName{Name: "inst", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
		Expect(updated.Status.Reason).To(BeEmpty())
		cond := apimeta.FindStatusCondition(updated.Status.Conditions, instanceConditionRoleDrift)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(roleDriftReasonBothActiveObserved))
	})

	It("fails the instance when both sides are standby", func() {
		config, instance, dataSync, resourceSync := newReadyInstanceObjects(disasterv1.FsmStateProtected, "")
		local := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(config, instance, dataSync, resourceSync).
			WithStatusSubresource(instance, dataSync, resourceSync).
			Build()
		r := newReconciler(local, map[string]client.Client{
			"cluster-a": newRemoteClient(newDeployment("web", "app", 0)),
			"cluster-b": newRemoteClient(newDeployment("web", "app", 0)),
		})

		_, err := r.handleProtected(ctx, r.Log, instance)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterInstance{}
		Expect(local.Get(ctx, types.NamespacedName{Name: "inst", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
		Expect(updated.Status.Reason).To(Equal(instanceReasonRoleDriftDetected))
		cond := apimeta.FindStatusCondition(updated.Status.Conditions, instanceConditionRoleDrift)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(roleDriftReasonBothStandby))
	})

	It("recovers a RoleDriftDetected failure when replica roles match expected state again", func() {
		config, instance, dataSync, resourceSync := newReadyInstanceObjects(disasterv1.FsmStateFailed, instanceReasonRoleDriftDetected)
		local := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(config, instance, dataSync, resourceSync).
			WithStatusSubresource(instance, dataSync, resourceSync).
			Build()
		r := newReconciler(local, map[string]client.Client{
			"cluster-a": newRemoteClient(newDeployment("web", "app", 2)),
			"cluster-b": newRemoteClient(newDeployment("web", "app", 0)),
		})

		_, err := r.handleFailed(ctx, r.Log, instance)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterInstance{}
		Expect(local.Get(ctx, types.NamespacedName{Name: "inst", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
		Expect(updated.Status.Reason).To(BeEmpty())
		cond := apimeta.FindStatusCondition(updated.Status.Conditions, instanceConditionRoleDrift)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(roleDriftReasonExpectedRoleMatched))
	})
})
