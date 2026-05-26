package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

type conformanceVectors struct {
	Now                string                  `json:"now"`
	CurrentFingerprint string                  `json:"currentFingerprint"`
	TrustedKeys        []conformanceKey        `json:"trustedKeys"`
	CanonicalCases     []conformanceCanonical  `json:"canonicalCases"`
	VerificationCases  []conformanceVerifyCase `json:"verificationCases"`
}

type conformanceKey struct {
	KeyID           string `json:"keyId"`
	PublicKeyBase64 string `json:"publicKeyBase64"`
}

type conformanceCanonical struct {
	Name              string          `json:"name"`
	License           json.RawMessage `json:"license"`
	Raw               string          `json:"raw"`
	WantPayload       string          `json:"wantPayload"`
	WantErrorContains string          `json:"wantErrorContains"`
}

type conformanceVerifyCase struct {
	Name                   string          `json:"name"`
	License                json.RawMessage `json:"license"`
	WantState              State           `json:"wantState"`
	WantReason             string          `json:"wantReason"`
	WantFingerprintMatched bool            `json:"wantFingerprintMatched"`
	WantMaxClusters        int             `json:"wantMaxClusters"`
}

func TestLicenseConformanceVectors(t *testing.T) {
	vectors := loadConformanceVectors(t)
	verifier := NewVerifier(conformanceTrustedKeys(t, vectors.TrustedKeys))
	now, err := time.Parse(time.RFC3339, vectors.Now)
	if err != nil {
		t.Fatalf("parse vector now: %v", err)
	}
	verifier.Now = func() time.Time { return now }

	for _, tc := range vectors.CanonicalCases {
		t.Run("canonical/"+tc.Name, func(t *testing.T) {
			raw := []byte(tc.Raw)
			if len(raw) == 0 {
				raw = []byte(tc.License)
			}
			payload, err := CanonicalPayload(raw)
			if tc.WantErrorContains != "" {
				if err == nil || !strings.Contains(err.Error(), tc.WantErrorContains) {
					t.Fatalf("expected canonical error containing %q, got payload=%q err=%v", tc.WantErrorContains, payload, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("canonical payload: %v", err)
			}
			if string(payload) != tc.WantPayload {
				t.Fatalf("unexpected canonical payload\nwant: %s\ngot:  %s", tc.WantPayload, payload)
			}
		})
	}

	for _, tc := range vectors.VerificationCases {
		t.Run("verify/"+tc.Name, func(t *testing.T) {
			entitlement := verifier.Verify([]byte(tc.License), vectors.CurrentFingerprint)
			if entitlement.State != tc.WantState {
				t.Fatalf("expected state %s, got state=%s reason=%s message=%s", tc.WantState, entitlement.State, entitlement.Reason, entitlement.Message)
			}
			if entitlement.Reason != tc.WantReason {
				t.Fatalf("expected reason %q, got %q", tc.WantReason, entitlement.Reason)
			}
			if entitlement.FingerprintMatched != tc.WantFingerprintMatched {
				t.Fatalf("expected fingerprintMatched=%v, got %v", tc.WantFingerprintMatched, entitlement.FingerprintMatched)
			}
			if entitlement.ClusterLimit() != tc.WantMaxClusters {
				t.Fatalf("expected maxClusters=%d, got %d", tc.WantMaxClusters, entitlement.ClusterLimit())
			}
		})
	}
}

func loadConformanceVectors(t *testing.T) conformanceVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/conformance/vectors.json")
	if err != nil {
		t.Fatalf("read conformance vectors: %v", err)
	}
	var vectors conformanceVectors
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("parse conformance vectors: %v", err)
	}
	return vectors
}

func conformanceTrustedKeys(t *testing.T, keys []conformanceKey) map[string]ed25519.PublicKey {
	t.Helper()
	out := make(map[string]ed25519.PublicKey, len(keys))
	for _, key := range keys {
		raw, err := base64.StdEncoding.DecodeString(key.PublicKeyBase64)
		if err != nil {
			t.Fatalf("decode public key %s: %v", key.KeyID, err)
		}
		if len(raw) != ed25519.PublicKeySize {
			t.Fatalf("public key %s has size %d", key.KeyID, len(raw))
		}
		out[key.KeyID] = ed25519.PublicKey(raw)
	}
	return out
}
