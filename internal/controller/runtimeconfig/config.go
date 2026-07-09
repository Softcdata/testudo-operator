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
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

const (
	SingletonName = "default"
)

type Snapshot struct {
	Source           string
	Generation       int64
	BackupRuntime    BackupRuntime
	RestoreRuntime   RestoreRuntime
	OperationRuntime OperationRuntime
	InstanceRuntime  InstanceRuntime
	SyncRuntime      SyncRuntime
	StorageRuntime   StorageRuntime
	ClusterRuntime   ClusterRuntime
}

type BackupRuntime struct {
	InProgressMaxWait time.Duration
	UnknownMaxWait    time.Duration
	PollInterval      time.Duration
}

type RestoreRuntime struct {
	InProgressMaxWait           time.Duration
	UnknownMaxWait              time.Duration
	InProgressPollInterval      time.Duration
	UnknownPollInterval         time.Duration
	ProgressCompleteGrace       time.Duration
	StartupGrace                time.Duration
	MissingGrace                time.Duration
	EmptyStatusGrace            time.Duration
	PodVolumeRestorePendingWait time.Duration
	RetryBackoff                time.Duration
	RetryLimit                  int
	RetryLimitProgress          int
	RetryLimitStartup           int
	RetryLimitMissing           int
	RetryLimitEmpty             int
}

type OperationRuntime struct {
	DefaultTimeoutMinutes int32
	StepStartRequeue      time.Duration
	StepRunningRequeue    time.Duration
	DefaultRetryInterval  time.Duration
}

type InstanceRuntime struct {
	TransitionWatchdogTimeout    time.Duration
	MinTransitionWatchdogTimeout time.Duration
	InitializingRequeue          time.Duration
	SteadyRequeue                time.Duration
	FailedRequeue                time.Duration
}

type SyncRuntime struct {
	SchedulerUpdateTimeout  time.Duration
	BackupObserveRequeue    time.Duration
	BackupInProgressRequeue time.Duration
	HistoryMissingRequeue   time.Duration
	RestoreObserveRequeue   time.Duration
	HistoryRetention        int
}

type StorageRuntime struct {
	RequeueInterval time.Duration
}

type ClusterRuntime struct {
	ReconcileInterval         time.Duration
	DeletionRetryInterval     time.Duration
	VeleroInstallTimeout      time.Duration
	VeleroZombieLockThreshold time.Duration
}

type FieldError struct {
	Field   string
	Message string
}

