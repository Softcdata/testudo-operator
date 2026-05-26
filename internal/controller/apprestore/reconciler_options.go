package apprestore

import (
	"github.com/softcdata/testudo-operator/internal/controller"
	"github.com/softcdata/testudo-operator/pkg/helper"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcilerOption customizes AppRestoreReconciler initialization.
type ReconcilerOption func(*AppRestoreReconciler)

// NewAppRestoreReconciler creates an AppRestoreReconciler with safe defaults.
func NewAppRestoreReconciler(
	cli client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	opts ...ReconcilerOption,
) *AppRestoreReconciler {
	r := &AppRestoreReconciler{
		Client:        cli,
		Scheme:        scheme,
		Recorder:      recorder,
		ClientFactory: &controller.DefaultClientFactory{},
	}
	if cli != nil {
		r.StatsHelper = helper.NewStatisticsHelper(cli)
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(r)
	}
	return r
}

// WithClientFactory overrides the default client factory.
func WithClientFactory(factory controller.ClientFactory) ReconcilerOption {
	return func(r *AppRestoreReconciler) {
		if factory != nil {
			r.ClientFactory = factory
		}
	}
}

// WithStatsHelper overrides the default statistics helper.
func WithStatsHelper(stats helper.StatisticsHelper) ReconcilerOption {
	return func(r *AppRestoreReconciler) {
		if stats != nil {
			r.StatsHelper = stats
		}
	}
}

// WithRestoreRuntime applies runtime options for restore convergence behavior.
func WithRestoreRuntime(opts ...RestoreRuntimeOption) ReconcilerOption {
	return func(r *AppRestoreReconciler) {
		if len(opts) == 0 {
			return
		}
		normalized := make([]RestoreRuntimeOption, 0, len(opts))
		for _, opt := range opts {
			if opt == nil {
				continue
			}
			normalized = append(normalized, opt)
		}
		r.restoreRuntimeOptions = normalized
	}
}
