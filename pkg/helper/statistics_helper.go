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

package helper

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

// StatisticsHelper defines the interface for managing BackupRestoreStatistics
type StatisticsHelper interface {
	GetOrCreate(ctx context.Context, scopeType disasterv1.ScopeType, scopeRef disasterv1.ScopeReference, namespace string, additionalLabels map[string]string, owner metav1.Object, scheme *runtime.Scheme) (*disasterv1.BackupRestoreStatistics, error)
	Get(ctx context.Context, name, namespace string) (*disasterv1.BackupRestoreStatistics, error)
	IncrementCounter(ctx context.Context, stats *disasterv1.BackupRestoreStatistics, counterName string, value int32, reason string) error
	DecrementCounter(ctx context.Context, stats *disasterv1.BackupRestoreStatistics, counterName string, value int32, reason string) error
	TransitionState(ctx context.Context, stats *disasterv1.BackupRestoreStatistics, changes map[string]int32, reason string) error
	SyncStats(ctx context.Context, stats *disasterv1.BackupRestoreStatistics, snapshot *disasterv1.BackupRestoreStatisticsStatus, reason string) error
	AggregateStatistics(ctx context.Context, namespace string, labels map[string]string) (*disasterv1.StatisticsData, error)
}

type statisticsHelper struct {
	client client.Client
	logger logr.Logger
}

// NewStatisticsHelper creates a new StatisticsHelper
func NewStatisticsHelper(client client.Client) StatisticsHelper {
	return &statisticsHelper{
		client: client,
		logger: log.Log.WithName("statistics-helper"),
	}
}

func (h *statisticsHelper) GetOrCreate(ctx context.Context, scopeType disasterv1.ScopeType, scopeRef disasterv1.ScopeReference, namespace string, additionalLabels map[string]string, owner metav1.Object, scheme *runtime.Scheme) (*disasterv1.BackupRestoreStatistics, error) {
	name := h.generateStatsName(scopeType, scopeRef)
	stats := &disasterv1.BackupRestoreStatistics{}

	err := h.client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, stats)
	if err == nil {
		// Check if labels need update
		updated := false
		if stats.Labels == nil {
			stats.Labels = make(map[string]string)
		}
		for k, v := range additionalLabels {
			if stats.Labels[k] != v {
				stats.Labels[k] = v
				updated = true
			}
		}
		// Ensure OwnerReference is set if missing (for existing resources)
		if owner != nil && scheme != nil {
			if len(stats.OwnerReferences) == 0 {
				if err := controllerutil.SetControllerReference(owner, stats, scheme); err == nil {
					updated = true
				}
			}
		}

		if updated {
			if err := h.client.Update(ctx, stats); err != nil {
				return nil, err
			}
		}
		return stats, nil
	}

	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}

	// Create new
	labels := map[string]string{
		"disaster.io/scope-type": string(scopeType),
		"disaster.io/scope-uid":  string(scopeRef.UID),
	}
	for k, v := range additionalLabels {
		labels[k] = v
	}

	stats = &disasterv1.BackupRestoreStatistics{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: disasterv1.BackupRestoreStatisticsSpec{
			ScopeType: scopeType,
			ScopeRef:  scopeRef,
		},
		Status: disasterv1.BackupRestoreStatisticsStatus{
			Statistics: disasterv1.StatisticsData{},
		},
	}

	if owner != nil && scheme != nil {
		if err := controllerutil.SetControllerReference(owner, stats, scheme); err != nil {
			return nil, err
		}
	}

	if err := h.client.Create(ctx, stats); err != nil {
		return nil, err
	}

	return stats, nil
}

func (h *statisticsHelper) Get(ctx context.Context, name, namespace string) (*disasterv1.BackupRestoreStatistics, error) {
	stats := &disasterv1.BackupRestoreStatistics{}
	err := h.client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, stats)
	return stats, err
}

func (h *statisticsHelper) IncrementCounter(ctx context.Context, stats *disasterv1.BackupRestoreStatistics, counterName string, value int32, reason string) error {
	return h.TransitionState(ctx, stats, map[string]int32{counterName: value}, reason)
}

func (h *statisticsHelper) DecrementCounter(ctx context.Context, stats *disasterv1.BackupRestoreStatistics, counterName string, value int32, reason string) error {
	return h.TransitionState(ctx, stats, map[string]int32{counterName: -value}, reason)
}

