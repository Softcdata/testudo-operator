package disasterinstance

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileSyncSchedulesUsesFieldLevelInheritance(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name                   string
		configDataPolicy       string
		configResourcePolicy   string
		instanceDataPolicy     string
		instanceResourcePolicy string
		expectedDataSchedule   string
		expectedResSchedule    string
	}{
		{
			name:                 "inherits config policies when instance override is empty",
			configDataPolicy:     "policy-ds-base",
			configResourcePolicy: "policy-rs-base",
			expectedDataSchedule: "*/5 * * * *",
			expectedResSchedule:  "0 * * * *",
		},
		{
			name:                 "overrides data policy only",
			configDataPolicy:     "policy-ds-base",
			configResourcePolicy: "policy-rs-base",
			instanceDataPolicy:   "policy-ds-override",
			expectedDataSchedule: "*/10 * * * *",
			expectedResSchedule:  "0 * * * *",
		},
		{
			name:                   "overrides resource policy only",
			configDataPolicy:       "policy-ds-base",
			configResourcePolicy:   "policy-rs-base",
			instanceResourcePolicy: "policy-rs-override",
			expectedDataSchedule:   "*/5 * * * *",
			expectedResSchedule:    "15 * * * *",
		},
		{
			name:                   "overrides both policies",
			configDataPolicy:       "policy-ds-base",
			configResourcePolicy:   "policy-rs-base",
			instanceDataPolicy:     "policy-ds-override",
			instanceResourcePolicy: "policy-rs-override",
			expectedDataSchedule:   "*/10 * * * *",
			expectedResSchedule:    "15 * * * *",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := newDisasterInstanceTestScheme(t)
			instance := newSyncPolicyTestInstance()
			instance.Spec.DataSyncPolicy = tc.instanceDataPolicy
			instance.Spec.ResourceSyncPolicy = tc.instanceResourcePolicy
			config := newSyncPolicyTestConfig(tc.configDataPolicy, tc.configResourcePolicy)

			objects := []ctrlclient.Object{config, instance}
			objects = appendSyncPolicyTestObjects(objects)
			cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			reconciler := &DisasterInstanceReconciler{Client: cli, Scheme: scheme}

			if _, err := reconciler.reconcileSyncSchedules(ctx, logr.Discard(), instance); err != nil {
				t.Fatalf("reconcileSyncSchedules returned error: %v", err)
			}

			assertDataSyncSchedule(t, ctx, cli, "dr-ds-instance-sync", tc.expectedDataSchedule)
			assertResourceSyncSchedule(t, ctx, cli, "dr-rs-instance-sync", tc.expectedResSchedule)
		})
	}
}

func TestReconcileSyncSchedulesClearsStaleSchedulesWhenEffectivePolicyIsEmpty(t *testing.T) {
	ctx := context.Background()
	scheme := newDisasterInstanceTestScheme(t)
	instance := newSyncPolicyTestInstance()
	config := newSyncPolicyTestConfig("", "")
	dataSync := &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dr-ds-instance-sync",
			Namespace: "default",
		},
		Spec: disasterv1.DataSyncSpec{
			Instance: instance.Name,
			Trigger: disasterv1.TriggerSpec{
				Schedule: "*/3 * * * *",
			},
		},
	}
	resourceSync := &disasterv1.ResourceSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dr-rs-instance-sync",
			Namespace: "default",
		},
		Spec: disasterv1.ResourceSyncSpec{
			Instance: instance.Name,
			Trigger: disasterv1.TriggerSpec{
				Schedule: "*/7 * * * *",
			},
		},
	}

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(config, instance, dataSync, resourceSync).
		Build()
	reconciler := &DisasterInstanceReconciler{Client: cli, Scheme: scheme}

	if _, err := reconciler.reconcileSyncSchedules(ctx, logr.Discard(), instance); err != nil {
		t.Fatalf("reconcileSyncSchedules returned error: %v", err)
	}

	assertDataSyncSchedule(t, ctx, cli, dataSync.Name, "")
	assertResourceSyncSchedule(t, ctx, cli, resourceSync.Name, "")
}

func TestReconcileSyncSchedulesClearsScheduleWhenPolicyDisabled(t *testing.T) {
	ctx := context.Background()
	scheme := newDisasterInstanceTestScheme(t)
	instance := newSyncPolicyTestInstance()
	config := newSyncPolicyTestConfig("policy-ds-disabled", "policy-rs-disabled")
	dataSync := &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dr-ds-instance-sync",
			Namespace: "default",
		},
		Spec: disasterv1.DataSyncSpec{
			Instance: instance.Name,
			Trigger: disasterv1.TriggerSpec{
				Schedule: "*/3 * * * *",
			},
		},
	}
	resourceSync := &disasterv1.ResourceSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dr-rs-instance-sync",
			Namespace: "default",
		},
		Spec: disasterv1.ResourceSyncSpec{
			Instance: instance.Name,
			Trigger: disasterv1.TriggerSpec{
				Schedule: "*/7 * * * *",
			},
		},
	}

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			config,
			instance,
			dataSync,
			resourceSync,
			newSyncPolicyTestPolicy("policy-ds-disabled", disasterv1.PolicyTypeDataSync, "*/5 * * * *", disasterv1.PolicyStateDisabled),
			newSyncPolicyTestPolicy("policy-rs-disabled", disasterv1.PolicyTypeResourceSync, "0 * * * *", disasterv1.PolicyStateDisabled),
		).
		Build()
	reconciler := &DisasterInstanceReconciler{Client: cli, Scheme: scheme}

	if _, err := reconciler.reconcileSyncSchedules(ctx, logr.Discard(), instance); err != nil {
		t.Fatalf("reconcileSyncSchedules returned error: %v", err)
	}

	assertDataSyncSchedule(t, ctx, cli, dataSync.Name, "")
	assertResourceSyncSchedule(t, ctx, cli, resourceSync.Name, "")
}

