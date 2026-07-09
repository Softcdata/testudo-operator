/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package runtimeconfig

import (
	"context"
	"fmt"
	"strings"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Reconciler watches the OperatorRuntimeConfig singleton and publishes an atomic runtime snapshot.
type Reconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Namespace string
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=operatorruntimeconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=operatorruntimeconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithName("runtime-config")
	if req.Name != SingletonName || (r.Namespace != "" && req.Namespace != r.Namespace) {
		logger.V(1).Info("ignoring non-singleton OperatorRuntimeConfig", "namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{}, nil
	}

	cfg := &disasterv1.OperatorRuntimeConfig{}
	if err := r.Get(ctx, req.NamespacedName, cfg); err != nil {
		if apierrors.IsNotFound(err) {
			active := ResetToStartupDefaults()
			logger.Info("OperatorRuntimeConfig singleton deleted or absent; reverted to startup defaults", "source", active.Source)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	base := StartupSnapshot()
	next, validationErrs := MergeSpec(base, cfg.Spec, cfg.Generation)
	if len(validationErrs) > 0 {
		message := FormatErrors(validationErrs)
		logger.Info("OperatorRuntimeConfig is invalid; keeping last valid runtime snapshot", "errors", message)
		if r.Recorder != nil {
			r.Recorder.Event(cfg, corev1.EventTypeWarning, "RuntimeConfigInvalid", message)
		}
		return ctrl.Result{}, r.updateStatus(ctx, cfg, false, message)
	}

	Activate(next)
	message := fmt.Sprintf("runtime config activated from generation %d", cfg.Generation)
	logger.Info(message)
	if r.Recorder != nil && !conditionIsTrue(cfg.Status.Conditions, disasterv1.OperatorRuntimeConfigConditionReady) {
		r.Recorder.Event(cfg, corev1.EventTypeNormal, "RuntimeConfigReady", message)
	}
	return ctrl.Result{}, r.updateStatus(ctx, cfg, true, message)
}

func (r *Reconciler) updateStatus(ctx context.Context, cfg *disasterv1.OperatorRuntimeConfig, ready bool, message string) error {
	now := metav1.Now()
	cfg.Status.ObservedGeneration = cfg.Generation
	if ready {
		cfg.Status.ActiveGeneration = cfg.Generation
		cfg.Status.LastActivatedTime = &now
		meta.SetStatusCondition(&cfg.Status.Conditions, metav1.Condition{
			Type:               disasterv1.OperatorRuntimeConfigConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Activated",
			Message:            message,
			ObservedGeneration: cfg.Generation,
			LastTransitionTime: now,
		})
		meta.SetStatusCondition(&cfg.Status.Conditions, metav1.Condition{
			Type:               disasterv1.OperatorRuntimeConfigConditionInvalid,
			Status:             metav1.ConditionFalse,
			Reason:             "Valid",
			Message:            "runtime config is valid",
			ObservedGeneration: cfg.Generation,
			LastTransitionTime: now,
		})
		return r.Status().Update(ctx, cfg)
	}

	meta.SetStatusCondition(&cfg.Status.Conditions, metav1.Condition{
		Type:               disasterv1.OperatorRuntimeConfigConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Invalid",
		Message:            "runtime config was not activated",
		ObservedGeneration: cfg.Generation,
		LastTransitionTime: now,
	})
	meta.SetStatusCondition(&cfg.Status.Conditions, metav1.Condition{
		Type:               disasterv1.OperatorRuntimeConfigConditionInvalid,
		Status:             metav1.ConditionTrue,
		Reason:             "ValidationFailed",
		Message:            message,
		ObservedGeneration: cfg.Generation,
		LastTransitionTime: now,
	})
	return r.Status().Update(ctx, cfg)
}

func conditionIsTrue(conditions []metav1.Condition, conditionType string) bool {
	cond := meta.FindStatusCondition(conditions, conditionType)
	return cond != nil && cond.Status == metav1.ConditionTrue
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if strings.TrimSpace(r.Namespace) == "" {
		r.Namespace = "disaster-system"
	}
	if r.Client == nil {
		r.Client = mgr.GetClient()
	}
	if r.Scheme == nil {
		r.Scheme = mgr.GetScheme()
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("runtimeconfig-controller")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.OperatorRuntimeConfig{}).
		Named("operatorruntimeconfig").
		Complete(r)
}
