package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	platformlicense "github.com/softcdata/testudo-operator/pkg/license"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "license" {
		usageAndExit()
	}
	var err error
	switch os.Args[2] {
	case "fingerprint":
		err = runFingerprint(os.Args[3:])
	case "install":
		err = runInstall(os.Args[3:])
	case "status":
		err = runStatus(os.Args[3:])
	default:
		usageAndExit()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runFingerprint(args []string) error {
	flags := flag.NewFlagSet("license fingerprint", flag.ExitOnError)
	namespace := flags.String("namespace", platformlicense.DefaultLicenseNamespace, "license namespace")
	kubeconfig := flags.String("kubeconfig", "", "path to kubeconfig")
	out := flags.String("out", "", "output fingerprint request JSON file")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cli, cfg, err := buildClient(*kubeconfig)
	if err != nil {
		return err
	}
	caBytes, err := caBundleFromConfig(cfg)
	if err != nil {
		return err
	}
	store := platformlicense.KubernetesStore{
		Client:    cli,
		Namespace: *namespace,
		CABundle:  caBytes,
	}
	fingerprint, err := store.Fingerprint(context.Background())
	if err != nil {
		return err
	}
	request := map[string]string{
		"product":            platformlicense.ProductName,
		"fingerprintVersion": platformlicense.FingerprintVersionK8SV1,
		"fingerprint":        fingerprint,
		"namespace":          strings.TrimSpace(*namespace),
		"generatedAt":        time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	payload, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(*out) != "" {
		if err := os.WriteFile(*out, append(payload, '\n'), 0o644); err != nil {
			return err
		}
	}
	fmt.Println(string(payload))
	return nil
}

func runInstall(args []string) error {
	flags := flag.NewFlagSet("license install", flag.ExitOnError)
	namespace := flags.String("namespace", platformlicense.DefaultLicenseNamespace, "license namespace")
	kubeconfig := flags.String("kubeconfig", "", "path to kubeconfig")
	file := flags.String("file", "", "license .lic file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*file) == "" {
		return fmt.Errorf("--file is required")
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	cli, _, err := buildClient(*kubeconfig)
	if err != nil {
		return err
	}

	ctx := context.Background()
	secret := platformlicense.BuildLicenseSecret(strings.TrimSpace(*namespace), raw)
	current := &corev1.Secret{}
	key := types.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}
	if err := cli.Get(ctx, key, current); err == nil {
		current.Type = secret.Type
		current.Labels = secret.Labels
		current.Data = secret.Data
		if err := cli.Update(ctx, current); err != nil {
			return err
		}
		fmt.Printf("updated license secret %s/%s\n", secret.Namespace, secret.Name)
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	if err := cli.Create(ctx, secret); err != nil {
		return err
	}
	fmt.Printf("created license secret %s/%s\n", secret.Namespace, secret.Name)
	return nil
}

func runStatus(args []string) error {
	flags := flag.NewFlagSet("license status", flag.ExitOnError)
	namespace := flags.String("namespace", platformlicense.DefaultLicenseNamespace, "license namespace")
	kubeconfig := flags.String("kubeconfig", "", "path to kubeconfig")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cli, _, err := buildClient(*kubeconfig)
	if err != nil {
		return err
	}
	configMap := &corev1.ConfigMap{}
	if err := cli.Get(context.Background(), types.NamespacedName{
		Namespace: strings.TrimSpace(*namespace),
		Name:      platformlicense.StatusConfigMapName,
	}, configMap); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(configMap.Data, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(payload))
	return nil
}

func buildClient(kubeconfig string) (client.Client, *rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if strings.TrimSpace(kubeconfig) != "" {
		loadingRules.ExplicitPath = strings.TrimSpace(kubeconfig)
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, nil, err
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}
	if err := disasterv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}
	cli, err := client.New(cfg, client.Options{Scheme: scheme})
	return cli, cfg, err
}

func caBundleFromConfig(cfg *rest.Config) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("kubeconfig is nil")
	}
	if len(cfg.CAData) > 0 {
		return cfg.CAData, nil
	}
	if strings.TrimSpace(cfg.CAFile) != "" {
		return os.ReadFile(cfg.CAFile)
	}
	return nil, fmt.Errorf("kubeconfig must contain certificate-authority-data or certificate-authority")
}

func usageAndExit() {
	fmt.Fprintln(os.Stderr, `usage:
  disasterctl license fingerprint [--namespace disaster-system] [--kubeconfig path] [--out fingerprint-request.json]
  disasterctl license install --file disaster-platform.lic [--namespace disaster-system] [--kubeconfig path]
  disasterctl license status [--namespace disaster-system] [--kubeconfig path]`)
	os.Exit(2)
}
