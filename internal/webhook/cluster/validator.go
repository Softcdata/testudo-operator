package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	platformlicense "github.com/softcdata/testudo-operator/pkg/license"
)

const ValidateClusterPath = "/validate-testudo-softcdata-com-v1-cluster"

// +kubebuilder:webhook:path=/validate-testudo-softcdata-com-v1-cluster,mutating=false,failurePolicy=Fail,sideEffects=None,groups=testudo.softcdata.com,resources=clusters,verbs=create,versions=v1,name=vcluster.kb.io,admissionReviewVersions=v1

type Validator struct {
	Client           client.Client
	LicenseNamespace string
	LicenseCAPath    string
	Verifier         *platformlicense.Verifier
}

func NewValidator(cli client.Client, licenseNamespace, licenseCAPath string, verifier *platformlicense.Verifier) *Validator {
	return &Validator{
		Client:           cli,
		LicenseNamespace: strings.TrimSpace(licenseNamespace),
		LicenseCAPath:    strings.TrimSpace(licenseCAPath),
		Verifier:         verifier,
	}
}

func (v *Validator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Create {
		return admission.Allowed("operation skipped")
	}
	if v == nil || v.Client == nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("cluster webhook client is not initialized"))
	}

	cluster := &disasterv1.Cluster{}
	if err := json.Unmarshal(req.Object.Raw, cluster); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	count, err := v.preCreateClusterCount(ctx)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	store := platformlicense.KubernetesStore{
		Client:    v.Client,
		Namespace: v.effectiveLicenseNamespace(),
		CAPath:    v.LicenseCAPath,
	}
	entitlement := store.Evaluate(ctx, v.effectiveVerifier())
	if entitlement.CanCreateCluster(count) {
		return admission.Allowed("cluster license limit allows create")
	}
	return admission.Denied(fmt.Sprintf(
		"%s: community edition allows at most %d clusters; current clusters=%d, license state=%s",
		platformlicense.ReasonLicenseLimitExceeded,
		entitlement.ClusterLimit(),
		count,
		entitlement.State,
	))
}

func (v *Validator) preCreateClusterCount(ctx context.Context) (int, error) {
	clusters := &disasterv1.ClusterList{}
	if err := v.Client.List(ctx, clusters); err != nil {
		return 0, err
	}
	count := 0
	for i := range clusters.Items {
		if clusters.Items[i].DeletionTimestamp.IsZero() {
			count++
		}
	}
	return count, nil
}

func (v *Validator) effectiveLicenseNamespace() string {
	if strings.TrimSpace(v.LicenseNamespace) != "" {
		return strings.TrimSpace(v.LicenseNamespace)
	}
	return platformlicense.DefaultLicenseNamespace
}

func (v *Validator) effectiveVerifier() *platformlicense.Verifier {
	if v.Verifier != nil {
		return v.Verifier
	}
	return platformlicense.NewDefaultVerifier()
}
