package controller

import (
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	platformlicense "github.com/softcdata/testudo-operator/pkg/license"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type LicenseStatusRunnable struct {
	Client    client.Client
	Namespace string
	CAPath    string
	Verifier  *platformlicense.Verifier
	Interval  time.Duration
	Log       logr.Logger
}

func NewLicenseStatusRunnable(cli client.Client, namespace, caPath string, verifier *platformlicense.Verifier) *LicenseStatusRunnable {
	return &LicenseStatusRunnable{
		Client:    cli,
		Namespace: namespace,
		CAPath:    strings.TrimSpace(caPath),
		Verifier:  verifier,
		Interval:  1 * time.Minute,
		Log:       logf.Log.WithName("license-status"),
	}
}

func (r *LicenseStatusRunnable) Start(ctx context.Context) error {
	if r == nil || r.Client == nil {
		return nil
	}
	if r.Interval <= 0 {
		r.Interval = 1 * time.Minute
	}
	r.reconcile(ctx)

	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

func (r *LicenseStatusRunnable) reconcile(ctx context.Context) {
	namespace := strings.TrimSpace(r.Namespace)
	if namespace == "" {
		namespace = platformlicense.DefaultLicenseNamespace
	}
	store := platformlicense.KubernetesStore{Client: r.Client, Namespace: namespace, CAPath: r.CAPath}
	if _, _, err := store.EnsureGateState(ctx); err != nil {
		r.Log.Error(err, "unable to ensure license gate state")
	}
	entitlement := store.Evaluate(ctx, r.Verifier)
	count, err := nonDeletingClusterCount(ctx, r.Client)
	if err != nil {
		r.Log.Error(err, "unable to count clusters for license status")
		return
	}
	if err := store.UpsertStatus(ctx, entitlement, count); err != nil {
		r.Log.Error(err, "unable to update license status configmap")
	}
}
