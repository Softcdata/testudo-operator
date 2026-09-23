package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func resolveBSLEndpoint(cluster *disasterv1.Cluster, defaultEndpoint string) (string, string, error) {
	if cluster == nil {
		return "", "", fmt.Errorf("target cluster context is required to resolve BSL endpoint")
	}
	override := ""
	if cluster.Spec.VeleroInstall != nil {
		override = strings.TrimSpace(cluster.Spec.VeleroInstall.BSLEndpoint)
	}
	if override != "" {
		if err := validateBSLEndpoint(override); err != nil {
			return "", "", fmt.Errorf("cluster %q bslEndpoint is invalid: %w", cluster.Name, err)
		}
		return override, "clusterOverride", nil
	}
	defaultEndpoint = strings.TrimSpace(defaultEndpoint)
	if err := validateBSLEndpoint(defaultEndpoint); err != nil {
		return "", "", fmt.Errorf("BSL controller default endpoint is invalid: %w", err)
	}
	return defaultEndpoint, "controllerDefault", nil
}

func validateBSLEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("endpoint is empty")
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("endpoint must be an absolute URL with scheme and host")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("endpoint must not contain user info, query, or fragment")
	}
	return nil
}

type StorageRuntimeSettings struct {
	Endpoint       string
	EndpointSource string
	Region         string
	UsePathStyle   bool
	CACert         []byte
}

func resolveStorageRuntimeSettings(ctx context.Context, reader client.Reader, sr *disasterv1.StorageRepository) (StorageRuntimeSettings, error) {
	settings := StorageRuntimeSettings{
		Endpoint:     sr.Spec.Endpoint,
		Region:       sr.Spec.Region,
		UsePathStyle: sr.Spec.UsesPathStyle(),
	}

	if sr.Spec.CASecretRef == nil || sr.Spec.CASecretRef.Name == "" {
		return settings, nil
	}

	secret := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{
		Name:      sr.Spec.CASecretRef.Name,
		Namespace: sr.Namespace,
	}, secret); err != nil {
		return settings, fmt.Errorf("failed to load CA secret %s/%s: %w", sr.Namespace, sr.Spec.CASecretRef.Name, err)
	}

	caBundle, ok := secret.Data[disasterv1.StorageRepositoryCASecretKey]
	if !ok || len(caBundle) == 0 {
		return settings, fmt.Errorf("CA secret %s/%s does not contain %s", sr.Namespace, sr.Spec.CASecretRef.Name, disasterv1.StorageRepositoryCASecretKey)
	}

	settings.CACert = append([]byte(nil), caBundle...)
	return settings, nil
}

func buildStorageHTTPClient(caBundle []byte) (*http.Client, error) {
	if len(caBundle) == 0 {
		return nil, nil
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if ok := rootCAs.AppendCertsFromPEM(caBundle); !ok {
		return nil, fmt.Errorf("failed to append custom CA bundle")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.RootCAs = rootCAs

	return &http.Client{Transport: transport}, nil
}

func desiredBSLConfig(settings StorageRuntimeSettings) map[string]string {
	return map[string]string{
		"region":           settings.Region,
		"s3Url":            settings.Endpoint,
		"s3ForcePathStyle": strconv.FormatBool(settings.UsePathStyle),
	}
}
