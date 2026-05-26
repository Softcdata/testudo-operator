package disasterinstance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/softcdata/testudo-operator/internal/controller/restore"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/tools"
)

const (
	// ValidateDisasterInstancePath is the webhook endpoint for DisasterInstance validation.
	ValidateDisasterInstancePath = "/validate-testudo-softcdata-com-v1-disasterinstance"
)

// +kubebuilder:webhook:path=/validate-testudo-softcdata-com-v1-disasterinstance,mutating=false,failurePolicy=Fail,sideEffects=None,groups=testudo.softcdata.com,resources=disasterinstances,verbs=create;update,versions=v1,name=vdisasterinstance.kb.io,admissionReviewVersions=v1

// RestorePolicyValidator validates restore policy modifier rules on create/update.
type RestorePolicyValidator struct {
	Client client.Client
	// ClusterRESTConfigFunc builds rest config from source cluster definition.
	ClusterRESTConfigFunc func(cluster *disasterv1.Cluster) (*rest.Config, error)
	// LocatorBuilderFunc builds a live resource locator from rest config.
	LocatorBuilderFunc func(restConfig *rest.Config) (restore.ModifierRuleResourceLocator, error)
}

// NewRestorePolicyValidator creates a DisasterInstance validating webhook handler.
func NewRestorePolicyValidator(cli client.Client) *RestorePolicyValidator {
	return &RestorePolicyValidator{
		Client:                cli,
		ClusterRESTConfigFunc: clusterRESTConfig,
		LocatorBuilderFunc: func(restConfig *rest.Config) (restore.ModifierRuleResourceLocator, error) {
			return restore.NewDynamicModifierRuleResourceLocator(restConfig)
		},
	}
}

// Handle performs submission-time validation for DisasterInstance.
func (v *RestorePolicyValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed("operation skipped")
	}

	instance := &disasterv1.DisasterInstance{}
	if err := json.Unmarshal(req.Object.Raw, instance); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := v.validateRestorePolicy(ctx, instance); err != nil {
		return admission.Denied(err.Error())
	}
	return admission.Allowed("restore policy rules are valid")
}

func (v *RestorePolicyValidator) validateRestorePolicy(ctx context.Context, instance *disasterv1.DisasterInstance) error {
	if instance == nil || instance.Spec.RestorePolicy == nil {
		return nil
	}
	if err := restore.ValidateRestoreResourceSelectionAtSubmission(instance.Spec.RestorePolicy.ResourceSelection); err != nil {
		return err
	}
	modifierInput, _, err := restore.EffectiveModifierRuleInput(instance.Spec.RestorePolicy)
	if err != nil {
		return err
	}
	if len(modifierInput) == 0 {
		return nil
	}
	if v.Client == nil {
		return fmt.Errorf("%s: webhook client is not initialized", restore.ModifierErrorRuleRejected)
	}

	configName := strings.TrimSpace(instance.Spec.Config)
	if configName == "" {
		return fmt.Errorf("%s: spec.config is required when modifierRules are configured", restore.ModifierErrorRuleRejected)
	}
	config := &disasterv1.DisasterConfig{}
	if err := v.Client.Get(ctx, types.NamespacedName{Name: configName}, config); err != nil {
		return fmt.Errorf(
			"%s: load disasterConfig %s failed: %w",
			restore.ModifierErrorRuleRejected,
			configName,
			err,
		)
	}

	sourceClusterName := strings.TrimSpace(config.Spec.SourceCluster)
	targetClusterName := strings.TrimSpace(config.Spec.TargetCluster)
	if sourceClusterName == "" || targetClusterName == "" {
		return fmt.Errorf(
			"%s: disasterConfig %s must define sourceCluster and targetCluster",
			restore.ModifierErrorRuleRejected,
			configName,
		)
	}

	sourceCluster := &disasterv1.Cluster{}
	if err := v.Client.Get(ctx, types.NamespacedName{Name: sourceClusterName}, sourceCluster); err != nil {
		return fmt.Errorf(
			"%s: load source cluster %s failed: %w",
			restore.ModifierErrorRuleRejected,
			sourceClusterName,
			err,
		)
	}

	clusterRESTConfigFunc := v.ClusterRESTConfigFunc
	if clusterRESTConfigFunc == nil {
		clusterRESTConfigFunc = clusterRESTConfig
	}
	restConfig, err := clusterRESTConfigFunc(sourceCluster)
	if err != nil {
		return fmt.Errorf(
			"%s: build source cluster config failed: %w",
			restore.ModifierErrorRuleRejected,
			err,
		)
	}
	locatorBuilder := v.LocatorBuilderFunc
	if locatorBuilder == nil {
		locatorBuilder = func(restConfig *rest.Config) (restore.ModifierRuleResourceLocator, error) {
			return restore.NewDynamicModifierRuleResourceLocator(restConfig)
		}
	}
	locator, err := locatorBuilder(restConfig)
	if err != nil {
		return fmt.Errorf(
			"%s: build source cluster resource locator failed: %w",
			restore.ModifierErrorRuleRejected,
			err,
		)
	}

	shadow := instance.DeepCopy()
	shadow.Spec.RestorePolicy.ModifierRules = modifierInput

	return restore.ValidateModifierRulesAtSubmission(
		ctx,
		shadow,
		sourceClusterName,
		targetClusterName,
		locator,
	)
}

func clusterRESTConfig(cluster *disasterv1.Cluster) (*rest.Config, error) {
	if cluster == nil {
		return nil, fmt.Errorf("nil cluster")
	}
	if len(cluster.Spec.KubeConfig) > 0 {
		return tools.GetRestConfig(cluster.Spec.KubeConfig)
	}
	if cluster.Spec.Token != "" && cluster.Spec.Endpoint != "" {
		return tools.GetRestConfigFromToken(cluster.Spec.Endpoint, cluster.Spec.Token)
	}
	return nil, fmt.Errorf("cluster %s has no kubeconfig or token/endpoint", cluster.Name)
}
