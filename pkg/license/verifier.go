package license

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var DefaultTrustedKeys = map[string]ed25519.PublicKey{
	"ed25519-2026-01": mustRawURLEncodedPublicKey("JVAiBtNy25YKdMZ1dI73feCeVoxNSBgYgG/k6zhtNjo="),
}

type Verifier struct {
	Product            string
	FingerprintVersion string
	Keys               map[string]ed25519.PublicKey
	Now                func() time.Time
}

func NewVerifier(keys map[string]ed25519.PublicKey) *Verifier {
	cloned := make(map[string]ed25519.PublicKey, len(keys))
	for keyID, key := range keys {
		cloned[keyID] = append(ed25519.PublicKey(nil), key...)
	}
	return &Verifier{
		Product:            ProductName,
		FingerprintVersion: FingerprintVersionK8SV1,
		Keys:               cloned,
	}
}

func NewDefaultVerifier() *Verifier {
	return NewVerifier(DefaultTrustedKeys)
}

func (v *Verifier) Verify(raw []byte, currentFingerprint string) Entitlement {
	claims, entitlement := v.verifyContent(raw)
	if !entitlement.IsActive() {
		return entitlement
	}

	if strings.TrimSpace(claims.Fingerprint) != strings.TrimSpace(currentFingerprint) {
		return fallbackEntitlement(StateFingerprintMismatch, ReasonLicenseFingerprintMismatch, "license fingerprint does not match this deployment")
	}

	entitlement.Message = "enterprise license is active"
	entitlement.FingerprintMatched = true
	return entitlement
}

func (v *Verifier) VerifyContent(raw []byte) Entitlement {
	_, entitlement := v.verifyContent(raw)
	return entitlement
}

func (v *Verifier) verifyContent(raw []byte) (*Claims, Entitlement) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, FreeEntitlement("NoLicense", "license secret is not installed")
	}

	claims, state, reason, message := parseAndValidateClaims(raw, v)
	if state != "" {
		return nil, fallbackEntitlement(state, reason, message)
	}

	key := v.Keys[strings.TrimSpace(claims.KeyID)]
	signature, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(claims.Signature))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, fallbackEntitlement(StateInvalidSignature, ReasonLicenseInvalid, "license signature is not valid base64url Ed25519 data")
	}

	payload, err := CanonicalPayload(raw)
	if err != nil {
		return nil, fallbackEntitlement(StateMalformed, ReasonLicenseInvalid, err.Error())
	}
	if !ed25519.Verify(key, payload, signature) {
		return nil, fallbackEntitlement(StateInvalidSignature, ReasonLicenseInvalid, "license signature verification failed")
	}

	now := time.Now().UTC()
	if v != nil && v.Now != nil {
		now = v.Now().UTC()
	}
	notBefore, _ := parseUTCRFC3339Seconds(claims.NotBefore)
	expiresAt, _ := parseUTCRFC3339Seconds(claims.ExpiresAt)
	if now.Before(notBefore) {
		return nil, fallbackEntitlement(StateNotYetValid, ReasonLicenseInvalid, "license is not yet valid")
	}
	if now.After(expiresAt) {
		return nil, fallbackEntitlement(StateExpired, ReasonLicenseExpired, "license is expired")
	}

	features := make(map[string]bool, len(claims.Features))
	for _, feature := range claims.Features {
		feature = strings.TrimSpace(feature)
		if feature != "" {
			features[feature] = true
		}
	}

	customer := ""
	if claims.Customer != nil {
		customer = strings.TrimSpace(claims.Customer.Name)
	}
	return claims, Entitlement{
		State:       StateActive,
		Edition:     strings.TrimSpace(claims.Edition),
		LicenseID:   strings.TrimSpace(claims.LicenseID),
		Customer:    customer,
		MaxClusters: *claims.Limits.MaxClusters,
		ExpiresAt:   &expiresAt,
		Features:    features,
		Message:     "license content is valid",
	}
}

