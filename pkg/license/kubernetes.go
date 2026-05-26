package license

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type KubernetesStore struct {
	Client    client.Client
	Reader    client.Reader
	Namespace string
	CAPath    string
	CABundle  []byte
	Now       func() time.Time
}

func (s KubernetesStore) reader() client.Reader {
	if s.Reader != nil {
		return s.Reader
	}
	return s.Client
}

func (s KubernetesStore) EffectiveNamespace() string {
	if strings.TrimSpace(s.Namespace) != "" {
		return strings.TrimSpace(s.Namespace)
	}
	return DefaultLicenseNamespace
}

func (s KubernetesStore) EffectiveCAPath() string {
	if strings.TrimSpace(s.CAPath) != "" {
		return strings.TrimSpace(s.CAPath)
	}
	return DefaultServiceAccountCAPath
}

func (s KubernetesStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s KubernetesStore) ReadLicense(ctx context.Context) ([]byte, bool, error) {
	reader := s.reader()
	if reader == nil {
		return nil, false, fmt.Errorf("kubernetes client is nil")
	}
	secret := &corev1.Secret{}
	err := reader.Get(ctx, types.NamespacedName{
		Namespace: s.EffectiveNamespace(),
		Name:      LicenseSecretName,
	}, secret)
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw := secret.Data[LicenseSecretDataKey]
	if len(raw) == 0 {
		return nil, true, fmt.Errorf("license secret %s/%s missing data key %q", secret.Namespace, secret.Name, LicenseSecretDataKey)
	}
	return raw, true, nil
}

func (s KubernetesStore) Evaluate(ctx context.Context, verifier *Verifier) Entitlement {
	raw, exists, err := s.ReadLicense(ctx)
	if err != nil {
		if exists {
			return fallbackEntitlement(StateMalformed, ReasonLicenseInvalid, err.Error())
		}
		return fallbackEntitlement(StateUnknown, ReasonLicenseInvalid, err.Error())
	}
	if !exists {
		return FreeEntitlement("NoLicense", "license secret is not installed")
	}

	if verifier == nil {
		verifier = NewDefaultVerifier()
	}
	content := verifier.VerifyContent(raw)
	if !content.IsActive() {
		return content
	}

	fingerprint, err := s.Fingerprint(ctx)
	if err != nil {
		return fallbackEntitlement(StateUnknown, ReasonLicenseEnvironmentInvalid, fmt.Sprintf("compute deployment fingerprint: %v", err))
	}
	return verifier.Verify(raw, fingerprint)
}

func (s KubernetesStore) Fingerprint(ctx context.Context) (string, error) {
	reader := s.reader()
	if reader == nil {
		return "", fmt.Errorf("kubernetes client is nil")
	}

	kubeSystem := &corev1.Namespace{}
	if err := reader.Get(ctx, types.NamespacedName{Name: metav1.NamespaceSystem}, kubeSystem); err != nil {
		return "", fmt.Errorf("get namespace %s: %w", metav1.NamespaceSystem, err)
	}

	platformNamespace := &corev1.Namespace{}
	if err := reader.Get(ctx, types.NamespacedName{Name: s.EffectiveNamespace()}, platformNamespace); err != nil {
		return "", fmt.Errorf("get namespace %s: %w", s.EffectiveNamespace(), err)
	}

	caBytes := s.CABundle
	if len(caBytes) == 0 {
		var err error
		caBytes, err = os.ReadFile(s.EffectiveCAPath())
		if err != nil {
			return "", fmt.Errorf("read API server CA: %w", err)
		}
	}
	caHash, err := HashAPIServerCABundle(caBytes)
	if err != nil {
		return "", fmt.Errorf("hash API server CA: %w", err)
	}

	installID, err := s.EnsureInstallID(ctx)
	if err != nil {
		return "", err
	}

	return ComputeK8SV1Fingerprint(FingerprintInputs{
		KubeSystemUID:        string(kubeSystem.UID),
		PlatformNamespaceUID: string(platformNamespace.UID),
		APIServerCASHA256:    caHash,
		InstallID:            installID,
	})
}

