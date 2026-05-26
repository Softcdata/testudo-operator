package tools

import (
	"encoding/base64"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func GetRestConfig(kubeConfig []byte) (*rest.Config, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeConfig)
	if err != nil {
		return nil, err
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return restConfig, nil
}

// decodeTokenIfNeeded decodes Base64 token if it's not already a raw JWT
func decodeTokenIfNeeded(token string) string {
	// JWT tokens start with "eyJ" (base64 of '{"')
	if strings.HasPrefix(token, "eyJ") {
		return token // Already raw JWT
	}
	// Try to decode as Base64
	if decoded, err := base64.StdEncoding.DecodeString(token); err == nil {
		return string(decoded)
	}
	return token // Return as-is if decode fails
}

func GetRestConfigFromToken(endpoint, token string) (*rest.Config, error) {
	return &rest.Config{
		Host:            endpoint,
		BearerToken:     decodeTokenIfNeeded(token),
		TLSClientConfig: rest.TLSClientConfig{Insecure: true},
	}, nil
}

func GenerateKubeConfigFromToken(endpoint, token string) ([]byte, error) {
	config := clientcmdapi.NewConfig()

	cluster := clientcmdapi.NewCluster()
	cluster.Server = endpoint
	cluster.InsecureSkipTLSVerify = true
	config.Clusters["cluster"] = cluster

	authInfo := clientcmdapi.NewAuthInfo()
	authInfo.Token = decodeTokenIfNeeded(token)
	config.AuthInfos["user"] = authInfo

	context := clientcmdapi.NewContext()
	context.Cluster = "cluster"
	context.AuthInfo = "user"
	config.Contexts["context"] = context

	config.CurrentContext = "context"

	return clientcmd.Write(*config)
}
