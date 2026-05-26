/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MockCommandExecutor for testing
type MockCommandExecutor struct {
	CalledWith  [][]string
	ReturnError error
}

func (m *MockCommandExecutor) Run(name string, args ...string) error {
	m.CalledWith = append(m.CalledWith, append([]string{name}, args...))
	return m.ReturnError
}

// MockClient for testing client errors
type MockClient struct {
	client.Client
	MockUpdate      func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
	MockList        func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	MockGet         func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	MockDelete      func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
	MockCreate      func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	MockDeleteAllOf func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error
	MockStatus      func() client.StatusWriter
}

func (m *MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if m.MockUpdate != nil {
		return m.MockUpdate(ctx, obj, opts...)
	}
	return m.Client.Update(ctx, obj, opts...)
}

func (m *MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if m.MockCreate != nil {
		return m.MockCreate(ctx, obj, opts...)
	}
	return m.Client.Create(ctx, obj, opts...)
}

func (m *MockClient) Status() client.StatusWriter {
	if m.MockStatus != nil {
		return m.MockStatus()
	}
	return m.Client.Status()
}

type MockStatusWriter struct {
	client.StatusWriter
	MockUpdate func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error
}

func (m *MockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if m.MockUpdate != nil {
		return m.MockUpdate(ctx, obj, opts...)
	}
	return m.StatusWriter.Update(ctx, obj, opts...)
}

func (m *MockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if m.MockList != nil {
		return m.MockList(ctx, list, opts...)
	}
	return m.Client.List(ctx, list, opts...)
}

func (m *MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if m.MockGet != nil {
		return m.MockGet(ctx, key, obj, opts...)
	}
	return m.Client.Get(ctx, key, obj, opts...)
}

func (m *MockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if m.MockDelete != nil {
		return m.MockDelete(ctx, obj, opts...)
	}
	return m.Client.Delete(ctx, obj, opts...)
}

func (m *MockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	if m.MockDeleteAllOf != nil {
		return m.MockDeleteAllOf(ctx, obj, opts...)
	}
	return m.Client.DeleteAllOf(ctx, obj, opts...)
}

func GetKubeConfigFromRestConfig(restConfig *rest.Config) ([]byte, error) {
	clusters := make(map[string]*clientcmdapi.Cluster)
	clusters["default-cluster"] = &clientcmdapi.Cluster{
		Server:                   restConfig.Host,
		CertificateAuthorityData: restConfig.CAData,
		InsecureSkipTLSVerify:    restConfig.Insecure,
	}
	contexts := make(map[string]*clientcmdapi.Context)
	contexts["default-context"] = &clientcmdapi.Context{
		Cluster:  "default-cluster",
		AuthInfo: "default-user",
	}
	authinfos := make(map[string]*clientcmdapi.AuthInfo)
	authinfos["default-user"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: restConfig.CertData,
		ClientKeyData:         restConfig.KeyData,
		Token:                 restConfig.BearerToken,
		Username:              restConfig.Username,
		Password:              restConfig.Password,
	}
	clientConfig := clientcmdapi.Config{
		Kind:           "Config",
		APIVersion:     "v1",
		Clusters:       clusters,
		Contexts:       contexts,
		CurrentContext: "default-context",
		AuthInfos:      authinfos,
	}
	return clientcmd.Write(clientConfig)
}

// MockClientFactory for testing cross-cluster client creation
type MockClientFactory struct {
	MockClient client.Client
	MockError  error
}

func (f *MockClientFactory) GetKubeClient(ctx context.Context, cli client.Client, scheme *runtime.Scheme, clusterName string) (client.Client, error) {
	return f.MockClient, f.MockError
}
