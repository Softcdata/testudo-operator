package disasteroperation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

// executeCheckReplicas 检查目标集群副本数是否已达到期望值
func (r *DisasterOperationReconciler) executeCheckReplicas(ctx context.Context, instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation) (bool, error) {
	waitUntilReady := resolveCheckReplicasWaitUntilReady(instance, operation)

	// 1. 获取 DisasterConfig
	config := &disasterv1.DisasterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.Config}, config); err != nil {
		return false, fmt.Errorf("failed to get config %s: %w", instance.Spec.Config, err)
	}

	// 2. 获取目标集群 Client
	targetClusterName := instance.Status.SecondaryCluster
	if targetClusterName == "" {
		targetClusterName = config.Spec.TargetCluster
	}

	remoteClient, err := r.getClusterClient(ctx, targetClusterName)
	if err != nil {
		return false, err
	}

	// 3. 获取期望副本数 Map
	replicasMap := make(map[string]int32)
	if instance.Status.ResourceSyncName != "" {
		cmName := fmt.Sprintf("replicas-%s", instance.Status.ResourceSyncName)
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: cmName}, cm); err == nil {
			if data, ok := cm.Data["replicas"]; ok {
				var storedMap map[string]int32
				if err := json.Unmarshal([]byte(data), &storedMap); err == nil {
					replicasMap = storedMap
				}
			}
		}
	}

	allReady := true
	firstBlocker := ""
	setFirstBlocker := func(msg string) {
		if strings.TrimSpace(msg) == "" || firstBlocker != "" {
			return
		}
		firstBlocker = strings.TrimSpace(msg)
	}
	knownStorageClasses := map[string]struct{}{}
	storageClassLoaded := false
	for _, ns := range instance.Spec.Namespaces {
		namespaceHasWorkload := false
		namespaceHasExpectedReplicas := false

		// 检查 Deployments
		deployList := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployList, client.InNamespace(ns)); err != nil {
			return false, err
		}
		for _, deploy := range deployList.Items {
			namespaceHasWorkload = true

			// 确定期望副本数
			var expectedReplicas int32 = -1
			key := fmt.Sprintf("%s/deployments/%s", ns, deploy.Name)

			if val, ok := replicasMap[key]; ok {
				expectedReplicas = val
			} else if val, ok := deploy.Annotations["testudo.softcdata.com/original-replicas"]; ok {
				var r int32
				if _, err := fmt.Sscanf(val, "%d", &r); err == nil {
					expectedReplicas = r
				}
			}

			// 如果找到了期望副本数
			if expectedReplicas != -1 {
				namespaceHasExpectedReplicas = true

				// 1. 检查配置是否下发 (Spec.Replicas)
				if deploy.Spec.Replicas == nil || *deploy.Spec.Replicas != expectedReplicas {
					r.Log.Info("Waiting for Deployment replicas update",
						"namespace", ns,
						"name", deploy.Name,
						"current", deploy.Spec.Replicas,
						"expected", expectedReplicas)
					allReady = false
					setFirstBlocker(fmt.Sprintf(
						"Deployment %s/%s spec.replicas=%s expected=%d",
						ns,
						deploy.Name,
						int32PtrString(deploy.Spec.Replicas),
						expectedReplicas,
					))
				}

				// 2. 如果配置要求等待就绪，则检查 Status.ReadyReplicas
				if waitUntilReady {
					if deploy.Status.ReadyReplicas < expectedReplicas {
						r.Log.Info("Waiting for Deployment to be ready",
							"namespace", ns,
							"name", deploy.Name,
							"ready", deploy.Status.ReadyReplicas,
							"expected", expectedReplicas)
						allReady = false
						setFirstBlocker(fmt.Sprintf(
							"Deployment %s/%s readyReplicas=%d expected=%d",
							ns,
							deploy.Name,
							deploy.Status.ReadyReplicas,
							expectedReplicas,
						))
					}
				}
				continue
			}

			// 兜底防护：未找到期望副本数时，若当前仍为 0，说明无法确认该工作负载是否应被拉起。
			currentReplicas := int32(0)
			if deploy.Spec.Replicas != nil {
				currentReplicas = *deploy.Spec.Replicas
			}
			if currentReplicas == 0 {
				r.Log.Info("Waiting for Deployment expected replicas metadata",
					"namespace", ns,
					"name", deploy.Name)
				allReady = false
				setFirstBlocker(fmt.Sprintf(
					"Deployment %s/%s missing expected replicas metadata (current=0)",
					ns,
					deploy.Name,
				))
			}
		}

		// 检查 StatefulSets
		stsList := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, stsList, client.InNamespace(ns)); err != nil {
			return false, err
		}
		for _, sts := range stsList.Items {
			namespaceHasWorkload = true

			var expectedReplicas int32 = -1
			key := fmt.Sprintf("%s/statefulsets/%s", ns, sts.Name)

			if val, ok := replicasMap[key]; ok {
				expectedReplicas = val
			} else if val, ok := sts.Annotations["testudo.softcdata.com/original-replicas"]; ok {
				var r int32
				if _, err := fmt.Sscanf(val, "%d", &r); err == nil {
					expectedReplicas = r
				}
			}

			if expectedReplicas != -1 {
				namespaceHasExpectedReplicas = true

				// 1. 检查配置是否下发
				if sts.Spec.Replicas == nil || *sts.Spec.Replicas != expectedReplicas {
					r.Log.Info("Waiting for StatefulSet replicas update",
						"namespace", ns,
						"name", sts.Name,
						"current", sts.Spec.Replicas,
						"expected", expectedReplicas)
					allReady = false
					setFirstBlocker(fmt.Sprintf(
						"StatefulSet %s/%s spec.replicas=%s expected=%d",
						ns,
						sts.Name,
						int32PtrString(sts.Spec.Replicas),
						expectedReplicas,
					))
				}

				// 2. 如果配置要求等待就绪
				if waitUntilReady {
					if sts.Status.ReadyReplicas < expectedReplicas {
						r.Log.Info("Waiting for StatefulSet to be ready",
							"namespace", ns,
							"name", sts.Name,
							"ready", sts.Status.ReadyReplicas,
							"expected", expectedReplicas)
						allReady = false
						setFirstBlocker(fmt.Sprintf(
							"StatefulSet %s/%s readyReplicas=%d expected=%d",
							ns,
							sts.Name,
							sts.Status.ReadyReplicas,
							expectedReplicas,
						))
					}
				}
				continue
			}

			currentReplicas := int32(0)
			if sts.Spec.Replicas != nil {
				currentReplicas = *sts.Spec.Replicas
			}
			if currentReplicas == 0 {
				r.Log.Info("Waiting for StatefulSet expected replicas metadata",
					"namespace", ns,
					"name", sts.Name)
				allReady = false
				setFirstBlocker(fmt.Sprintf(
					"StatefulSet %s/%s missing expected replicas metadata (current=0)",
					ns,
					sts.Name,
				))
			}
		}

		// 命名空间存在工作负载但完全缺失期望副本元数据时，禁止误判为已就绪。
		if namespaceHasWorkload && !namespaceHasExpectedReplicas {
			r.Log.Info("Waiting for expected replicas metadata in namespace", "namespace", ns)
			allReady = false
			setFirstBlocker(fmt.Sprintf(
				"Namespace %s has workloads but missing expected replicas metadata",
				ns,
			))
		}

		// 阻断条件：命名空间存在 Pending PVC/Pod 时，不允许进入 SwitchRoles。
		// 对于已知不可恢复场景（例如 StorageClass 不存在）直接返回错误，使操作失败收敛。
		blocked, blockDetail, blockErr := checkNamespacePendingReadiness(ctx, remoteClient, ns, &knownStorageClasses, &storageClassLoaded)
		if blockErr != nil {
			return false, blockErr
		}
		if blocked {
			allReady = false
			setFirstBlocker(blockDetail)
		}
	}

	setCheckReplicasBlockerMessage(operation, firstBlocker)
	return allReady, nil
}

