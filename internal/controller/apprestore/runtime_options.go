package apprestore

import (
	"time"

	. "github.com/softcdata/testudo-operator/internal/controller"
)

// RestoreRuntimeConfig centralizes tuning knobs for AppRestore runtime behavior.
// Defaults are intentionally aligned with existing behavior.
type RestoreRuntimeConfig struct {
	RestoreInProgressMaxWaitDefault time.Duration
	RestoreUnknownMaxWaitDefault    time.Duration
	ProgressCompleteGrace           time.Duration
	StartupGrace                    time.Duration
	MissingGrace                    time.Duration
	EmptyStatusGrace                time.Duration
	PodVolumeRestorePendingMaxWait  time.Duration
	RetryBackoff                    time.Duration
	AutoRetryLimit                  int
	AutoRetryLimitProgress          int
	AutoRetryLimitStartup           int
	AutoRetryLimitMissing           int
	AutoRetryLimitEmpty             int

	autoRetryLimitExplicit         bool
	autoRetryLimitProgressExplicit bool
	autoRetryLimitStartupExplicit  bool
	autoRetryLimitMissingExplicit  bool
	autoRetryLimitEmptyExplicit    bool
}

// RestoreRuntimeOption customizes RestoreRuntimeConfig.
type RestoreRuntimeOption func(*RestoreRuntimeConfig)

func defaultRestoreRuntimeConfig() RestoreRuntimeConfig {
	return RestoreRuntimeConfig{
		RestoreInProgressMaxWaitDefault: RestorePhaseInProgressMaxWait,
		RestoreUnknownMaxWaitDefault:    RestorePhaseUnknownMaxWait,
		ProgressCompleteGrace:           5 * time.Minute,
		StartupGrace:                    5 * time.Minute,
		MissingGrace:                    90 * time.Second,
		EmptyStatusGrace:                5 * time.Minute,
		PodVolumeRestorePendingMaxWait:  10 * time.Minute,
		RetryBackoff:                    15 * time.Second,
		AutoRetryLimit:                  1,
		AutoRetryLimitProgress:          1,
		AutoRetryLimitStartup:           1,
		AutoRetryLimitMissing:           2,
		AutoRetryLimitEmpty:             2,
	}
}

// NewRestoreRuntimeConfig creates config with defaults and applies options.
func NewRestoreRuntimeConfig(opts ...RestoreRuntimeOption) RestoreRuntimeConfig {
	cfg := defaultRestoreRuntimeConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}
	if cfg.AutoRetryLimit < 0 {
		cfg.AutoRetryLimit = 0
	}
	if cfg.autoRetryLimitExplicit {
		if !cfg.autoRetryLimitProgressExplicit {
			cfg.AutoRetryLimitProgress = cfg.AutoRetryLimit
		}
		if !cfg.autoRetryLimitStartupExplicit {
			cfg.AutoRetryLimitStartup = cfg.AutoRetryLimit
		}
		if !cfg.autoRetryLimitMissingExplicit {
			cfg.AutoRetryLimitMissing = cfg.AutoRetryLimit
		}
		if !cfg.autoRetryLimitEmptyExplicit {
			cfg.AutoRetryLimitEmpty = cfg.AutoRetryLimit
		}
	}
	if cfg.AutoRetryLimitProgress < 0 {
		cfg.AutoRetryLimitProgress = 0
	}
	if cfg.AutoRetryLimitStartup < 0 {
		cfg.AutoRetryLimitStartup = 0
	}
	if cfg.AutoRetryLimitMissing < 0 {
		cfg.AutoRetryLimitMissing = 0
	}
	if cfg.AutoRetryLimitEmpty < 0 {
		cfg.AutoRetryLimitEmpty = 0
	}
	if cfg.ProgressCompleteGrace <= 0 {
		cfg.ProgressCompleteGrace = defaultRestoreRuntimeConfig().ProgressCompleteGrace
	}
	if cfg.StartupGrace <= 0 {
		cfg.StartupGrace = defaultRestoreRuntimeConfig().StartupGrace
	}
	if cfg.MissingGrace <= 0 {
		cfg.MissingGrace = defaultRestoreRuntimeConfig().MissingGrace
	}
	if cfg.EmptyStatusGrace <= 0 {
		cfg.EmptyStatusGrace = defaultRestoreRuntimeConfig().EmptyStatusGrace
	}
	if cfg.PodVolumeRestorePendingMaxWait <= 0 {
		cfg.PodVolumeRestorePendingMaxWait = defaultRestoreRuntimeConfig().PodVolumeRestorePendingMaxWait
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = defaultRestoreRuntimeConfig().RetryBackoff
	}
	if cfg.RestoreInProgressMaxWaitDefault <= 0 {
		cfg.RestoreInProgressMaxWaitDefault = defaultRestoreRuntimeConfig().RestoreInProgressMaxWaitDefault
	}
	if cfg.RestoreUnknownMaxWaitDefault <= 0 {
		cfg.RestoreUnknownMaxWaitDefault = defaultRestoreRuntimeConfig().RestoreUnknownMaxWaitDefault
	}
	return cfg
}

func WithRestoreInProgressMaxWaitDefault(v time.Duration) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.RestoreInProgressMaxWaitDefault = v
	}
}

func WithRestoreUnknownMaxWaitDefault(v time.Duration) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.RestoreUnknownMaxWaitDefault = v
	}
}

func WithProgressCompleteGrace(v time.Duration) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.ProgressCompleteGrace = v
	}
}

func WithStartupGrace(v time.Duration) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.StartupGrace = v
	}
}

func WithMissingGrace(v time.Duration) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.MissingGrace = v
	}
}

func WithEmptyStatusGrace(v time.Duration) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.EmptyStatusGrace = v
	}
}

func WithPodVolumeRestorePendingMaxWait(v time.Duration) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.PodVolumeRestorePendingMaxWait = v
	}
}

func WithRetryBackoff(v time.Duration) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.RetryBackoff = v
	}
}

func WithAutoRetryLimit(v int) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.AutoRetryLimit = v
		cfg.autoRetryLimitExplicit = true
	}
}

func WithAutoRetryLimitProgress(v int) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.AutoRetryLimitProgress = v
		cfg.autoRetryLimitProgressExplicit = true
	}
}

func WithAutoRetryLimitStartup(v int) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.AutoRetryLimitStartup = v
		cfg.autoRetryLimitStartupExplicit = true
	}
}

func WithAutoRetryLimitMissing(v int) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.AutoRetryLimitMissing = v
		cfg.autoRetryLimitMissingExplicit = true
	}
}

func WithAutoRetryLimitEmpty(v int) RestoreRuntimeOption {
	return func(cfg *RestoreRuntimeConfig) {
		cfg.AutoRetryLimitEmpty = v
		cfg.autoRetryLimitEmptyExplicit = true
	}
}
