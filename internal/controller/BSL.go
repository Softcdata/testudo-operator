package controller

import (
	"bytes"
	"context"
	"fmt"
	"strconv"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type BSL interface {
	ApplyStorageRepositoryForCluster(ctx context.Context, source client.Reader, target client.Client, cluster *disasterv1.Cluster, sr *disasterv1.StorageRepository, bslName, prefix string) error
}
type DefaultBSL struct{}

// ApplyStorageRepositoryForCluster refreshes the Velero BSL mapping for one
// physical target cluster. The target cluster owns the optional endpoint override.
func (d *DefaultBSL) ApplyStorageRepositoryForCluster(ctx context.Context, source client.Reader, target client.Client, cluster *disasterv1.Cluster, sr *disasterv1.StorageRepository, bslName, prefix string) error {
	if cluster == nil {
		return fmt.Errorf("target cluster context is required to apply BackupStorageLocation")
	}
	logger := logf.FromContext(ctx)

	settings, err := resolveStorageRuntimeSettings(ctx, source, sr)
	if err != nil {
		logger.Error(err, "unable to resolve StorageRepository runtime settings")
		return err
	}
	settings.Endpoint, settings.EndpointSource, err = resolveBSLEndpoint(cluster, settings.Endpoint)
	if err != nil {
		logger.Error(err, "unable to resolve BSL endpoint", "cluster", cluster.Name)
		return err
	}
	logger.Info("resolved BSL endpoint", "cluster", cluster.Name, "endpointSource", settings.EndpointSource, "s3Url", settings.Endpoint)

	err = d.updateSecret(ctx, target, sr)
	if err != nil {
		logger.Error(err, "unable to update StorageRepository credentials")
		return err
	}

	err = d.updateBackupStorageLocation(ctx, target, bslName, prefix, sr, settings)
	if err != nil {
		logger.Error(err, "unable to update StorageRepository credentials")
		return err
	}
	return nil
}
func (r *DefaultBSL) updateSecret(ctx context.Context, cli client.Client, sr *disasterv1.StorageRepository) error {
	logger := logf.FromContext(ctx)
	secret := &corev1.Secret{}
	err := cli.Get(ctx, types.NamespacedName{Name: VeleroCredentialSecretName, Namespace: VeleroNamespace}, secret)
	if apierrors.IsNotFound(err) {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      VeleroCredentialSecretName,
				Namespace: VeleroNamespace,
				Labels: map[string]string{
					"testudo.softcdata.com/storage-repository": sr.Name,
				},
			},
			Data: map[string][]byte{
				sr.Name: []byte(fmt.Sprintf(VeleroCredentialTemplate, sr.Spec.AccessKey, sr.Spec.SecretKey)),
			},
		}
		err = cli.Create(ctx, secret)
		if err != nil {
			logger.Error(err, "unable to create Secret")
			return err
		}
		return nil
	}
	if err != nil {
		logger.Error(err, "unable to fetch Secret")
		return err
	}
	secret.Data[sr.Name] = []byte(fmt.Sprintf(VeleroCredentialTemplate, sr.Spec.AccessKey, sr.Spec.SecretKey))
	err = cli.Update(ctx, secret)
	if err != nil {
		logger.Error(err, "unable to update Secret")
		return err
	}
	return nil
}