// resolveCheckReplicasWaitUntilReady 与 resolveWaitUntilReady 规则基本一致，
// 但在 CheckReplicas 步骤默认启用就绪校验，避免误切角色。
func resolveCheckReplicasWaitUntilReady(instance *disasterv1.DisasterInstance, operation *disasterv1.DisasterOperation) bool {
	if operation != nil && operation.Spec.SkipPodReadyCheck != nil {
		return !*operation.Spec.SkipPodReadyCheck
	}
	if operation != nil && operation.Spec.WaitUntilReady {
		return true
	}
	if instance != nil && instance.Spec.SkipPodReadyCheck != nil {
		return !*instance.Spec.SkipPodReadyCheck
	}
	return true
}

func checkNamespacePendingReadiness(
	ctx context.Context,
	remoteClient client.Client,
	namespace string,
	knownStorageClasses *map[string]struct{},
	storageClassLoaded *bool,
) (bool, string, error) {
	blocked := false
	blockDetail := ""
	setBlockDetail := func(msg string) {
		if strings.TrimSpace(msg) == "" || blockDetail != "" {
			return
		}
		blockDetail = strings.TrimSpace(msg)
	}

	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := remoteClient.List(ctx, pvcList, client.InNamespace(namespace)); err != nil {
		return false, "", err
	}
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if pvc.Status.Phase != corev1.ClaimPending {
			continue
		}
		// 命中 SC 缺失时直接失败，避免进入长时间不收敛或误切角色。
		if pvc.Spec.StorageClassName != nil && strings.TrimSpace(*pvc.Spec.StorageClassName) != "" {
			scName := strings.TrimSpace(*pvc.Spec.StorageClassName)
			exists, err := storageClassExists(ctx, remoteClient, scName, knownStorageClasses, storageClassLoaded)
			if err != nil {
				return false, "", err
			}
			if !exists {
				return false, "", fmt.Errorf(
					"CheckReplicasBlocked: PVC %s/%s Pending because StorageClass %s not found",
					namespace, pvc.Name, scName,
				)
			}
		}
		blocked = true
		setBlockDetail(describePendingPVC(pvc))
	}

	podList := &corev1.PodList{}
	if err := remoteClient.List(ctx, podList, client.InNamespace(namespace)); err != nil {
		return false, "", err
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodPending {
			blocked = true
			setBlockDetail(describePendingPod(pod))
		}
	}

	return blocked, blockDetail, nil
}