func (h *statisticsHelper) TransitionState(ctx context.Context, stats *disasterv1.BackupRestoreStatistics, changes map[string]int32, reason string) error {
	// Use RetryOnConflict to handle concurrent updates
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch latest version
		currentStats := &disasterv1.BackupRestoreStatistics{}
		if err := h.client.Get(ctx, types.NamespacedName{Name: stats.Name, Namespace: stats.Namespace}, currentStats); err != nil {
			return err
		}

		original := currentStats.DeepCopy()

		// Apply changes
		for k, v := range changes {
			switch strings.ToLower(k) {
			case "total":
				currentStats.Status.Statistics.Total += v
			case "inprogress":
				currentStats.Status.Statistics.InProgress += v
			case "completed":
				currentStats.Status.Statistics.Completed += v
			case "failed":
				currentStats.Status.Statistics.Failed += v
			case "canceled":
				currentStats.Status.Statistics.Canceled += v
			case "unknown":
				currentStats.Status.Statistics.Unknown += v
			}
		}

		// Update metadata
		now := metav1.Now()
		currentStats.Status.LastUpdateTime = &now
		currentStats.Status.LastUpdateReason = reason

		// Append event
		event := disasterv1.StatisticsEvent{
			Timestamp: now,
			Type:      "Transition",
			Reason:    reason,
			Changes:   changes,
		}
		h.appendEvent(currentStats, event)

		return h.client.Status().Patch(ctx, currentStats, client.MergeFrom(original))
	})
}

func (h *statisticsHelper) SyncStats(ctx context.Context, stats *disasterv1.BackupRestoreStatistics, snapshot *disasterv1.BackupRestoreStatisticsStatus, reason string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentStats := &disasterv1.BackupRestoreStatistics{}
		if err := h.client.Get(ctx, types.NamespacedName{Name: stats.Name, Namespace: stats.Namespace}, currentStats); err != nil {
			return err
		}

		// Check if update is needed
		if h.isStatsEqual(currentStats.Status.Statistics, snapshot.Statistics) {
			return nil
		}

		original := currentStats.DeepCopy()

		// Overwrite statistics
		currentStats.Status.Statistics = snapshot.Statistics

		// Update metadata
		now := metav1.Now()
		currentStats.Status.LastUpdateTime = &now
		currentStats.Status.LastUpdateReason = reason

		// Append event
		event := disasterv1.StatisticsEvent{
			Timestamp: now,
			Type:      "Sync",
			Reason:    reason,
			Message:   "Full sync from snapshot",
		}
		h.appendEvent(currentStats, event)

		return h.client.Status().Patch(ctx, currentStats, client.MergeFrom(original))
	})
}

// AggregateStatistics aggregates statistics for a given namespace and labels
func (h *statisticsHelper) AggregateStatistics(ctx context.Context, namespace string, labels map[string]string) (*disasterv1.StatisticsData, error) {
	list := &disasterv1.BackupRestoreStatisticsList{}

	opts := []client.ListOption{
		client.InNamespace(namespace),
	}
	if len(labels) > 0 {
		opts = append(opts, client.MatchingLabels(labels))
	}

	if err := h.client.List(ctx, list, opts...); err != nil {
		return nil, err
	}

	// Initialize result
	result := &disasterv1.StatisticsData{}

	for _, item := range list.Items {
		// Filter: only aggregate app level statistics to avoid double counting if other levels are introduced
		if item.Spec.ScopeType != disasterv1.ScopeTypeApp {
			continue
		}

		result.Total += item.Status.Statistics.Total
		result.InProgress += item.Status.Statistics.InProgress
		result.Completed += item.Status.Statistics.Completed
		result.Failed += item.Status.Statistics.Failed
		result.Canceled += item.Status.Statistics.Canceled
		result.Unknown += item.Status.Statistics.Unknown
	}

	return result, nil
}

func (h *statisticsHelper) generateStatsName(scopeType disasterv1.ScopeType, scopeRef disasterv1.ScopeReference) string {
	// Simple naming strategy: <scopeType>-<refName>-stats
	// For AppBackup, name is unique in namespace.
	return fmt.Sprintf("%s-%s-stats", scopeType, scopeRef.Name)
}

func (h *statisticsHelper) appendEvent(stats *disasterv1.BackupRestoreStatistics, event disasterv1.StatisticsEvent) {
	if len(stats.Status.EventLog) >= 100 {
		// Remove oldest
		stats.Status.EventLog = stats.Status.EventLog[1:]
	}
	stats.Status.EventLog = append(stats.Status.EventLog, event)
}

func (h *statisticsHelper) isStatsEqual(a, b disasterv1.StatisticsData) bool {
	return a.Total == b.Total &&
		a.InProgress == b.InProgress &&
		a.Completed == b.Completed &&
		a.Failed == b.Failed &&
		a.Canceled == b.Canceled &&
		a.Unknown == b.Unknown
}
