package disasterinstance

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctrlpkg "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
)

const (
	instanceConditionRoleDrift = "RoleDrift"

	instanceReasonRoleDriftDetected = "RoleDriftDetected"

	roleDriftReasonExpectedRoleMatched = "ExpectedRoleMatched"
	roleDriftReasonBothActiveObserved  = "BothActiveObserved"
	roleDriftReasonRoleReversed        = "RoleReversed"
	roleDriftReasonBothStandby         = "BothStandby"
	roleDriftReasonCheckFailed         = "CheckFailed"
	roleDriftReasonNoWorkloadObserved  = "NoWorkloadObserved"
)

type sampledClusterRole string

const (
	sampledClusterRoleActive  sampledClusterRole = "Active"
	sampledClusterRoleStandby sampledClusterRole = "Standby"
	sampledClusterRoleUnknown sampledClusterRole = "Unknown"
)

type clusterReplicaSample struct {
	ClusterName                 string
	Role                        sampledClusterRole
	WorkloadCount               int
	NonZeroReplicaWorkloadCount int
	ZeroReplicaWorkloadCount    int
	DesiredReplicasTotal        int32
}

type roleDriftEvaluation struct {
	ConditionStatus metav1.ConditionStatus
	Reason          string
	Message         string
	HardFailure     bool
	PrimarySample   clusterReplicaSample
	SecondarySample clusterReplicaSample
}

func (r *DisasterInstanceReconciler) evaluateRoleDrift(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
) roleDriftEvaluation {
	expectedPrimary := strings.TrimSpace(instance.Status.PrimaryCluster)
	expectedSecondary := strings.TrimSpace(instance.Status.SecondaryCluster)
	if expectedPrimary == "" || expectedSecondary == "" {
		return roleDriftEvaluation{
			ConditionStatus: metav1.ConditionUnknown,
			Reason:          roleDriftReasonCheckFailed,
			Message:         fmt.Sprintf("cannot evaluate role drift because expected primary/secondary is incomplete: primary=%q secondary=%q", expectedPrimary, expectedSecondary),
		}
	}

	primarySample, primaryErr := r.sampleClusterReplicaRole(ctx, instance, expectedPrimary)
	secondarySample, secondaryErr := r.sampleClusterReplicaRole(ctx, instance, expectedSecondary)
	if primaryErr != nil || secondaryErr != nil {
		return roleDriftEvaluation{
			ConditionStatus: metav1.ConditionUnknown,
			Reason:          roleDriftReasonCheckFailed,
			Message: fmt.Sprintf(
				"failed to evaluate role drift: primary=%s err=%v secondary=%s err=%v",
				expectedPrimary,
				primaryErr,
				expectedSecondary,
				secondaryErr,
			),
			PrimarySample:   primarySample,
			SecondarySample: secondarySample,
		}
	}

	if primarySample.Role == sampledClusterRoleUnknown || secondarySample.Role == sampledClusterRoleUnknown {
		return roleDriftEvaluation{
			ConditionStatus: metav1.ConditionUnknown,
			Reason:          roleDriftReasonNoWorkloadObserved,
			Message: fmt.Sprintf(
				"cannot evaluate role drift because sampled workloads are incomplete: expectedPrimary=%s %s; expectedSecondary=%s %s",
				expectedPrimary,
				formatClusterReplicaSample(primarySample),
				expectedSecondary,
				formatClusterReplicaSample(secondarySample),
			),
			PrimarySample:   primarySample,
			SecondarySample: secondarySample,
		}
	}

	message := fmt.Sprintf(
		"expectedPrimary=%s %s; expectedSecondary=%s %s",
		expectedPrimary,
		formatClusterReplicaSample(primarySample),
		expectedSecondary,
		formatClusterReplicaSample(secondarySample),
	)

	switch {
	case primarySample.Role == sampledClusterRoleActive && secondarySample.Role == sampledClusterRoleStandby:
		return roleDriftEvaluation{
			ConditionStatus: metav1.ConditionFalse,
			Reason:          roleDriftReasonExpectedRoleMatched,
			Message:         message,
			PrimarySample:   primarySample,
			SecondarySample: secondarySample,
		}
	case primarySample.Role == sampledClusterRoleActive && secondarySample.Role == sampledClusterRoleActive:
		return roleDriftEvaluation{
			ConditionStatus: metav1.ConditionFalse,
			Reason:          roleDriftReasonBothActiveObserved,
			Message:         message + "; both clusters have non-zero replicas, which is recorded as dual-active observation and is not treated as hard role drift",
			PrimarySample:   primarySample,
			SecondarySample: secondarySample,
		}
	case primarySample.Role == sampledClusterRoleStandby && secondarySample.Role == sampledClusterRoleActive:
		return roleDriftEvaluation{
			ConditionStatus: metav1.ConditionTrue,
			Reason:          roleDriftReasonRoleReversed,
			Message:         message + "; expected primary is scaled to zero while expected secondary has non-zero replicas",
			HardFailure:     true,
			PrimarySample:   primarySample,
			SecondarySample: secondarySample,
		}
	case primarySample.Role == sampledClusterRoleStandby && secondarySample.Role == sampledClusterRoleStandby:
		return roleDriftEvaluation{
			ConditionStatus: metav1.ConditionTrue,
			Reason:          roleDriftReasonBothStandby,
			Message:         message + "; both clusters are scaled to zero",
			HardFailure:     true,
			PrimarySample:   primarySample,
			SecondarySample: secondarySample,
		}
	default:
		return roleDriftEvaluation{
			ConditionStatus: metav1.ConditionUnknown,
			Reason:          roleDriftReasonCheckFailed,
			Message:         message + "; unsupported sampled role combination",
			PrimarySample:   primarySample,
			SecondarySample: secondarySample,
		}
	}
}