func TestReconcileSyncSchedulesRefreshesScheduleWhenPolicyCronChanges(t *testing.T) {
	ctx := context.Background()
	scheme := newDisasterInstanceTestScheme(t)
	instance := newSyncPolicyTestInstance()
	config := newSyncPolicyTestConfig("policy-ds-base", "policy-rs-base")
	dataPolicy := newSyncPolicyTestPolicy("policy-ds-base", disasterv1.PolicyTypeDataSync, "*/5 * * * *", disasterv1.PolicyStateEnabled)
	resourcePolicy := newSyncPolicyTestPolicy("policy-rs-base", disasterv1.PolicyTypeResourceSync, "0 * * * *", disasterv1.PolicyStateEnabled)

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(config, instance, dataPolicy, resourcePolicy).
		Build()
	reconciler := &DisasterInstanceReconciler{Client: cli, Scheme: scheme}

	if _, err := reconciler.reconcileSyncSchedules(ctx, logr.Discard(), instance); err != nil {
		t.Fatalf("first reconcileSyncSchedules returned error: %v", err)
	}
	assertDataSyncSchedule(t, ctx, cli, "dr-ds-instance-sync", "*/5 * * * *")
	assertResourceSyncSchedule(t, ctx, cli, "dr-rs-instance-sync", "0 * * * *")

	if err := cli.Get(ctx, types.NamespacedName{Name: dataPolicy.Name, Namespace: dataPolicy.Namespace}, dataPolicy); err != nil {
		t.Fatalf("get data policy: %v", err)
	}
	dataPolicy.Spec.Schedule = "*/11 * * * *"
	if err := cli.Update(ctx, dataPolicy); err != nil {
		t.Fatalf("update data policy: %v", err)
	}

	if err := cli.Get(ctx, types.NamespacedName{Name: resourcePolicy.Name, Namespace: resourcePolicy.Namespace}, resourcePolicy); err != nil {
		t.Fatalf("get resource policy: %v", err)
	}
	resourcePolicy.Spec.Schedule = "30 * * * *"
	if err := cli.Update(ctx, resourcePolicy); err != nil {
		t.Fatalf("update resource policy: %v", err)
	}

	if _, err := reconciler.reconcileSyncSchedules(ctx, logr.Discard(), instance); err != nil {
		t.Fatalf("second reconcileSyncSchedules returned error: %v", err)
	}

	assertDataSyncSchedule(t, ctx, cli, "dr-ds-instance-sync", "*/11 * * * *")
	assertResourceSyncSchedule(t, ctx, cli, "dr-rs-instance-sync", "30 * * * *")
}

func newSyncPolicyTestInstance() *disasterv1.DisasterInstance {
	return &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-sync",
			Namespace: "default",
			UID:       types.UID("instance-sync-uid"),
		},
		Spec: disasterv1.DisasterInstanceSpec{
			Config: "config-sync",
		},
	}
}

func newSyncPolicyTestConfig(dataPolicy, resourcePolicy string) *disasterv1.DisasterConfig {
	return &disasterv1.DisasterConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config-sync",
		},
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:      "cluster-a",
			TargetCluster:      "cluster-b",
			StorageRepository:  "repo-a",
			DataSyncPolicy:     dataPolicy,
			ResourceSyncPolicy: resourcePolicy,
		},
	}
}

func newSyncPolicyTestPolicy(name string, policyType disasterv1.PolicyType, schedule string, state disasterv1.PolicyState) *disasterv1.DisasterPolicy {
	return &disasterv1.DisasterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: disasterv1.DisasterPolicySpec{
			Type:     policyType,
			Schedule: schedule,
			State:    state,
		},
	}
}

func appendSyncPolicyTestObjects(objects []ctrlclient.Object) []ctrlclient.Object {
	return append(
		objects,
		newSyncPolicyTestPolicy("policy-ds-base", disasterv1.PolicyTypeDataSync, "*/5 * * * *", disasterv1.PolicyStateEnabled),
		newSyncPolicyTestPolicy("policy-rs-base", disasterv1.PolicyTypeResourceSync, "0 * * * *", disasterv1.PolicyStateEnabled),
		newSyncPolicyTestPolicy("policy-ds-override", disasterv1.PolicyTypeDataSync, "*/10 * * * *", disasterv1.PolicyStateEnabled),
		newSyncPolicyTestPolicy("policy-rs-override", disasterv1.PolicyTypeResourceSync, "15 * * * *", disasterv1.PolicyStateEnabled),
	)
}

func assertDataSyncSchedule(t *testing.T, ctx context.Context, cli ctrlclient.Client, name, want string) {
	t.Helper()
	dataSync := &disasterv1.DataSync{}
	if err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, dataSync); err != nil {
		t.Fatalf("get datasync %s: %v", name, err)
	}
	if got := dataSync.Spec.Trigger.Schedule; got != want {
		t.Fatalf("unexpected datasync schedule: got %q want %q", got, want)
	}
}

func assertResourceSyncSchedule(t *testing.T, ctx context.Context, cli ctrlclient.Client, name, want string) {
	t.Helper()
	resourceSync := &disasterv1.ResourceSync{}
	if err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, resourceSync); err != nil {
		t.Fatalf("get resourcesync %s: %v", name, err)
	}
	if got := resourceSync.Spec.Trigger.Schedule; got != want {
		t.Fatalf("unexpected resourcesync schedule: got %q want %q", got, want)
	}
}
