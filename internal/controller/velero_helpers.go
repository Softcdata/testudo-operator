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

package controller

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// VeleroCRDsPath 是容器内 Velero CRDs 文件的路径
	VeleroCRDsPath = "./velero-crds.yaml"
	// VeleroCRDsPathEnv 允许通过环境变量覆盖 CRDs 文件路径
	VeleroCRDsPathEnv = "VELERO_CRDS_PATH"

	// ZombieLockThreshold 是判定 Helm 锁为僵尸锁的时间阈值
	// 应大于 Helm 的默认超时时间（通常为 5 分钟）
	ZombieLockThreshold = 10 * time.Minute

	// Helm Release Secret 的 Labels
	helmOwnerLabel  = "owner"
	helmOwnerValue  = "helm"
	helmNameLabel   = "name"
	helmStatusLabel = "status"

	// 表示僵尸锁的 Helm pending 状态
	helmStatusPendingInstall  = "pending-install"
	helmStatusPendingUpgrade  = "pending-upgrade"
	helmStatusPendingRollback = "pending-rollback"
)

func resolveVeleroCRDsPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(VeleroCRDsPathEnv)); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("%s=%q is set but file not found", VeleroCRDsPathEnv, p)
	}

	// Candidate paths (try in order).
	candidates := []string{
		VeleroCRDsPath,
		"./dist/velero-crds.yaml",
		"/app/velero-crds.yaml",
	}

	// Also try resolving relative to this source file location (works in tests regardless of CWD).
	if _, file, _, ok := runtime.Caller(0); ok && file != "" {
		base := filepath.Dir(file) // .../internal/controller
		candidates = append(candidates,
			filepath.Join(base, "..", "..", "velero-crds.yaml"),
			filepath.Join(base, "..", "..", "dist", "velero-crds.yaml"),
		)
	}

	for _, p := range candidates {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("velero crds file not found (tried: %v). Set %s to override", candidates, VeleroCRDsPathEnv)
}

// EnsureVeleroCRDs 从嵌入的文件读取 Velero CRDs 并应用到目标集群。
// 这允许我们跳过通常用于安装 CRDs 的 Helm Hook Job，从而避免镜像拉取问题。
func EnsureVeleroCRDs(ctx context.Context, cli client.Client) error {
	logger := logf.FromContext(ctx).WithName("velero-crds")
	logger.Info("Starting Velero CRDs installation")

	// 读取 CRDs 文件
	path, err := resolveVeleroCRDsPath()
	if err != nil {
		logger.Error(err, "Failed to resolve Velero CRDs path")
		return err
	}
	logger.V(1).Info("Reading CRDs file", "path", path)
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Error(err, "Failed to read Velero CRDs file", "path", path)
		return err
	}
	logger.V(1).Info("CRDs file read successfully", "size", len(data))

	// 解析多文档 YAML
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	crdsApplied := 0
	crdsUpdated := 0

	for {
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			logger.Error(err, "Failed to read YAML document")
			return err
		}

		// 跳过空文档
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}

		// 解码 CRD
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(doc, crd); err != nil {
			logger.Error(err, "Failed to unmarshal CRD")
			return err
		}

		// 跳过非 CRD 资源（如空文档或其他资源）
		if crd.Kind != "CustomResourceDefinition" || crd.Name == "" {
			continue
		}

		// 应用 CRD
		created, err := applyVeleroCRD(ctx, cli, crd)
		if err != nil {
			logger.Error(err, "Failed to apply CRD", "name", crd.Name)
			return err
		}
		if created {
			crdsApplied++
			logger.V(1).Info("Created CRD", "name", crd.Name)
		} else {
			crdsUpdated++
			logger.V(1).Info("Updated CRD", "name", crd.Name)
		}
	}

	logger.Info("Velero CRDs installation completed", "created", crdsApplied, "updated", crdsUpdated)
	return nil
}

// applyVeleroCRD 使用 Create-or-Update 模式将单个 CRD 应用到集群
// 返回值: created (是否为新创建), error
func applyVeleroCRD(ctx context.Context, cli client.Client, crd *apiextensionsv1.CustomResourceDefinition) (bool, error) {
	existing := &apiextensionsv1.CustomResourceDefinition{}
	err := cli.Get(ctx, types.NamespacedName{Name: crd.Name}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// CRD 不存在，创建它
			return true, cli.Create(ctx, crd)
		}
		return false, err
	}

	// CRD 已存在，更新它
	// 保留 resourceVersion 以用于更新
	crd.ResourceVersion = existing.ResourceVersion
	return false, cli.Update(ctx, crd)
}

// CleanupZombieHelmLocks 检查并移除阻止新安装的僵尸 Helm Release Secrets。
// 这些"僵尸锁"发生在之前的 helm install/upgrade 被异常中断时。
func CleanupZombieHelmLocks(ctx context.Context, cli client.Client, namespace string, releaseName string) error {
	logger := logf.FromContext(ctx).WithName("helm-lock-cleanup")
	logger.Info("Checking for zombie Helm locks", "namespace", namespace, "release", releaseName)

	// 列出目标命名空间中带有 Helm owner label 的 Secrets
	secretList := &corev1.SecretList{}
	if err := cli.List(ctx, secretList,
		client.InNamespace(namespace),
		client.MatchingLabels{
			helmOwnerLabel: helmOwnerValue,
			helmNameLabel:  releaseName,
		},
	); err != nil {
		logger.Error(err, "Failed to list Helm secrets")
		return err
	}

	logger.V(1).Info("Found Helm release secrets", "count", len(secretList.Items))

	deletedCount := 0
	for _, secret := range secretList.Items {
		status := secret.Labels[helmStatusLabel]

		// 只考虑 pending 状态
		if !isPendingStatus(status) {
			logger.V(1).Info("Skipping non-pending secret", "secret", secret.Name, "status", status)
			continue
		}

		// 检查这个锁是否足够老以被认为是僵尸锁
		age := time.Since(secret.CreationTimestamp.Time)
		if age < ZombieLockThreshold {
			logger.Info("Found pending Helm release but it's recent, skipping",
				"secret", secret.Name,
				"status", status,
				"age", age.String(),
				"threshold", ZombieLockThreshold.String())
			continue
		}

		// 这是一个僵尸锁，删除它
		logger.Info("Deleting zombie Helm lock",
			"secret", secret.Name,
			"status", status,
			"age", age.String())

		if err := cli.Delete(ctx, &secret); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to delete zombie Helm lock", "secret", secret.Name)
				// 继续处理其他 secrets，不中断整个操作
			}
		} else {
			deletedCount++
		}
	}

	if deletedCount > 0 {
		logger.Info("Zombie Helm locks cleanup completed", "deleted", deletedCount)
	} else {
		logger.V(1).Info("No zombie Helm locks found")
	}

	return nil
}

// isPendingStatus 检查 Helm Release 状态是否表示 pending/stuck 状态
func isPendingStatus(status string) bool {
	switch status {
	case helmStatusPendingInstall, helmStatusPendingUpgrade, helmStatusPendingRollback:
		return true
	default:
		return false
	}
}
