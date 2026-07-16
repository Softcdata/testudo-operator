package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultTrafficlessImage               = "busybox:1.36"
	LegacyDefaultTrafficlessImage         = "busybox:latest"
	DefaultTrafficlessRuntimeSource       = "default"
	ExplicitTrafficlessRuntimeSource      = "explicit"
	TargetClusterTrafficlessRuntimeSource = "targetClusterRegistry"
)

// TrafficlessRuntime describes the platform runtime image used by FSB trafficless pods.
type TrafficlessRuntime struct {
	Image          string
	Command        []string
	PullSecretName string
	Source         string
	TargetCluster  string
}

// ValidateTrafficlessVeleroRuntime verifies the runtime components required by
// file-system backup restores. Callers decide how an unavailable runtime maps to
// their own lifecycle reason; this helper only returns observed facts.
func ValidateTrafficlessVeleroRuntime(ctx context.Context, cli client.Client) error {
	if cli == nil {
		return fmt.Errorf("target Kubernetes client is nil")
	}

	deployment := &appsv1.Deployment{}
	if err := cli.Get(ctx, types.NamespacedName{Name: "velero", Namespace: VeleroNamespace}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("deployment %s/%s is not found", VeleroNamespace, "velero")
		}
		return fmt.Errorf("get deployment %s/%s: %w", VeleroNamespace, "velero", err)
	}
	if deployment.Status.ReadyReplicas < 1 || deployment.Status.AvailableReplicas < 1 {
		return fmt.Errorf(
			"deployment %s/%s ready=%d available=%d unavailable=%d",
			deployment.Namespace,
			deployment.Name,
			deployment.Status.ReadyReplicas,
			deployment.Status.AvailableReplicas,
			deployment.Status.UnavailableReplicas,
		)
	}

	nodeAgent := &appsv1.DaemonSet{}
	if err := cli.Get(ctx, types.NamespacedName{Name: "node-agent", Namespace: VeleroNamespace}, nodeAgent); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("daemonset %s/%s is not found", VeleroNamespace, "node-agent")
		}
		return fmt.Errorf("get daemonset %s/%s: %w", VeleroNamespace, "node-agent", err)
	}
	if nodeAgent.Status.DesiredNumberScheduled < 1 || nodeAgent.Status.NumberReady < nodeAgent.Status.DesiredNumberScheduled {
		return fmt.Errorf(
			"daemonset %s/%s ready=%d desired=%d",
			nodeAgent.Namespace,
			nodeAgent.Name,
			nodeAgent.Status.NumberReady,
			nodeAgent.Status.DesiredNumberScheduled,
		)
	}

	return nil
}

func ResolveTrafficlessRuntime(
	ctx context.Context,
	managementClient client.Client,
	targetCluster string,
	cfg *disasterv1.TrafficlessConfig,
) (TrafficlessRuntime, *disasterv1.Cluster, error) {
	runtime := TrafficlessRuntime{
		Image:         DefaultTrafficlessImage,
		Command:       ResolveTrafficlessCommand(cfg),
		Source:        DefaultTrafficlessRuntimeSource,
		TargetCluster: strings.TrimSpace(targetCluster),
	}

	explicitImage := ""
	if cfg != nil {
		explicitImage = strings.TrimSpace(cfg.Image)
	}
	if explicitImage != "" && !IsDefaultTrafficlessImage(explicitImage) {
		runtime.Image = explicitImage
		runtime.Source = ExplicitTrafficlessRuntimeSource
	}

	if managementClient == nil || runtime.TargetCluster == "" {
		return runtime, nil, nil
	}

	cluster := &disasterv1.Cluster{}
	if err := managementClient.Get(ctx, types.NamespacedName{Name: runtime.TargetCluster}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return runtime, nil, nil
		}
		return TrafficlessRuntime{}, nil, fmt.Errorf("get target cluster %s for trafficless runtime: %w", runtime.TargetCluster, err)
	}

	if IsDefaultTrafficlessImage(explicitImage) && cluster.Spec.VeleroInstall != nil {
		if registry := normalizeVeleroImageRegistry(cluster.Spec.VeleroInstall.ImageRegistry); registry != "" {
			runtime.Image = joinRegistryRepository(registry, DefaultTrafficlessImage)
			runtime.Source = TargetClusterTrafficlessRuntimeSource
		}
	}

	if cluster.Spec.VeleroInstall != nil &&
		cluster.Spec.VeleroInstall.RegistryCredentialSecretRef != nil &&
		strings.TrimSpace(cluster.Spec.VeleroInstall.RegistryCredentialSecretRef.Name) != "" {
		runtime.PullSecretName = veleroRegistryTargetSecretName(cluster.Name)
	}

	return runtime, cluster, nil
}