func storageClassExists(
	ctx context.Context,
	remoteClient client.Client,
	storageClassName string,
	knownStorageClasses *map[string]struct{},
	storageClassLoaded *bool,
) (bool, error) {
	if !*storageClassLoaded {
		scList := &storagev1.StorageClassList{}
		if err := remoteClient.List(ctx, scList); err != nil {
			return false, fmt.Errorf("CheckReplicasBlocked: list storageclasses failed: %w", err)
		}
		cache := make(map[string]struct{}, len(scList.Items))
		for i := range scList.Items {
			cache[scList.Items[i].Name] = struct{}{}
		}
		*knownStorageClasses = cache
		*storageClassLoaded = true
	}
	_, ok := (*knownStorageClasses)[storageClassName]
	return ok, nil
}

func setCheckReplicasBlockerMessage(operation *disasterv1.DisasterOperation, blocker string) {
	if operation == nil {
		return
	}
	targetStepName := string(disasterv1.FailoverStepCheckReplicas)
	for i := range operation.Status.Steps {
		if operation.Status.Steps[i].Name != targetStepName {
			continue
		}
		operation.Status.Steps[i].Message = strings.TrimSpace(blocker)
		return
	}
}

func int32PtrString(v *int32) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *v)
}

func describePendingPVC(pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil {
		return ""
	}
	reason := ""
	for i := range pvc.Status.Conditions {
		condition := pvc.Status.Conditions[i]
		if strings.TrimSpace(condition.Reason) == "" {
			continue
		}
		reason = strings.TrimSpace(condition.Reason)
		break
	}
	if reason == "" {
		return fmt.Sprintf("PVC %s/%s Pending", pvc.Namespace, pvc.Name)
	}
	return fmt.Sprintf("PVC %s/%s Pending (reason=%s)", pvc.Namespace, pvc.Name, reason)
}

func describePendingPod(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	reason := extractPendingPodReason(pod)
	if reason == "" {
		return fmt.Sprintf("Pod %s/%s Pending", pod.Namespace, pod.Name)
	}
	return fmt.Sprintf("Pod %s/%s Pending (reason=%s)", pod.Namespace, pod.Name, reason)
}

func extractPendingPodReason(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	for i := range pod.Status.InitContainerStatuses {
		waiting := pod.Status.InitContainerStatuses[i].State.Waiting
		if waiting == nil || strings.TrimSpace(waiting.Reason) == "" {
			continue
		}
		return strings.TrimSpace(waiting.Reason)
	}
	for i := range pod.Status.ContainerStatuses {
		waiting := pod.Status.ContainerStatuses[i].State.Waiting
		if waiting == nil || strings.TrimSpace(waiting.Reason) == "" {
			continue
		}
		return strings.TrimSpace(waiting.Reason)
	}
	for i := range pod.Status.Conditions {
		condition := pod.Status.Conditions[i]
		if condition.Type != corev1.PodScheduled || condition.Status != corev1.ConditionFalse {
			continue
		}
		if strings.TrimSpace(condition.Reason) != "" {
			return strings.TrimSpace(condition.Reason)
		}
	}
	return strings.TrimSpace(pod.Status.Reason)
}