func (r *DisasterInstanceReconciler) sampleClusterReplicaRole(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
	clusterName string,
) (clusterReplicaSample, error) {
	sample := clusterReplicaSample{ClusterName: clusterName, Role: sampledClusterRoleUnknown}
	remoteClient, err := r.getRoleDriftClusterClient(ctx, clusterName)
	if err != nil {
		return sample, err
	}

	namespaceScopes := instance.Spec.Namespaces
	if len(namespaceScopes) == 0 {
		namespaceScopes = []string{""}
	}
	for _, namespace := range namespaceScopes {
		listOptions, err := roleDriftListOptions(instance, namespace)
		if err != nil {
			return sample, err
		}

		deployments := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployments, listOptions...); err != nil {
			return sample, err
		}
		for i := range deployments.Items {
			replicas := int32(1)
			if deployments.Items[i].Spec.Replicas != nil {
				replicas = *deployments.Items[i].Spec.Replicas
			}
			sample.addWorkload(replicas)
		}

		statefulSets := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, statefulSets, listOptions...); err != nil {
			return sample, err
		}
		for i := range statefulSets.Items {
			replicas := int32(1)
			if statefulSets.Items[i].Spec.Replicas != nil {
				replicas = *statefulSets.Items[i].Spec.Replicas
			}
			sample.addWorkload(replicas)
		}
	}

	switch {
	case sample.WorkloadCount == 0:
		sample.Role = sampledClusterRoleUnknown
	case sample.NonZeroReplicaWorkloadCount > 0:
		sample.Role = sampledClusterRoleActive
	default:
		sample.Role = sampledClusterRoleStandby
	}
	return sample, nil
}

func (s *clusterReplicaSample) addWorkload(replicas int32) {
	s.WorkloadCount++
	s.DesiredReplicasTotal += replicas
	if replicas > 0 {
		s.NonZeroReplicaWorkloadCount++
		return
	}
	s.ZeroReplicaWorkloadCount++
}

func (r *DisasterInstanceReconciler) getRoleDriftClusterClient(ctx context.Context, clusterName string) (client.Client, error) {
	if r.KubeClientGetter != nil {
		return r.KubeClientGetter(ctx, r.Client, r.Scheme, clusterName)
	}
	return ctrlpkg.GetKubeClientSet(ctx, r.Client, r.Scheme, clusterName)
}

func roleDriftListOptions(instance *disasterv1.DisasterInstance, namespace string) ([]client.ListOption, error) {
	options := make([]client.ListOption, 0, 2)
	if strings.TrimSpace(namespace) != "" {
		options = append(options, client.InNamespace(namespace))
	}
	if instance.Spec.LabelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(instance.Spec.LabelSelector)
		if err != nil {
			return nil, err
		}
		options = append(options, client.MatchingLabelsSelector{Selector: selector})
	}
	return options, nil
}

func formatClusterReplicaSample(sample clusterReplicaSample) string {
	return fmt.Sprintf(
		"role=%s workloads=%d nonZeroWorkloads=%d zeroWorkloads=%d desiredReplicas=%d",
		sample.Role,
		sample.WorkloadCount,
		sample.NonZeroReplicaWorkloadCount,
		sample.ZeroReplicaWorkloadCount,
		sample.DesiredReplicasTotal,
	)
}

func applyRoleDriftCondition(instance *disasterv1.DisasterInstance, evaluation roleDriftEvaluation) {
	apimeta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               instanceConditionRoleDrift,
		Status:             evaluation.ConditionStatus,
		Reason:             evaluation.Reason,
		Message:            evaluation.Message,
		ObservedGeneration: instance.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *DisasterInstanceReconciler) guardByRoleDrift(
	ctx context.Context,
	log logr.Logger,
	instance *disasterv1.DisasterInstance,
) (handled bool, err error) {
	evaluation := r.evaluateRoleDrift(ctx, instance)
	applyRoleDriftCondition(instance, evaluation)
	if !evaluation.HardFailure {
		return false, nil
	}

	instance.Status.FsmState = disasterv1.FsmStateFailed
	helper.SetStatusError(&instance.Status, instanceReasonRoleDriftDetected, evaluation.Message)
	instance.Status.AvailableOperations = []string{}
	if err := r.Status().Update(ctx, instance); err != nil {
		return true, err
	}
	log.Info("实例真实主备关系与期望不一致，进入 Failed", "reason", evaluation.Reason, "message", evaluation.Message)
	r.Recorder.Eventf(instance, "Warning", "RoleDriftDetected", "真实主备关系与期望不一致: %s", evaluation.Message)
	return true, nil
}
