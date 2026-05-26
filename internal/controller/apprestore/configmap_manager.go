package apprestore

import (
	"context"
	"fmt"

	"github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// ConfigMapManager handles the lifecycle of ResourceModifier ConfigMaps.
type ConfigMapManager struct {
	Client client.Client
}

// NewConfigMapManager creates a new ConfigMapManager.
func NewConfigMapManager(client client.Client) *ConfigMapManager {
	return &ConfigMapManager{
		Client: client,
	}
}

// EnsureConfigMap creates or updates the ResourceModifier ConfigMap for the given AppRestore.
// It returns the name of the ConfigMap.
func (m *ConfigMapManager) EnsureConfigMap(ctx context.Context, appRestore *disasterv1.AppRestore) (string, error) {
	if len(appRestore.Spec.ResourceModifierRules) == 0 {
		return "", nil
	}

	cmName := m.generateConfigMapName(appRestore)

	// Serialize rules to YAML
	// Velero expects:
	// version: v1
	// resourceModifierRules: ...
	data := map[string]interface{}{
		"version":               "v1",
		"resourceModifierRules": appRestore.Spec.ResourceModifierRules,
	}

	yamlData, err := yaml.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal resource modifier rules: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: controller.VeleroNamespace,
			Labels: map[string]string{
				"apprestore.testudo.softcdata.com/uid":        string(appRestore.UID),
				"apprestore.testudo.softcdata.com/managed-by": "disaster-operator",
			},
		},
		Data: map[string]string{
			cmName: string(yamlData),
		},
	}
	cm.Labels, _ = EnsureCleanupLabels(cm.Labels, CleanupDescriptor{
		OwnerUID:     string(appRestore.UID),
		RelationCode: "finalizer.resourceModifierConfigMap",
		Strategy:     CleanupStrategyDelete,
	})

	// Create or Update
	existingCM := &corev1.ConfigMap{}
	err = m.Client.Get(ctx, types.NamespacedName{Name: cmName, Namespace: controller.VeleroNamespace}, existingCM)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := m.Client.Create(ctx, cm); err != nil {
				return "", fmt.Errorf("failed to create configmap: %w", err)
			}
			return cmName, nil
		}
		return "", fmt.Errorf("failed to get configmap: %w", err)
	}

	// Update if needed
	existingCM.Data = cm.Data
	existingCM.Labels = cm.Labels
	if err := m.Client.Update(ctx, existingCM); err != nil {
		return "", fmt.Errorf("failed to update configmap: %w", err)
	}

	return cmName, nil
}

// DeleteConfigMap deletes the ResourceModifier ConfigMap for the given AppRestore.
func (m *ConfigMapManager) DeleteConfigMap(ctx context.Context, appRestore *disasterv1.AppRestore) error {
	opts := []client.DeleteAllOfOption{
		client.InNamespace(controller.VeleroNamespace),
		client.MatchingLabels{
			"apprestore.testudo.softcdata.com/uid": string(appRestore.UID),
		},
	}

	if err := m.Client.DeleteAllOf(ctx, &corev1.ConfigMap{}, opts...); err != nil {
		return fmt.Errorf("failed to delete configmaps: %w", err)
	}

	return nil
}

func (m *ConfigMapManager) generateConfigMapName(appRestore *disasterv1.AppRestore) string {
	uid := string(appRestore.UID)
	shortUID := uid
	if len(uid) > 8 {
		shortUID = uid[:8]
	}
	return fmt.Sprintf("am-%s-%s", appRestore.Name, shortUID)
}