func (d *DefaultBSL) updateBackupStorageLocation(ctx context.Context, cli client.Client, bslName, prefix string, sr *disasterv1.StorageRepository, settings StorageRuntimeSettings) error {
	logger := logf.FromContext(ctx)
	bsl := &velerov1.BackupStorageLocation{}
	err := cli.Get(ctx, types.NamespacedName{Name: bslName, Namespace: VeleroNamespace}, bsl)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = d.createBackupStorageLocation(ctx, cli, bslName, prefix, sr, settings)
			if err != nil {
				logger.Error(err, "unable to create velero BackupStorageLocation")
				return err
			}
			return nil
		}
		logger.Error(err, "get velero BackupStorageLocation error")
		return err
	}
	// update bsl
	needUpdate := false
	if bsl.Spec.Config == nil {
		bsl.Spec.Config = make(map[string]string)
		needUpdate = true
	}
	if bsl.Spec.StorageType.ObjectStorage == nil {
		bsl.Spec.StorageType.ObjectStorage = &velerov1.ObjectStorageLocation{}
		needUpdate = true
	}
	if bsl.Spec.StorageType.ObjectStorage.Bucket != sr.Spec.Bucket {
		bsl.Spec.StorageType.ObjectStorage.Bucket = sr.Spec.Bucket
		needUpdate = true
	}

	if bsl.Spec.StorageType.ObjectStorage.Prefix != prefix {
		bsl.Spec.StorageType.ObjectStorage.Prefix = prefix
		needUpdate = true
	}

	if bsl.Spec.Config["region"] != sr.Spec.Region {
		bsl.Spec.Config["region"] = sr.Spec.Region
		needUpdate = true
	}
	if bsl.Spec.Config["s3Url"] != settings.Endpoint {
		bsl.Spec.Config["s3Url"] = settings.Endpoint
		needUpdate = true
	}
	desiredPathStyle := strconv.FormatBool(settings.UsePathStyle)
	if bsl.Spec.Config["s3ForcePathStyle"] != desiredPathStyle {
		bsl.Spec.Config["s3ForcePathStyle"] = desiredPathStyle
		needUpdate = true
	}
	if !bytes.Equal(bsl.Spec.StorageType.ObjectStorage.CACert, settings.CACert) {
		bsl.Spec.StorageType.ObjectStorage.CACert = append([]byte(nil), settings.CACert...)
		needUpdate = true
	}
	// bsl.Spec.Credential.Key = sr.Name
	// bsl.Spec.AccessMode = velerov1.BackupStorageLocationAccessModeReadWrite
	if needUpdate {
		err = cli.Update(ctx, bsl)
		if err != nil {
			logger.Error(err, "unable to update velero BackupStorageLocation")
			return err
		}
		return nil
	}

	if bsl.Status.Phase == velerov1.BackupStorageLocationPhaseAvailable {
		sr.Status.Status = disasterv1.StorageRepositoryStatusAvailable
		return nil
	}
	sr.Status.Status = disasterv1.StorageRepositoryStatusUnavailable
	return fmt.Errorf("BackupStorageLocation %s is in Unavailable status", bslName)
}

func (d *DefaultBSL) createBackupStorageLocation(ctx context.Context, cli client.Client, bslName, prefix string, sr *disasterv1.StorageRepository, settings StorageRuntimeSettings) error {
	logger := logf.FromContext(ctx)
	bsl := &velerov1.BackupStorageLocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bslName,
			Namespace: VeleroNamespace,
			Labels: map[string]string{
				"testudo.softcdata.com/storage-repository": sr.Name,
			},
		},
		Spec: velerov1.BackupStorageLocationSpec{
			Provider: "aws", // s3
			Config:   desiredBSLConfig(settings),
			StorageType: velerov1.StorageType{
				ObjectStorage: &velerov1.ObjectStorageLocation{
					Bucket: sr.Spec.Bucket,
					Prefix: prefix,
					CACert: append([]byte(nil), settings.CACert...),
				},
			},
			Credential: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: VeleroCredentialSecretName,
				},
				Key: sr.Name,
			},
			AccessMode: velerov1.BackupStorageLocationAccessModeReadWrite,
		},
	}
	err := cli.Create(ctx, bsl)
	if err != nil {
		logger.Error(err, "unable to create BackupStorageLocation")
		return err
	}

	// BSL created successfully, but it needs time to become Available
	// Return an error to trigger requeue and wait for BSL to be validated by Velero
	sr.Status.Status = disasterv1.StorageRepositoryStatusUnavailable
	return fmt.Errorf("BackupStorageLocation %s is in Unavailable status", bslName)
}