func parseAndValidateClaims(raw []byte, verifier *Verifier) (*Claims, State, string, string) {
	if err := rejectDuplicateObjectNames(raw); err != nil {
		return nil, StateMalformed, ReasonLicenseInvalid, err.Error()
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, StateMalformed, ReasonLicenseInvalid, err.Error()
	}
	if root == nil {
		return nil, StateMalformed, ReasonLicenseInvalid, "license root must be an object"
	}
	for _, field := range []string{
		"version",
		"licenseId",
		"product",
		"customer",
		"edition",
		"fingerprintVersion",
		"fingerprint",
		"issuedAt",
		"notBefore",
		"expiresAt",
		"limits",
		"features",
		"keyId",
		"signature",
	} {
		if _, ok := root[field]; !ok {
			return nil, StateMalformed, ReasonLicenseInvalid, fmt.Sprintf("license field %q is required", field)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var claims Claims
	if err := decoder.Decode(&claims); err != nil {
		return nil, StateMalformed, ReasonLicenseInvalid, err.Error()
	}
	if err := ensureNoTrailingJSON(decoder); err != nil {
		return nil, StateMalformed, ReasonLicenseInvalid, err.Error()
	}

	if claims.Version != SupportedLicenseVersion {
		return nil, StateUnsupportedVersion, ReasonLicenseUnsupportedVersion, fmt.Sprintf("unsupported license version %d", claims.Version)
	}

	product := ProductName
	if verifier != nil && strings.TrimSpace(verifier.Product) != "" {
		product = strings.TrimSpace(verifier.Product)
	}
	if strings.TrimSpace(claims.Product) != product {
		return nil, StateProductMismatch, ReasonLicenseProductMismatch, fmt.Sprintf("license product must be %q", product)
	}

	fpVersion := FingerprintVersionK8SV1
	if verifier != nil && strings.TrimSpace(verifier.FingerprintVersion) != "" {
		fpVersion = strings.TrimSpace(verifier.FingerprintVersion)
	}
	if strings.TrimSpace(claims.FingerprintVersion) != fpVersion {
		return nil, StateMalformed, ReasonLicenseInvalid, fmt.Sprintf("license fingerprintVersion must be %q", fpVersion)
	}

	if strings.TrimSpace(claims.KeyID) == "" {
		return nil, StateMalformed, ReasonLicenseInvalid, "license keyId is required"
	}
	if verifier == nil || verifier.Keys == nil {
		return nil, StateUnknownKey, ReasonLicenseUnknownKey, fmt.Sprintf("license keyId %q is unknown", claims.KeyID)
	}
	key, ok := verifier.Keys[strings.TrimSpace(claims.KeyID)]
	if !ok || len(key) != ed25519.PublicKeySize {
		return nil, StateUnknownKey, ReasonLicenseUnknownKey, fmt.Sprintf("license keyId %q is unknown", claims.KeyID)
	}

	if strings.TrimSpace(claims.LicenseID) == "" {
		return nil, StateMalformed, ReasonLicenseInvalid, "licenseId is required"
	}
	if claims.Customer == nil || strings.TrimSpace(claims.Customer.Name) == "" {
		return nil, StateMalformed, ReasonLicenseInvalid, "customer.name is required"
	}
	if strings.TrimSpace(claims.Edition) == "" {
		return nil, StateMalformed, ReasonLicenseInvalid, "edition is required"
	}
	if strings.TrimSpace(claims.Fingerprint) == "" {
		return nil, StateMalformed, ReasonLicenseInvalid, "fingerprint is required"
	}
	if claims.Limits == nil || claims.Limits.MaxClusters == nil {
		return nil, StateMalformed, ReasonLicenseInvalid, "limits.maxClusters is required"
	}
	if *claims.Limits.MaxClusters < 0 && *claims.Limits.MaxClusters != UnlimitedClusters {
		return nil, StateMalformed, ReasonLicenseInvalid, "limits.maxClusters must be -1 or >= 0"
	}
	if claims.Features == nil {
		return nil, StateMalformed, ReasonLicenseInvalid, "features is required"
	}
	if strings.TrimSpace(claims.Signature) == "" {
		return nil, StateInvalidSignature, ReasonLicenseInvalid, "signature is required"
	}

	issuedAt, err := parseUTCRFC3339Seconds(claims.IssuedAt)
	if err != nil {
		return nil, StateMalformed, ReasonLicenseInvalid, fmt.Sprintf("issuedAt: %v", err)
	}
	notBefore, err := parseUTCRFC3339Seconds(claims.NotBefore)
	if err != nil {
		return nil, StateMalformed, ReasonLicenseInvalid, fmt.Sprintf("notBefore: %v", err)
	}
	expiresAt, err := parseUTCRFC3339Seconds(claims.ExpiresAt)
	if err != nil {
		return nil, StateMalformed, ReasonLicenseInvalid, fmt.Sprintf("expiresAt: %v", err)
	}
	if issuedAt.After(expiresAt) {
		return nil, StateMalformed, ReasonLicenseInvalid, "issuedAt must not be after expiresAt"
	}
	if notBefore.After(expiresAt) {
		return nil, StateMalformed, ReasonLicenseInvalid, "notBefore must not be after expiresAt"
	}

	return &claims, "", "", ""
}

func parseUTCRFC3339Seconds(value string) (time.Time, error) {
	if !strings.HasSuffix(value, "Z") {
		return time.Time{}, fmt.Errorf("must use UTC RFC3339 format ending with Z")
	}
	if strings.Contains(value, ".") {
		return time.Time{}, fmt.Errorf("must not contain fractional seconds")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	if parsed.Format(time.RFC3339) != value {
		return time.Time{}, fmt.Errorf("must use canonical UTC RFC3339 seconds")
	}
	return parsed, nil
}

func mustRawURLEncodedPublicKey(value string) ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		panic(err)
	}
	if len(raw) != ed25519.PublicKeySize {
		panic("invalid Ed25519 public key size")
	}
	return ed25519.PublicKey(raw)
}
