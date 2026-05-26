package controller

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/tools"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	RestorePhaseInProgressWaitSeconds time.Duration = 5 * time.Second  // 当恢复处于进行中状态时，等待的秒数
	RestorePhaseUnknownWaitSeconds    time.Duration = 10 * time.Second // 当恢复处于未知状态时，等待的秒数
	RestorePhaseCreateWaitSeconds     time.Duration = 3 * time.Second  // 创建恢复后，等待的秒数
	RestorePhaseInProgressMaxWait     time.Duration = 1 * time.Hour    // 当恢复处于进行中状态时，最大等待时间
	RestorePhaseUnknownMaxWait        time.Duration = 1 * time.Hour    // 当恢复处于未知状态时，最大等待时间

	// Backup timeout constants
	BackupPhaseInProgressMaxWait time.Duration = 2 * time.Hour    // 当备份处于进行中状态时，最大等待时间
	BackupPhaseUnknownMaxWait    time.Duration = 10 * time.Minute // 当备份已创建但 Velero 尚未写入 phase 时，最大等待时间
)

var (
	VeleroNamespace            = "velero"
	VeleroCredentialSecretName = "disaster-s3-credentials"
	VeleroCredentialTemplate   = `[default]
aws_access_key_id = %s
aws_secret_access_key = %s`
)

const DefaultManagementNamespace = "disaster-system"

var configuredManagementNamespace = DefaultManagementNamespace

func SetManagementNamespace(namespace string) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = DefaultManagementNamespace
	}
	configuredManagementNamespace = namespace
}

func ManagementNamespace() string {
	if namespace := strings.TrimSpace(configuredManagementNamespace); namespace != "" {
		return namespace
	}
	return DefaultManagementNamespace
}

// GetKubeClientSet 获取 kube client
func GetKubeClientSet(ctx context.Context, cli client.Client, scheme *runtime.Scheme, clusterName string) (client.Client, error) {
	cluster, err := GetClusterByClusterName(ctx, cli, clusterName)
	if err != nil {
		return nil, err
	}

	return GetKubeClientSetWithCluster(ctx, cli, scheme, cluster)
}

// ClientFactory defines an interface for creating kube clients
type ClientFactory interface {
	GetKubeClient(ctx context.Context, cli client.Client, scheme *runtime.Scheme, clusterName string) (client.Client, error)
}
type DefaultClientFactory struct{}

func (f *DefaultClientFactory) GetKubeClient(ctx context.Context, cli client.Client, scheme *runtime.Scheme, clusterName string) (client.Client, error) {
	return GetKubeClientSet(ctx, cli, scheme, clusterName)
}

// CommandExecutor is an interface for executing commands
type CommandExecutor interface {
	Run(name string, args ...string) error
}

// DefaultCommandExecutor is the default implementation of CommandExecutor
type DefaultCommandExecutor struct{}

func (e *DefaultCommandExecutor) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// GetKubeClientSet 获取 kube client
func GetKubeClientSetWithCluster(ctx context.Context, cli client.Client, scheme *runtime.Scheme, cluster *disasterv1.Cluster) (client.Client, error) {
	var restConfig *rest.Config
	var err error

	if len(cluster.Spec.KubeConfig) > 0 {
		restConfig, err = tools.GetRestConfig(cluster.Spec.KubeConfig)
	} else if cluster.Spec.Token != "" && cluster.Spec.Endpoint != "" {
		restConfig, err = tools.GetRestConfigFromToken(cluster.Spec.Endpoint, cluster.Spec.Token)
	} else {
		return nil, fmt.Errorf("neither kubeconfig nor token/endpoint provided")
	}

	if err != nil {
		return nil, err
	}

	return client.New(restConfig, client.Options{Scheme: scheme})
}

// 获取集群的 kubeconfig
func GetClusterByClusterName(ctx context.Context, cli client.Client, clsutername string) (*disasterv1.Cluster, error) {
	cluster := &disasterv1.Cluster{}
	err := cli.Get(ctx, types.NamespacedName{Name: clsutername}, cluster)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}