func ResolveTrafficlessCommand(cfg *disasterv1.TrafficlessConfig) []string {
	if cfg != nil && len(cfg.Command) > 0 {
		return append([]string(nil), cfg.Command...)
	}
	return []string{"sleep", "3600"}
}

func IsDefaultTrafficlessImage(image string) bool {
	switch strings.TrimSpace(image) {
	case "", DefaultTrafficlessImage, LegacyDefaultTrafficlessImage:
		return true
	default:
		return false
	}
}

func TrafficlessRestoreNamespaces(sourceNamespaces []string, namespaceMapping map[string]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(sourceNamespaces)+len(namespaceMapping))
	appendNS := func(ns string) {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			return
		}
		if _, ok := seen[ns]; ok {
			return
		}
		seen[ns] = struct{}{}
		out = append(out, ns)
	}

	for _, sourceNS := range sourceNamespaces {
		sourceNS = strings.TrimSpace(sourceNS)
		if sourceNS == "" {
			continue
		}
		if mapped := strings.TrimSpace(namespaceMapping[sourceNS]); mapped != "" {
			appendNS(mapped)
			continue
		}
		appendNS(sourceNS)
	}
	for _, mapped := range namespaceMapping {
		appendNS(mapped)
	}
	return out
}

func TrafficlessImagePullSecretsPatch(secretName string) (disasterv1.JSONPatch, bool) {
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		return disasterv1.JSONPatch{}, false
	}
	value, err := json.Marshal([]corev1.LocalObjectReference{{Name: secretName}})
	if err != nil {
		return disasterv1.JSONPatch{}, false
	}
	return disasterv1.JSONPatch{
		Operation: "add",
		Path:      "/spec/imagePullSecrets",
		Value:     string(value),
	}, true
}

func SyncTrafficlessRegistryPullSecret(
	ctx context.Context,
	managementClient client.Client,
	targetClient client.Client,
	cluster *disasterv1.Cluster,
	namespaces []string,
) (string, error) {
	if managementClient == nil || targetClient == nil || cluster == nil ||
		cluster.Spec.VeleroInstall == nil ||
		cluster.Spec.VeleroInstall.RegistryCredentialSecretRef == nil ||
		strings.TrimSpace(cluster.Spec.VeleroInstall.RegistryCredentialSecretRef.Name) == "" {
		return "", nil
	}

	sourceSecretName := strings.TrimSpace(cluster.Spec.VeleroInstall.RegistryCredentialSecretRef.Name)
	sourceSecret := &corev1.Secret{}
	if err := managementClient.Get(ctx, types.NamespacedName{Name: sourceSecretName, Namespace: ManagementNamespace()}, sourceSecret); err != nil {
		return "", fmt.Errorf("get management-plane trafficless registry secret %s/%s: %w", ManagementNamespace(), sourceSecretName, err)
	}
	if sourceSecret.Type != corev1.SecretTypeDockerConfigJson {
		return "", fmt.Errorf("management-plane trafficless registry secret %s/%s is not dockerconfigjson", sourceSecret.Namespace, sourceSecret.Name)
	}
	dockerConfig, ok := sourceSecret.Data[corev1.DockerConfigJsonKey]
	if !ok || len(dockerConfig) == 0 {
		return "", fmt.Errorf("management-plane trafficless registry secret %s/%s is missing %s", sourceSecret.Namespace, sourceSecret.Name, corev1.DockerConfigJsonKey)
	}

	targetSecretName := veleroRegistryTargetSecretName(cluster.Name)
	for _, namespace := range TrafficlessRestoreNamespaces(namespaces, nil) {
		if err := ensureTrafficlessNamespace(ctx, targetClient, namespace); err != nil {
			return "", fmt.Errorf("ensure trafficless restore namespace %s: %w", namespace, err)
		}
		if err := upsertTrafficlessRegistrySecret(ctx, targetClient, namespace, targetSecretName, dockerConfig); err != nil {
			return "", fmt.Errorf("sync trafficless registry secret to %s/%s: %w", namespace, targetSecretName, err)
		}
	}
	return targetSecretName, nil
}

func ensureTrafficlessNamespace(ctx context.Context, cli client.Client, namespace string) error {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil
	}
	ns := &corev1.Namespace{}
	if err := cli.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			return cli.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		}
		return err
	}
	return nil
}

func upsertTrafficlessRegistrySecret(ctx context.Context, cli client.Client, namespace, name string, dockerConfig []byte) error {
	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := cli.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return cli.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: append([]byte(nil), dockerConfig...),
				},
			})
		}
		return err
	}

	secret.Type = corev1.SecretTypeDockerConfigJson
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[corev1.DockerConfigJsonKey] = append([]byte(nil), dockerConfig...)
	return cli.Update(ctx, secret)
}