func (s KubernetesStore) EnsureInstallID(ctx context.Context) (string, error) {
	if s.Client == nil {
		return "", fmt.Errorf("kubernetes client is nil")
	}
	namespace := s.EffectiveNamespace()
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: namespace, Name: InstallIDSecretName}
	reader := s.reader()
	if reader == nil {
		return "", fmt.Errorf("kubernetes client is nil")
	}
	if err := reader.Get(ctx, key, secret); err == nil {
		installID := strings.TrimSpace(string(secret.Data[InstallIDSecretDataKey]))
		if installID == "" {
			return "", fmt.Errorf("install-id secret %s/%s missing data key %q", namespace, InstallIDSecretName, InstallIDSecretDataKey)
		}
		return installID, nil
	} else if !apierrors.IsNotFound(err) {
		return "", err
	}

	installID := uuid.NewString()
	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      InstallIDSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"testudo.softcdata.com/license": "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			InstallIDSecretDataKey: []byte(installID),
		},
	}
	if err := s.Client.Create(ctx, secret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return s.EnsureInstallID(ctx)
		}
		return "", err
	}
	return installID, nil
}

func (s KubernetesStore) EnsureGateState(ctx context.Context) (time.Time, bool, error) {
	reader := s.reader()
	if reader == nil {
		return time.Time{}, false, fmt.Errorf("kubernetes client is nil")
	}
	namespace := s.EffectiveNamespace()
	key := types.NamespacedName{Namespace: namespace, Name: GateStateConfigMapName}
	configMap := &corev1.ConfigMap{}
	if err := reader.Get(ctx, key, configMap); err == nil {
		enabledAt, err := parseUTCRFC3339Seconds(strings.TrimSpace(configMap.Data[GateStateEnabledAtKey]))
		if err != nil {
			return time.Time{}, false, fmt.Errorf("invalid %s/%s %s: %w", namespace, GateStateConfigMapName, GateStateEnabledAtKey, err)
		}
		return enabledAt, false, nil
	} else if !apierrors.IsNotFound(err) {
		return time.Time{}, false, err
	}

	enabledAt := s.now().Truncate(time.Second)
	configMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GateStateConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"testudo.softcdata.com/license": "true",
			},
		},
		Data: map[string]string{
			GateStateEnabledAtKey: enabledAt.Format(time.RFC3339),
		},
	}
	if err := s.Client.Create(ctx, configMap); err != nil {
		if apierrors.IsAlreadyExists(err) {
			enabledAt, _, getErr := s.EnsureGateState(ctx)
			return enabledAt, false, getErr
		}
		return time.Time{}, false, err
	}
	return enabledAt, true, nil
}

func (s KubernetesStore) UpsertStatus(ctx context.Context, entitlement Entitlement, clusterCount int) error {
	if s.Client == nil {
		return fmt.Errorf("kubernetes client is nil")
	}
	namespace := s.EffectiveNamespace()
	key := types.NamespacedName{Namespace: namespace, Name: StatusConfigMapName}
	data := map[string]string{
		"state":              string(entitlement.State),
		"edition":            entitlement.Edition,
		"licenseId":          entitlement.LicenseID,
		"customer":           entitlement.CustomerName(),
		"expiresAt":          "",
		"maxClusters":        strconv.Itoa(entitlement.ClusterLimit()),
		"clusterCount":       strconv.Itoa(clusterCount),
		"fingerprintMatched": strconv.FormatBool(entitlement.FingerprintMatched),
		"reason":             entitlement.Reason,
		"message":            entitlement.Message,
		"lastCheckedAt":      s.now().Truncate(time.Second).Format(time.RFC3339),
	}
	if entitlement.ExpiresAt != nil {
		data["expiresAt"] = entitlement.ExpiresAt.UTC().Format(time.RFC3339)
	}

	configMap := &corev1.ConfigMap{}
	reader := s.reader()
	if reader == nil {
		return fmt.Errorf("kubernetes client is nil")
	}
	if err := reader.Get(ctx, key, configMap); err == nil {
		configMap.Data = data
		if configMap.Labels == nil {
			configMap.Labels = map[string]string{}
		}
		configMap.Labels["testudo.softcdata.com/license-status"] = "true"
		return s.Client.Update(ctx, configMap)
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	configMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StatusConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"testudo.softcdata.com/license-status": "true",
			},
		},
		Data: data,
	}
	if err := s.Client.Create(ctx, configMap); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return s.UpsertStatus(ctx, entitlement, clusterCount)
		}
		return err
	}
	return nil
}

func BuildLicenseSecret(namespace string, raw []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LicenseSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"testudo.softcdata.com/license": "true",
			},
		},
		Type: corev1.SecretType(LicenseSecretType),
		Data: map[string][]byte{
			LicenseSecretDataKey: raw,
		},
	}
}