func (e FieldError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

var (
	startupMu sync.RWMutex
	startup   Snapshot
	active    atomic.Value
	enabled   atomic.Bool
)

func init() {
	startup = DefaultSnapshot()
	active.Store(startup)
}

func DefaultSnapshot() Snapshot {
	return Snapshot{
		Source: "defaults",
		BackupRuntime: BackupRuntime{
			InProgressMaxWait: 2 * time.Hour,
			UnknownMaxWait:    10 * time.Minute,
			PollInterval:      10 * time.Second,
		},
		RestoreRuntime: RestoreRuntime{
			InProgressMaxWait:           time.Hour,
			UnknownMaxWait:              time.Hour,
			InProgressPollInterval:      5 * time.Second,
			UnknownPollInterval:         10 * time.Second,
			ProgressCompleteGrace:       5 * time.Minute,
			StartupGrace:                5 * time.Minute,
			MissingGrace:                90 * time.Second,
			EmptyStatusGrace:            5 * time.Minute,
			PodVolumeRestorePendingWait: 10 * time.Minute,
			RetryBackoff:                15 * time.Second,
			RetryLimit:                  1,
			RetryLimitProgress:          1,
			RetryLimitStartup:           1,
			RetryLimitMissing:           2,
			RetryLimitEmpty:             2,
		},
		OperationRuntime: OperationRuntime{
			DefaultTimeoutMinutes: 60,
			StepStartRequeue:      time.Second,
			StepRunningRequeue:    5 * time.Second,
			DefaultRetryInterval:  5 * time.Second,
		},
		InstanceRuntime: InstanceRuntime{
			TransitionWatchdogTimeout:    2 * time.Minute,
			MinTransitionWatchdogTimeout: 30 * time.Second,
			InitializingRequeue:          10 * time.Second,
			SteadyRequeue:                time.Minute,
			FailedRequeue:                time.Minute,
		},
		SyncRuntime: SyncRuntime{
			SchedulerUpdateTimeout:  30 * time.Second,
			BackupObserveRequeue:    2 * time.Second,
			BackupInProgressRequeue: 5 * time.Second,
			HistoryMissingRequeue:   5 * time.Second,
			RestoreObserveRequeue:   10 * time.Second,
			HistoryRetention:        20,
		},
		StorageRuntime: StorageRuntime{
			RequeueInterval: 10 * time.Second,
		},
		ClusterRuntime: ClusterRuntime{
			ReconcileInterval:         time.Minute,
			DeletionRetryInterval:     10 * time.Second,
			VeleroInstallTimeout:      10 * time.Minute,
			VeleroZombieLockThreshold: 10 * time.Minute,
		},
	}
}

func SetStartupDefaults(s Snapshot) {
	if s.Source == "" {
		s.Source = "startup"
	}
	startupMu.Lock()
	startup = s
	startupMu.Unlock()
	active.Store(s)
	enabled.Store(true)
}

func ResetToStartupDefaults() Snapshot {
	startupMu.RLock()
	s := startup
	startupMu.RUnlock()
	s.Source = "startup"
	s.Generation = 0
	active.Store(s)
	return s
}

func Activate(s Snapshot) {
	active.Store(s)
	enabled.Store(true)
}

func SnapshotCurrent() Snapshot {
	if v := active.Load(); v != nil {
		return v.(Snapshot)
	}
	return DefaultSnapshot()
}

func ActiveSnapshot() (Snapshot, bool) {
	return SnapshotCurrent(), enabled.Load()
}

func StartupSnapshot() Snapshot {
	startupMu.RLock()
	defer startupMu.RUnlock()
	return startup
}

func ResetForTest() {
	defaults := DefaultSnapshot()
	startupMu.Lock()
	startup = defaults
	startupMu.Unlock()
	active.Store(defaults)
	enabled.Store(false)
}

func MergeSpec(base Snapshot, spec disasterv1.OperatorRuntimeConfigSpec, generation int64) (Snapshot, []FieldError) {
	next := base
	next.Source = "OperatorRuntimeConfig/default"
	next.Generation = generation

	if spec.BackupRuntime != nil {
		mergeBackupRuntime(&next, spec.BackupRuntime)
	}
	if spec.RestoreRuntime != nil {
		mergeRestoreRuntime(&next, spec.RestoreRuntime)
	}
	if spec.OperationRuntime != nil {
		mergeOperationRuntime(&next, spec.OperationRuntime)
	}
	if spec.InstanceRuntime != nil {
		mergeInstanceRuntime(&next, spec.InstanceRuntime)
	}
	if spec.SyncRuntime != nil {
		mergeSyncRuntime(&next, spec.SyncRuntime)
	}
	if spec.StorageRuntime != nil {
		mergeStorageRuntime(&next, spec.StorageRuntime)
	}
	if spec.ClusterRuntime != nil {
		mergeClusterRuntime(&next, spec.ClusterRuntime)
	}

	return next, Validate(next)
}

func mergeBackupRuntime(next *Snapshot, spec *disasterv1.BackupRuntimeConfigSpec) {
	if v := spec.InProgressMaxWait; v != nil {
		next.BackupRuntime.InProgressMaxWait = v.Duration
	}
	if v := spec.UnknownMaxWait; v != nil {
		next.BackupRuntime.UnknownMaxWait = v.Duration
	}
	if v := spec.PollInterval; v != nil {
		next.BackupRuntime.PollInterval = v.Duration
	}
}

func mergeRestoreRuntime(next *Snapshot, spec *disasterv1.RestoreRuntimeConfigSpec) {
	if v := spec.InProgressMaxWait; v != nil {
		next.RestoreRuntime.InProgressMaxWait = v.Duration
	}
	if v := spec.UnknownMaxWait; v != nil {
		next.RestoreRuntime.UnknownMaxWait = v.Duration
	}
	if v := spec.InProgressPollInterval; v != nil {
		next.RestoreRuntime.InProgressPollInterval = v.Duration
	}
	if v := spec.UnknownPollInterval; v != nil {
		next.RestoreRuntime.UnknownPollInterval = v.Duration
	}
	if v := spec.ProgressCompleteGrace; v != nil {
		next.RestoreRuntime.ProgressCompleteGrace = v.Duration
	}
	if v := spec.StartupGrace; v != nil {
		next.RestoreRuntime.StartupGrace = v.Duration
	}
	if v := spec.MissingGrace; v != nil {
		next.RestoreRuntime.MissingGrace = v.Duration
	}
	if v := spec.EmptyStatusGrace; v != nil {
		next.RestoreRuntime.EmptyStatusGrace = v.Duration
	}
	if v := spec.PodVolumeRestorePendingWait; v != nil {
		next.RestoreRuntime.PodVolumeRestorePendingWait = v.Duration
	}
	if v := spec.RetryBackoff; v != nil {
		next.RestoreRuntime.RetryBackoff = v.Duration
	}
	if v := spec.RetryLimit; v != nil {
		limit := int(*v)
		next.RestoreRuntime.RetryLimit = limit
		if spec.RetryLimitProgress == nil {
			next.RestoreRuntime.RetryLimitProgress = limit
		}
		if spec.RetryLimitStartup == nil {
			next.RestoreRuntime.RetryLimitStartup = limit
		}
		if spec.RetryLimitMissing == nil {
			next.RestoreRuntime.RetryLimitMissing = limit
		}
		if spec.RetryLimitEmpty == nil {
			next.RestoreRuntime.RetryLimitEmpty = limit
		}
	}
	if v := spec.RetryLimitProgress; v != nil {
		next.RestoreRuntime.RetryLimitProgress = int(*v)
	}
	if v := spec.RetryLimitStartup; v != nil {
		next.RestoreRuntime.RetryLimitStartup = int(*v)
	}
	if v := spec.RetryLimitMissing; v != nil {
		next.RestoreRuntime.RetryLimitMissing = int(*v)
	}
	if v := spec.RetryLimitEmpty; v != nil {
		next.RestoreRuntime.RetryLimitEmpty = int(*v)
	}
}

func mergeOperationRuntime(next *Snapshot, spec *disasterv1.OperationRuntimeConfigSpec) {
	if v := spec.DefaultTimeoutMinutes; v != nil {
		next.OperationRuntime.DefaultTimeoutMinutes = *v
	}
	if v := spec.StepStartRequeue; v != nil {
		next.OperationRuntime.StepStartRequeue = v.Duration
	}
	if v := spec.StepRunningRequeue; v != nil {
		next.OperationRuntime.StepRunningRequeue = v.Duration
	}
	if v := spec.DefaultRetryInterval; v != nil {
		next.OperationRuntime.DefaultRetryInterval = v.Duration
	}
}

func mergeInstanceRuntime(next *Snapshot, spec *disasterv1.InstanceRuntimeConfigSpec) {
	if v := spec.TransitionWatchdogTimeout; v != nil {
		next.InstanceRuntime.TransitionWatchdogTimeout = v.Duration
	}
	if v := spec.MinTransitionWatchdogTimeout; v != nil {
		next.InstanceRuntime.MinTransitionWatchdogTimeout = v.Duration
	}
	if v := spec.InitializingRequeue; v != nil {
		next.InstanceRuntime.InitializingRequeue = v.Duration
	}
	if v := spec.SteadyRequeue; v != nil {
		next.InstanceRuntime.SteadyRequeue = v.Duration
	}
	if v := spec.FailedRequeue; v != nil {
		next.InstanceRuntime.FailedRequeue = v.Duration
	}
}

func mergeSyncRuntime(next *Snapshot, spec *disasterv1.SyncRuntimeConfigSpec) {
	if v := spec.SchedulerUpdateTimeout; v != nil {
		next.SyncRuntime.SchedulerUpdateTimeout = v.Duration
	}
	if v := spec.BackupObserveRequeue; v != nil {
		next.SyncRuntime.BackupObserveRequeue = v.Duration
	}
	if v := spec.BackupInProgressRequeue; v != nil {
		next.SyncRuntime.BackupInProgressRequeue = v.Duration
	}
	if v := spec.HistoryMissingRequeue; v != nil {
		next.SyncRuntime.HistoryMissingRequeue = v.Duration
	}
	if v := spec.RestoreObserveRequeue; v != nil {
		next.SyncRuntime.RestoreObserveRequeue = v.Duration
	}
	if v := spec.HistoryRetention; v != nil {
		next.SyncRuntime.HistoryRetention = int(*v)
	}
}

func mergeStorageRuntime(next *Snapshot, spec *disasterv1.StorageRuntimeConfigSpec) {
	if v := spec.RequeueInterval; v != nil {
		next.StorageRuntime.RequeueInterval = v.Duration
	}
}

func mergeClusterRuntime(next *Snapshot, spec *disasterv1.ClusterRuntimeConfigSpec) {
	if v := spec.ReconcileInterval; v != nil {
		next.ClusterRuntime.ReconcileInterval = v.Duration
	}
	if v := spec.DeletionRetryInterval; v != nil {
		next.ClusterRuntime.DeletionRetryInterval = v.Duration
	}
	if v := spec.VeleroInstallTimeout; v != nil {
		next.ClusterRuntime.VeleroInstallTimeout = v.Duration
	}
	if v := spec.VeleroZombieLockThreshold; v != nil {
		next.ClusterRuntime.VeleroZombieLockThreshold = v.Duration
	}
}

func Validate(s Snapshot) []FieldError {
	var errs []FieldError
	errs = append(errs, checkDuration("backupRuntime.inProgressMaxWait", s.BackupRuntime.InProgressMaxWait, time.Minute, 24*time.Hour)...)
	errs = append(errs, checkDuration("backupRuntime.unknownMaxWait", s.BackupRuntime.UnknownMaxWait, time.Minute, 24*time.Hour)...)
	errs = append(errs, checkDuration("backupRuntime.pollInterval", s.BackupRuntime.PollInterval, time.Second, 5*time.Minute)...)

	errs = append(errs, checkDuration("restoreRuntime.inProgressMaxWait", s.RestoreRuntime.InProgressMaxWait, time.Minute, 24*time.Hour)...)
	errs = append(errs, checkDuration("restoreRuntime.unknownMaxWait", s.RestoreRuntime.UnknownMaxWait, time.Minute, 24*time.Hour)...)
	errs = append(errs, checkDuration("restoreRuntime.inProgressPollInterval", s.RestoreRuntime.InProgressPollInterval, time.Second, 5*time.Minute)...)
	errs = append(errs, checkDuration("restoreRuntime.unknownPollInterval", s.RestoreRuntime.UnknownPollInterval, time.Second, 5*time.Minute)...)
	errs = append(errs, checkDuration("restoreRuntime.progressCompleteGrace", s.RestoreRuntime.ProgressCompleteGrace, 30*time.Second, 24*time.Hour)...)
	errs = append(errs, checkDuration("restoreRuntime.startupGrace", s.RestoreRuntime.StartupGrace, 30*time.Second, 24*time.Hour)...)
	errs = append(errs, checkDuration("restoreRuntime.missingGrace", s.RestoreRuntime.MissingGrace, 30*time.Second, 24*time.Hour)...)
	errs = append(errs, checkDuration("restoreRuntime.emptyStatusGrace", s.RestoreRuntime.EmptyStatusGrace, 30*time.Second, 24*time.Hour)...)
	errs = append(errs, checkDuration("restoreRuntime.podVolumeRestorePendingMaxWait", s.RestoreRuntime.PodVolumeRestorePendingWait, time.Minute, 24*time.Hour)...)
	errs = append(errs, checkDuration("restoreRuntime.retryBackoff", s.RestoreRuntime.RetryBackoff, time.Second, time.Hour)...)
	errs = append(errs, checkInt("restoreRuntime.retryLimit", s.RestoreRuntime.RetryLimit, 0, 10)...)
	errs = append(errs, checkInt("restoreRuntime.retryLimitProgress", s.RestoreRuntime.RetryLimitProgress, 0, 10)...)
	errs = append(errs, checkInt("restoreRuntime.retryLimitStartup", s.RestoreRuntime.RetryLimitStartup, 0, 10)...)
	errs = append(errs, checkInt("restoreRuntime.retryLimitMissing", s.RestoreRuntime.RetryLimitMissing, 0, 10)...)
	errs = append(errs, checkInt("restoreRuntime.retryLimitEmpty", s.RestoreRuntime.RetryLimitEmpty, 0, 10)...)

	errs = append(errs, checkInt32("operationRuntime.defaultTimeoutMinutes", s.OperationRuntime.DefaultTimeoutMinutes, 1, 1440)...)
	errs = append(errs, checkDuration("operationRuntime.stepStartRequeue", s.OperationRuntime.StepStartRequeue, time.Second, 5*time.Minute)...)
	errs = append(errs, checkDuration("operationRuntime.stepRunningRequeue", s.OperationRuntime.StepRunningRequeue, time.Second, 5*time.Minute)...)
	errs = append(errs, checkDuration("operationRuntime.defaultRetryInterval", s.OperationRuntime.DefaultRetryInterval, time.Second, time.Hour)...)

	errs = append(errs, checkDuration("instanceRuntime.transitionWatchdogTimeout", s.InstanceRuntime.TransitionWatchdogTimeout, 30*time.Second, 24*time.Hour)...)
	errs = append(errs, checkDuration("instanceRuntime.minTransitionWatchdogTimeout", s.InstanceRuntime.MinTransitionWatchdogTimeout, 10*time.Second, time.Hour)...)
	if s.InstanceRuntime.TransitionWatchdogTimeout < s.InstanceRuntime.MinTransitionWatchdogTimeout {
		errs = append(errs, FieldError{
			Field:   "instanceRuntime.transitionWatchdogTimeout",
			Message: fmt.Sprintf("must be >= instanceRuntime.minTransitionWatchdogTimeout (%s)", s.InstanceRuntime.MinTransitionWatchdogTimeout),
		})
	}
	errs = append(errs, checkDuration("instanceRuntime.initializingRequeue", s.InstanceRuntime.InitializingRequeue, time.Second, 10*time.Minute)...)
	errs = append(errs, checkDuration("instanceRuntime.steadyRequeue", s.InstanceRuntime.SteadyRequeue, 5*time.Second, 30*time.Minute)...)
	errs = append(errs, checkDuration("instanceRuntime.failedRequeue", s.InstanceRuntime.FailedRequeue, 5*time.Second, 30*time.Minute)...)

	errs = append(errs, checkDuration("syncRuntime.schedulerUpdateTimeout", s.SyncRuntime.SchedulerUpdateTimeout, time.Second, 10*time.Minute)...)
	errs = append(errs, checkDuration("syncRuntime.backupObserveRequeue", s.SyncRuntime.BackupObserveRequeue, time.Second, 5*time.Minute)...)
	errs = append(errs, checkDuration("syncRuntime.backupInProgressRequeue", s.SyncRuntime.BackupInProgressRequeue, time.Second, 5*time.Minute)...)
	errs = append(errs, checkDuration("syncRuntime.historyMissingRequeue", s.SyncRuntime.HistoryMissingRequeue, time.Second, 5*time.Minute)...)
	errs = append(errs, checkDuration("syncRuntime.restoreObserveRequeue", s.SyncRuntime.RestoreObserveRequeue, time.Second, 5*time.Minute)...)
	errs = append(errs, checkInt("syncRuntime.historyRetention", s.SyncRuntime.HistoryRetention, 1, 500)...)

	errs = append(errs, checkDuration("storageRuntime.requeueInterval", s.StorageRuntime.RequeueInterval, 5*time.Second, time.Hour)...)
	errs = append(errs, checkDuration("clusterRuntime.reconcileInterval", s.ClusterRuntime.ReconcileInterval, 10*time.Second, time.Hour)...)
	errs = append(errs, checkDuration("clusterRuntime.deletionRetryInterval", s.ClusterRuntime.DeletionRetryInterval, time.Second, 10*time.Minute)...)
	errs = append(errs, checkDuration("clusterRuntime.veleroInstallTimeout", s.ClusterRuntime.VeleroInstallTimeout, time.Minute, 2*time.Hour)...)
	errs = append(errs, checkDuration("clusterRuntime.veleroZombieLockThreshold", s.ClusterRuntime.VeleroZombieLockThreshold, 5*time.Minute, 24*time.Hour)...)
	return errs
}

func checkDuration(path string, value, min, max time.Duration) []FieldError {
	if value < min {
		return []FieldError{{Field: path, Message: fmt.Sprintf("must be >= %s, got %s", min, value)}}
	}
	if value > max {
		return []FieldError{{Field: path, Message: fmt.Sprintf("must be <= %s, got %s", max, value)}}
	}
	return nil
}

func checkInt(path string, value, min, max int) []FieldError {
	if value < min {
		return []FieldError{{Field: path, Message: fmt.Sprintf("must be >= %d, got %d", min, value)}}
	}
	if value > max {
		return []FieldError{{Field: path, Message: fmt.Sprintf("must be <= %d, got %d", max, value)}}
	}
	return nil
}

func checkInt32(path string, value, min, max int32) []FieldError {
	if value < min {
		return []FieldError{{Field: path, Message: fmt.Sprintf("must be >= %d, got %d", min, value)}}
	}
	if value > max {
		return []FieldError{{Field: path, Message: fmt.Sprintf("must be <= %d, got %d", max, value)}}
	}
	return nil
}

func FormatErrors(errs []FieldError) string {
	if len(errs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ")
}
