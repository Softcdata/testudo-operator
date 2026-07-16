package disasterdrill

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

func readyResourceSync(instanceName string) *disasterv1.ResourceSync {
	return &disasterv1.ResourceSync{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-rs-" + instanceName, Namespace: "default"},
		Spec:       disasterv1.ResourceSyncSpec{Instance: instanceName},
		Status: disasterv1.ResourceSyncStatus{
			State:          disasterv1.ResourceSyncStateReady,
			LastBackupName: "resource-backup",
		},
	}
}

func fullRestoreDataSync(instanceName string) *disasterv1.DataSync {
	return &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-ds-" + instanceName, Namespace: "default"},
		Spec:       disasterv1.DataSyncSpec{Instance: instanceName},
		Status: disasterv1.DataSyncStatus{
			State:          disasterv1.DataSyncStateReady,
			LastBackupName: "data-backup",
		},
	}
}

func resourceOnlyDataSync(instanceName string) *disasterv1.DataSync {
	ds := fullRestoreDataSync(instanceName)
	ds.Status.Conditions = []metav1.Condition{{
		Type:   drillNoDataVolumesCondition,
		Status: metav1.ConditionTrue,
		Reason: drillNoPVCFoundReason,
	}}
	ds.Status.History = []disasterv1.SyncHistoryRecord{{Status: drillSkippedHistoryStatus}}
	return ds
}

func drillTestInstance(name string) *disasterv1.DisasterInstance {
	return &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status: disasterv1.DisasterInstanceStatus{
			FsmState:         disasterv1.FsmStateProtected,
			PrimaryCluster:   "cluster-a",
			SecondaryCluster: "cluster-b",
			DataSyncName:     "dr-ds-" + name,
			ResourceSyncName: "dr-rs-" + name,
		},
	}
}

var _ = Describe("Drill backup restore mode classification", func() {
	It("classifies an explicit no-data status as ResourceOnly even with a stale backup name", func() {
		mode, err := classifyDrillRestoreMode(resourceOnlyDataSync("app"), readyResourceSync("app"))
		Expect(err).NotTo(HaveOccurred())
		Expect(mode).To(Equal(disasterv1.RestoreModeResourceOnly))
	})

	It("rejects an explicit no-data condition when the latest history is not Skipped", func() {
		ds := resourceOnlyDataSync("app")
		ds.Status.History = append(ds.Status.History, disasterv1.SyncHistoryRecord{Status: "Completed"})

		_, err := classifyDrillRestoreMode(ds, readyResourceSync("app"))
		Expect(err).To(MatchError(ContainSubstring("不是 Skipped")))
	})

	It("does not classify an incomplete no-data status as ResourceOnly", func() {
		ds := fullRestoreDataSync("app")
		ds.Status.LastBackupName = ""
		ds.Status.Conditions = []metav1.Condition{{
			Type: drillNoDataVolumesCondition, Status: metav1.ConditionTrue, Reason: "WrongReason",
		}}
		ds.Status.History = []disasterv1.SyncHistoryRecord{{Status: drillSkippedHistoryStatus}}

		_, err := classifyDrillRestoreMode(ds, readyResourceSync("app"))
		Expect(err).To(MatchError(ContainSubstring("没有可用的数据备份")))
	})

	It("requires both a true condition and the NoPVCFound reason", func() {
		for _, condition := range []metav1.Condition{
			{},
			{Type: drillNoDataVolumesCondition, Status: metav1.ConditionFalse, Reason: drillNoPVCFoundReason},
			{Type: drillNoDataVolumesCondition, Status: metav1.ConditionTrue, Reason: "OtherReason"},
		} {
			ds := fullRestoreDataSync("app")
			ds.Status.LastBackupName = ""
			if condition.Type != "" {
				ds.Status.Conditions = []metav1.Condition{condition}
			}
			ds.Status.History = []disasterv1.SyncHistoryRecord{{Status: drillSkippedHistoryStatus}}

			_, err := classifyDrillRestoreMode(ds, readyResourceSync("app"))
			Expect(err).To(HaveOccurred())
		}
	})

	It("requires a Ready ResourceSync backup for every restore mode", func() {
		rs := readyResourceSync("app")
		rs.Status.LastBackupName = ""

		_, err := classifyDrillRestoreMode(resourceOnlyDataSync("app"), rs)
		Expect(err).To(MatchError(ContainSubstring("没有可用的资源备份")))
	})

	It("keeps the regular backup path as FullRestore", func() {
		mode, err := classifyDrillRestoreMode(fullRestoreDataSync("app"), readyResourceSync("app"))
		Expect(err).NotTo(HaveOccurred())
		Expect(mode).To(Equal(disasterv1.RestoreModeFullRestore))
	})

	It("rejects a DataSync that is not Ready even when it retains a backup name", func() {
		ds := fullRestoreDataSync("app")
		ds.Status.State = disasterv1.DataSyncStateFailed

		_, err := classifyDrillRestoreMode(ds, readyResourceSync("app"))
		Expect(err).To(MatchError(ContainSubstring("DataSync 状态不是 Ready")))
	})
})

