package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"
)

const testFingerprint = "sha256:0123456789abcdef"

func TestVerifierActiveLicense(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	raw := signedTestLicense(t, privateKey, "test-key", func(claims map[string]any) {})
	verifier := NewVerifier(map[string]ed25519.PublicKey{"test-key": publicKey})
	verifier.Now = func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }

	entitlement := verifier.Verify(raw, testFingerprint)
	if entitlement.State != StateActive {
		t.Fatalf("expected active entitlement, got state=%s reason=%s message=%s", entitlement.State, entitlement.Reason, entitlement.Message)
	}
	if entitlement.ClusterLimit() != UnlimitedClusters {
		t.Fatalf("expected unlimited clusters, got %d", entitlement.ClusterLimit())
	}
	if !entitlement.CanCreateCluster(100) || !entitlement.CanAcceptCluster(100) {
		t.Fatalf("expected unlimited entitlement to allow cluster creation and acceptance")
	}
}

func TestVerifierNegativeStates(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	verifier := NewVerifier(map[string]ed25519.PublicKey{"test-key": publicKey})
	verifier.Now = func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }

	tests := []struct {
		name      string
		raw       func() []byte
		wantState State
	}{
		{
			name: "product mismatch",
			raw: func() []byte {
				return signedTestLicense(t, privateKey, "test-key", func(claims map[string]any) {
					claims["product"] = "other-product"
				})
			},
			wantState: StateProductMismatch,
		},
		{
			name: "unknown key",
			raw: func() []byte {
				return signedTestLicense(t, privateKey, "unknown-key", func(claims map[string]any) {})
			},
			wantState: StateUnknownKey,
		},
		{
			name: "unsupported version",
			raw: func() []byte {
				return signedTestLicense(t, privateKey, "test-key", func(claims map[string]any) {
					claims["version"] = 2
				})
			},
			wantState: StateUnsupportedVersion,
		},
		{
			name: "malformed limits",
			raw: func() []byte {
				return signedTestLicense(t, privateKey, "test-key", func(claims map[string]any) {
					claims["limits"] = map[string]any{"maxClusters": -2}
				})
			},
			wantState: StateMalformed,
		},
		{
			name: "expired",
			raw: func() []byte {
				return signedTestLicense(t, privateKey, "test-key", func(claims map[string]any) {
					claims["expiresAt"] = "2026-05-01T00:00:00Z"
				})
			},
			wantState: StateExpired,
		},
		{
			name: "fingerprint mismatch",
			raw: func() []byte {
				return signedTestLicense(t, privateKey, "test-key", func(claims map[string]any) {
					claims["fingerprint"] = "sha256:ffffffff"
				})
			},
			wantState: StateFingerprintMismatch,
		},
		{
			name: "invalid signature",
			raw: func() []byte {
				raw := signedTestLicense(t, privateKey, "test-key", func(claims map[string]any) {})
				var claims map[string]any
				if err := json.Unmarshal(raw, &claims); err != nil {
					t.Fatalf("unmarshal signed license: %v", err)
				}
				claims["signature"] = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
				raw, err := json.Marshal(claims)
				if err != nil {
					t.Fatalf("marshal invalid signature license: %v", err)
				}
				return raw
			},
			wantState: StateInvalidSignature,
		},
		{
			name: "duplicate field",
			raw: func() []byte {
				return []byte(`{"version":1,"version":1}`)
			},
			wantState: StateMalformed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entitlement := verifier.Verify(tt.raw(), testFingerprint)
			if entitlement.State != tt.wantState {
				t.Fatalf("expected state %s, got %s reason=%s message=%s", tt.wantState, entitlement.State, entitlement.Reason, entitlement.Message)
			}
			if entitlement.ClusterLimit() != DefaultFreeMaxClusters {
				t.Fatalf("negative state must fall back to free limit, got %d", entitlement.ClusterLimit())
			}
		})
	}
}

func TestCanonicalPayloadRemovesSignatureAndSortsKeys(t *testing.T) {
	raw := []byte(`{"z":"last","signature":"ignored","a":{"b":2,"a":1}}`)
	canonical, err := CanonicalPayload(raw)
	if err != nil {
		t.Fatalf("canonical payload: %v", err)
	}
	want := `{"a":{"a":1,"b":2},"z":"last"}`
	if string(canonical) != want {
		t.Fatalf("unexpected canonical payload\nwant: %s\ngot:  %s", want, canonical)
	}
}

func TestHashAPIServerCABundleIsDeterministicForPEMAndDER(t *testing.T) {
	certDER := newTestCertificateDER(t)
	pemBundle := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	pemHash, err := HashAPIServerCABundle(pemBundle)
	if err != nil {
		t.Fatalf("hash PEM bundle: %v", err)
	}
	derHash, err := HashAPIServerCABundle(certDER)
	if err != nil {
		t.Fatalf("hash DER bundle: %v", err)
	}
	if pemHash != derHash {
		t.Fatalf("expected PEM and DER hash to match, pem=%s der=%s", pemHash, derHash)
	}
}

func signedTestLicense(t *testing.T, privateKey ed25519.PrivateKey, keyID string, mutate func(map[string]any)) []byte {
	t.Helper()
	claims := map[string]any{
		"version":            1,
		"licenseId":          "LIC-TEST-0001",
		"product":            ProductName,
		"customer":           map[string]any{"name": "Acme Corp", "contact": "ops@example.com"},
		"edition":            "enterprise",
		"fingerprintVersion": FingerprintVersionK8SV1,
		"fingerprint":        testFingerprint,
		"issuedAt":           "2026-05-01T00:00:00Z",
		"notBefore":          "2026-05-01T00:00:00Z",
		"expiresAt":          "2027-05-01T00:00:00Z",
		"limits":             map[string]any{"maxClusters": -1},
		"features":           []any{"cluster.unlimited"},
		"issuer":             "unit-test",
		"keyId":              keyID,
	}
	if mutate != nil {
		mutate(claims)
	}
	unsigned, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal unsigned license: %v", err)
	}
	signature, err := signTestRawLicense(unsigned, privateKey)
	if err != nil {
		t.Fatalf("sign license: %v", err)
	}
	claims["signature"] = signature
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal signed license: %v", err)
	}
	return raw
}

func signTestRawLicense(raw []byte, privateKey ed25519.PrivateKey) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("private key must be %d bytes", ed25519.PrivateKeySize)
	}
	payload, err := CanonicalPayload(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload)), nil
}

func newTestCertificateDER(t *testing.T) []byte {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate cert key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return der
}
