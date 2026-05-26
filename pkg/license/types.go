package license

import "time"

const (
	ProductName                 = "disaster-platform"
	SupportedLicenseVersion     = 1
	FingerprintVersionK8SV1     = "k8s-v1"
	DefaultFreeMaxClusters      = 2
	UnlimitedClusters           = -1
	DefaultLicenseNamespace     = "disaster-system"
	LicenseSecretName           = "disaster-platform-license"
	LicenseSecretDataKey        = "license.lic"
	LicenseSecretType           = "testudo.softcdata.com/license"
	InstallIDSecretName         = "disaster-platform-install-id"
	InstallIDSecretDataKey      = "install-id"
	StatusConfigMapName         = "disaster-platform-license-status"
	GateStateConfigMapName      = "disaster-platform-license-gate-state"
	GateStateEnabledAtKey       = "enabledAt"
	DefaultServiceAccountCAPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	AnnotationLicenseAccepted       = "testudo.softcdata.com/license-accepted"
	AnnotationLicenseAcceptedAt     = "testudo.softcdata.com/license-accepted-at"
	AnnotationLicenseID             = "testudo.softcdata.com/license-id"
	AnnotationLicenseAcceptedReason = "testudo.softcdata.com/license-accepted-reason"

	LicenseIDGrandfathered              = "grandfathered"
	LicenseAcceptedReasonPreGateUpgrade = "pre-license-gate-upgrade"

	ReasonLicenseLimitExceeded       = "LicenseLimitExceeded"
	ReasonLicenseInvalid             = "LicenseInvalid"
	ReasonLicenseExpired             = "LicenseExpired"
	ReasonLicenseFingerprintMismatch = "LicenseFingerprintMismatch"
	ReasonLicenseEnvironmentInvalid  = "LicenseEnvironmentInvalid"
	ReasonLicenseUnsupportedVersion  = "LicenseUnsupportedVersion"
	ReasonLicenseUnknownKey          = "LicenseUnknownKey"
	ReasonLicenseProductMismatch     = "LicenseProductMismatch"
)

type State string

const (
	StateFree                State = "Free"
	StateActive              State = "Active"
	StateExpired             State = "Expired"
	StateInvalidSignature    State = "InvalidSignature"
	StateFingerprintMismatch State = "FingerprintMismatch"
	StateNotYetValid         State = "NotYetValid"
	StateMalformed           State = "Malformed"
	StateUnsupportedVersion  State = "UnsupportedVersion"
	StateProductMismatch     State = "ProductMismatch"
	StateUnknownKey          State = "UnknownKey"
	StateUnknown             State = "Unknown"
)

type Customer struct {
	Name    string `json:"name"`
	Contact string `json:"contact"`
}

type Limits struct {
	MaxClusters *int `json:"maxClusters"`
}

type Claims struct {
	Version            int       `json:"version"`
	LicenseID          string    `json:"licenseId"`
	Product            string    `json:"product"`
	Customer           *Customer `json:"customer"`
	Edition            string    `json:"edition"`
	FingerprintVersion string    `json:"fingerprintVersion"`
	Fingerprint        string    `json:"fingerprint"`
	IssuedAt           string    `json:"issuedAt"`
	NotBefore          string    `json:"notBefore"`
	ExpiresAt          string    `json:"expiresAt"`
	Limits             *Limits   `json:"limits"`
	Features           []string  `json:"features"`
	Issuer             string    `json:"issuer,omitempty"`
	KeyID              string    `json:"keyId"`
	Signature          string    `json:"signature"`
}

type Entitlement struct {
	State              State
	Edition            string
	LicenseID          string
	Customer           string
	MaxClusters        int
	ExpiresAt          *time.Time
	Features           map[string]bool
	Reason             string
	Message            string
	FingerprintMatched bool
}

func FreeEntitlement(reason, message string) Entitlement {
	return Entitlement{
		State:       StateFree,
		MaxClusters: DefaultFreeMaxClusters,
		Features:    map[string]bool{},
		Reason:      reason,
		Message:     message,
	}
}

func fallbackEntitlement(state State, reason, message string) Entitlement {
	return Entitlement{
		State:       state,
		MaxClusters: DefaultFreeMaxClusters,
		Features:    map[string]bool{},
		Reason:      reason,
		Message:     message,
	}
}

func (e Entitlement) IsActive() bool {
	return e.State == StateActive
}

func (e Entitlement) ClusterLimit() int {
	if e.State == StateActive {
		return e.MaxClusters
	}
	return DefaultFreeMaxClusters
}

func (e Entitlement) CanCreateCluster(preCreateCount int) bool {
	limit := e.ClusterLimit()
	return limit < 0 || preCreateCount < limit
}

func (e Entitlement) CanAcceptCluster(acceptedSiblingCount int) bool {
	limit := e.ClusterLimit()
	return limit < 0 || acceptedSiblingCount < limit
}

func (e Entitlement) Allows(feature string) bool {
	return e.Features != nil && e.Features[feature]
}

func (e Entitlement) StableReason() string {
	switch e.State {
	case StateExpired:
		return ReasonLicenseExpired
	case StateFingerprintMismatch:
		return ReasonLicenseFingerprintMismatch
	case StateUnsupportedVersion:
		return ReasonLicenseUnsupportedVersion
	case StateUnknownKey:
		return ReasonLicenseUnknownKey
	case StateProductMismatch:
		return ReasonLicenseProductMismatch
	case StateUnknown:
		if e.Reason != "" {
			return e.Reason
		}
		return ReasonLicenseInvalid
	case StateMalformed, StateInvalidSignature, StateNotYetValid:
		return ReasonLicenseInvalid
	default:
		return ""
	}
}

func (e Entitlement) CustomerName() string {
	return e.Customer
}