var _ = Describe("DisasterDrill backup preflight", func() {
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

	newReconciler := func(objects ...client.Object) (*DisasterDrillReconciler, client.Client) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(objects...).
			WithStatusSubresource(&disasterv1.DisasterDrill{}).
			Build()
		return &DisasterDrillReconciler{
			Client: fakeClient, Scheme: s, Log: ctrl.Log.WithName("test"), Recorder: recorder,
		}, fakeClient
	}

	pendingDrill := func(name, instanceName string) *disasterv1.DisasterDrill {
		return &disasterv1.DisasterDrill{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       disasterv1.DisasterDrillSpec{InstanceName: instanceName},
			Status:     disasterv1.DisasterDrillStatus{State: disasterv1.DrillStatePending},
		}
	}

	readyCluster := func() *disasterv1.Cluster {
		return &disasterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"},
			Status:     disasterv1.ClusterStatus{Status: "Ready"},
		}
	}

	It("enters Ready in ResourceOnly mode for an explicit no-PVC DataSync", func() {
		instance := drillTestInstance("app")
		drill := pendingDrill("resource-only", instance.Name)
		r, fakeClient := newReconciler(instance, resourceOnlyDataSync("app"), readyResourceSync("app"), readyCluster(), drill)

		_, err := r.handlePending(ctx, ctrl.Log.WithName("test"), drill)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterDrill{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: drill.Name, Namespace: drill.Namespace}, updated)).To(Succeed())
		Expect(updated.Status.State).To(Equal(disasterv1.DrillStateReady))
		Expect(updated.Status.RestoreMode).To(Equal(disasterv1.RestoreModeResourceOnly))
		Expect(updated.Status.InstanceRestoreModes).To(Equal(map[string]disasterv1.RestoreMode{
			instance.Name: disasterv1.RestoreModeResourceOnly,
		}))
		Expect(updated.Status.ValidationResults.BackupAvailable).To(BeTrue())
	})

	It("fails before Ready when the ordinary DataSync backup is missing even with skipValidation", func() {
		instance := drillTestInstance("app")
		ds := fullRestoreDataSync("app")
		ds.Status.LastBackupName = ""
		drill := pendingDrill("missing-data-backup", instance.Name)
		drill.Spec.SkipValidation = true
		r, fakeClient := newReconciler(instance, ds, readyResourceSync("app"), readyCluster(), drill)

		_, err := r.handlePending(ctx, ctrl.Log.WithName("test"), drill)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterDrill{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: drill.Name, Namespace: drill.Namespace}, updated)).To(Succeed())
		Expect(updated.Status.State).To(Equal(disasterv1.DrillStateFailed))
		Expect(updated.Status.ValidationResults.BackupAvailable).To(BeFalse())
		Expect(updated.Status.Message).To(ContainSubstring("没有可用的数据备份"))
		operations := &disasterv1.DisasterOperationList{}
		Expect(fakeClient.List(ctx, operations, client.InNamespace("default"))).To(Succeed())
		Expect(operations.Items).To(BeEmpty())
	})

	It("fails before Ready when the ResourceSync backup is missing", func() {
		instance := drillTestInstance("app")
		rs := readyResourceSync("app")
		rs.Status.LastBackupName = ""
		drill := pendingDrill("missing-resource-backup", instance.Name)
		r, fakeClient := newReconciler(instance, resourceOnlyDataSync("app"), rs, readyCluster(), drill)

		_, err := r.handlePending(ctx, ctrl.Log.WithName("test"), drill)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterDrill{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: drill.Name, Namespace: drill.Namespace}, updated)).To(Succeed())
		Expect(updated.Status.State).To(Equal(disasterv1.DrillStateFailed))
		Expect(updated.Status.Reason).To(Equal(drillReasonBackupUnavailable))
		Expect(updated.Status.ValidationResults.BackupAvailable).To(BeFalse())
		Expect(updated.Status.Message).To(ContainSubstring("没有可用的资源备份"))
	})

	It("aggregates mixed group members while preserving per-instance modes", func() {
		full := drillTestInstance("full")
		resourceOnly := drillTestInstance("resource-only")
		group := &disasterv1.DisasterGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "mixed-group", Namespace: "default"},
			Spec:       disasterv1.DisasterGroupSpec{Levels: [][]string{{full.Name, resourceOnly.Name}}},
		}
		drill := &disasterv1.DisasterDrill{
			ObjectMeta: metav1.ObjectMeta{Name: "mixed", Namespace: "default"},
			Spec: disasterv1.DisasterDrillSpec{
				GroupName: "mixed-group", TargetCluster: "cluster-b",
			},
			Status: disasterv1.DisasterDrillStatus{State: disasterv1.DrillStatePending},
		}
		r, fakeClient := newReconciler(
			full, resourceOnly, group, drill, readyCluster(),
			fullRestoreDataSync(full.Name), readyResourceSync(full.Name),
			resourceOnlyDataSync(resourceOnly.Name), readyResourceSync(resourceOnly.Name),
		)

		_, err := r.handlePending(ctx, ctrl.Log.WithName("test"), drill)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterDrill{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: drill.Name, Namespace: drill.Namespace}, updated)).To(Succeed())
		Expect(updated.Status.State).To(Equal(disasterv1.DrillStateReady))
		Expect(updated.Status.RestoreMode).To(Equal(disasterv1.RestoreModeMixed))
		Expect(updated.Status.InstanceRestoreModes).To(Equal(map[string]disasterv1.RestoreMode{
			full.Name:         disasterv1.RestoreModeFullRestore,
			resourceOnly.Name: disasterv1.RestoreModeResourceOnly,
		}))
	})

	It("fails the whole group preflight when one member has no valid data mode", func() {
		valid := drillTestInstance("valid")
		invalid := drillTestInstance("invalid")
		invalidDataSync := fullRestoreDataSync(invalid.Name)
		invalidDataSync.Status.LastBackupName = ""
		group := &disasterv1.DisasterGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-group", Namespace: "default"},
			Spec:       disasterv1.DisasterGroupSpec{Levels: [][]string{{valid.Name, invalid.Name}}},
		}
		drill := &disasterv1.DisasterDrill{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-group-drill", Namespace: "default"},
			Spec: disasterv1.DisasterDrillSpec{
				GroupName: "invalid-group", TargetCluster: "cluster-b",
			},
			Status: disasterv1.DisasterDrillStatus{State: disasterv1.DrillStatePending},
		}
		r, fakeClient := newReconciler(
			valid, invalid, group, drill, readyCluster(),
			fullRestoreDataSync(valid.Name), readyResourceSync(valid.Name),
			invalidDataSync, readyResourceSync(invalid.Name),
		)

		_, err := r.handlePending(ctx, ctrl.Log.WithName("test"), drill)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterDrill{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: drill.Name, Namespace: drill.Namespace}, updated)).To(Succeed())
		Expect(updated.Status.State).To(Equal(disasterv1.DrillStateFailed))
		Expect(updated.Status.ValidationResults.BackupAvailable).To(BeFalse())
		operations := &disasterv1.DisasterOperationList{}
		Expect(fakeClient.List(ctx, operations, client.InNamespace("default"))).To(Succeed())
		Expect(operations.Items).To(BeEmpty())
	})

	It("passes a frozen ResourceOnly mode to the confirmed Operation", func() {
		instance := drillTestInstance("app")
		drill := &disasterv1.DisasterDrill{
			ObjectMeta: metav1.ObjectMeta{Name: "confirmed", Namespace: "default"},
			Spec: disasterv1.DisasterDrillSpec{
				InstanceName: instance.Name, Confirmed: true,
			},
			Status: disasterv1.DisasterDrillStatus{
				State:                disasterv1.DrillStateReady,
				TargetCluster:        "cluster-b",
				RestoreMode:          disasterv1.RestoreModeResourceOnly,
				InstanceRestoreModes: map[string]disasterv1.RestoreMode{instance.Name: disasterv1.RestoreModeResourceOnly},
			},
		}
		r, fakeClient := newReconciler(instance, resourceOnlyDataSync("app"), readyResourceSync("app"), drill)

		_, err := r.handleReady(ctx, ctrl.Log.WithName("test"), drill)
		Expect(err).NotTo(HaveOccurred())

		operations := &disasterv1.DisasterOperationList{}
		Expect(fakeClient.List(ctx, operations, client.InNamespace("default"))).To(Succeed())
		Expect(operations.Items).To(HaveLen(1))
		Expect(operations.Items[0].Spec.DrillConfig.RestoreMode).To(Equal(disasterv1.RestoreModeResourceOnly))
	})

	It("backfills a mode snapshot before executing a legacy Ready Drill", func() {
		instance := drillTestInstance("app")
		drill := &disasterv1.DisasterDrill{
			ObjectMeta: metav1.ObjectMeta{Name: "legacy-ready", Namespace: "default"},
			Spec: disasterv1.DisasterDrillSpec{
				InstanceName: instance.Name, Confirmed: true,
			},
			Status: disasterv1.DisasterDrillStatus{
				State:         disasterv1.DrillStateReady,
				TargetCluster: "cluster-b",
				RestoreMode:   disasterv1.RestoreModeFullRestore,
			},
		}
		r, fakeClient := newReconciler(instance, resourceOnlyDataSync("app"), readyResourceSync("app"), drill)

		result, err := r.handleReady(ctx, ctrl.Log.WithName("test"), drill)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeTrue())
		operations := &disasterv1.DisasterOperationList{}
		Expect(fakeClient.List(ctx, operations, client.InNamespace("default"))).To(Succeed())
		Expect(operations.Items).To(BeEmpty())

		updated := &disasterv1.DisasterDrill{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: drill.Name, Namespace: drill.Namespace}, updated)).To(Succeed())
		Expect(updated.Status.RestoreMode).To(Equal(disasterv1.RestoreModeResourceOnly))
		Expect(updated.Status.InstanceRestoreModes).To(Equal(map[string]disasterv1.RestoreMode{
			instance.Name: disasterv1.RestoreModeResourceOnly,
		}))

		_, err = r.handleReady(ctx, ctrl.Log.WithName("test"), updated)
		Expect(err).NotTo(HaveOccurred())
		Expect(fakeClient.List(ctx, operations, client.InNamespace("default"))).To(Succeed())
		Expect(operations.Items).To(HaveLen(1))
		Expect(operations.Items[0].Spec.DrillConfig.RestoreMode).To(Equal(disasterv1.RestoreModeResourceOnly))
	})

	It("rejects confirmation after the frozen restore mode changes", func() {
		instance := drillTestInstance("app")
		drill := &disasterv1.DisasterDrill{
			ObjectMeta: metav1.ObjectMeta{Name: "drift", Namespace: "default"},
			Spec: disasterv1.DisasterDrillSpec{
				InstanceName: instance.Name, Confirmed: true,
			},
			Status: disasterv1.DisasterDrillStatus{
				State:                disasterv1.DrillStateReady,
				TargetCluster:        "cluster-b",
				RestoreMode:          disasterv1.RestoreModeResourceOnly,
				InstanceRestoreModes: map[string]disasterv1.RestoreMode{instance.Name: disasterv1.RestoreModeResourceOnly},
			},
		}
		r, fakeClient := newReconciler(instance, fullRestoreDataSync("app"), readyResourceSync("app"), drill)

		_, err := r.handleReady(ctx, ctrl.Log.WithName("test"), drill)
		Expect(err).NotTo(HaveOccurred())

		updated := &disasterv1.DisasterDrill{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: drill.Name, Namespace: drill.Namespace}, updated)).To(Succeed())
		Expect(updated.Status.State).To(Equal(disasterv1.DrillStateFailed))
		Expect(updated.Status.Reason).To(Equal(drillReasonBackupStateChanged))
		operations := &disasterv1.DisasterOperationList{}
		Expect(fakeClient.List(ctx, operations, client.InNamespace("default"))).To(Succeed())
		Expect(operations.Items).To(BeEmpty())
	})
})
