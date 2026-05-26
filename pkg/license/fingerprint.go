package license

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
)

type FingerprintInputs struct {
	KubeSystemUID        string
	PlatformNamespaceUID string
	APIServerCASHA256    string
	InstallID            string
}

func ComputeK8SV1Fingerprint(inputs FingerprintInputs) (string, error) {
	values := []string{
		strings.TrimSpace(inputs.KubeSystemUID),
		strings.TrimSpace(inputs.PlatformNamespaceUID),
		strings.TrimSpace(inputs.APIServerCASHA256),
		strings.TrimSpace(inputs.InstallID),
	}
	for i, value := range values {
		if value == "" {
			return "", fmt.Errorf("fingerprint input %d is empty", i)
		}
	}
	payload := strings.Join([]string{
		"disaster-platform:k8s-v1",
		values[0],
		values[1],
		values[2],
		values[3],
	}, "\n")
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func HashAPIServerCABundle(bundle []byte) (string, error) {
	certs, err := parseCertificateBundle(bundle)
	if err != nil {
		return "", err
	}
	if len(certs) == 0 {
		return "", fmt.Errorf("CA bundle does not contain X.509 certificates")
	}

	derHashes := make([]string, 0, len(certs))
	for _, cert := range certs {
		sum := sha256.Sum256(cert.Raw)
		derHashes = append(derHashes, hex.EncodeToString(sum[:]))
	}
	sort.Strings(derHashes)
	joined := strings.Join(derHashes, "\n")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:]), nil
}

func parseCertificateBundle(bundle []byte) ([]*x509.Certificate, error) {
	if len(bundle) == 0 {
		return nil, fmt.Errorf("CA bundle is empty")
	}

	var certs []*x509.Certificate
	remaining := bundle
	for {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		remaining = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PEM certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	if len(certs) > 0 {
		return certs, nil
	}

	parsed, err := x509.ParseCertificates(bundle)
	if err == nil && len(parsed) > 0 {
		return parsed, nil
	}
	cert, singleErr := x509.ParseCertificate(bundle)
	if singleErr == nil {
		return []*x509.Certificate{cert}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("parse DER certificate bundle: %w", err)
	}
	return nil, singleErr
}
