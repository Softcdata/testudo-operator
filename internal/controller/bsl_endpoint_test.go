package controller

import (
	"context"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveBSLEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		cluster *disasterv1.Cluster
		def     string
		want    string
		source  string
		wantErr bool
	}{
		{name: "cluster override wins", cluster: &disasterv1.Cluster{Spec: disasterv1.ClusterSpec{VeleroInstall: &disasterv1.VeleroInstallSpec{BSLEndpoint: "http://cluster:9000"}}}, def: "http://default:9000", want: "http://cluster:9000", source: "clusterOverride"},
		{name: "default fallback", cluster: &disasterv1.Cluster{}, def: "http://default:9000", want: "http://default:9000", source: "controllerDefault"},
		{name: "invalid override does not fallback", cluster: &disasterv1.Cluster{Spec: disasterv1.ClusterSpec{VeleroInstall: &disasterv1.VeleroInstallSpec{BSLEndpoint: "cluster:9000"}}}, def: "http://default:9000", wantErr: true},
		{name: "missing both", cluster: &disasterv1.Cluster{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, source, err := resolveBSLEndpoint(tt.cluster, tt.def)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tt.wantErr)
			}
			if err == nil && (got != tt.want || source != tt.source) {
				t.Fatalf("got endpoint=%q source=%q, want endpoint=%q source=%q", got, source, tt.want, tt.source)
			}
		})
	}
}

func TestDefaultBSLUsesClusterEndpointAndRepairsDrift(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	sr := &disasterv1.StorageRepository{ObjectMeta: metav1.ObjectMeta{Name: "repo", Namespace: "disaster-system"}, Spec: disasterv1.StorageRepositorySpec{Bucket: "bucket", Region: "region", Endpoint: "http://default:9000", AccessKey: "ak", SecretKey: "sk"}}
	cluster := &disasterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "same-city"}, Spec: disasterv1.ClusterSpec{VeleroInstall: &disasterv1.VeleroInstallSpec{BSLEndpoint: "http://cluster:9000"}}}
	bsl := &velerov1.BackupStorageLocation{ObjectMeta: metav1.ObjectMeta{Name: "repo-same-city", Namespace: VeleroNamespace}, Spec: velerov1.BackupStorageLocationSpec{Config: map[string]string{"s3Url": "http://wrong:9000", "region": "old"}, StorageType: velerov1.StorageType{ObjectStorage: &velerov1.ObjectStorageLocation{Bucket: "old", Prefix: "old"}}}}
	source := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sr).Build()
	target := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bsl).Build()
	if err := (&DefaultBSL{}).ApplyStorageRepositoryForCluster(context.Background(), source, target, cluster, sr, bsl.Name, "same-city"); err != nil {
		t.Fatalf("apply BSL: %v", err)
	}
	got := &velerov1.BackupStorageLocation{}
	if err := target.Get(context.Background(), types.NamespacedName{Name: bsl.Name, Namespace: VeleroNamespace}, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Config["s3Url"] != "http://cluster:9000" || got.Spec.Config["region"] != "region" || got.Spec.ObjectStorage.Bucket != "bucket" || got.Spec.ObjectStorage.Prefix != "same-city" {
		t.Fatalf("BSL was not fully reconciled: %#v", got.Spec)
	}
}

func TestValidateBSLEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "minio:9000", "http://", "ftp://minio:9000", "http://minio:9000/path?x=1", "http://user:pass@minio:9000"} {
		if err := validateBSLEndpoint(endpoint); err == nil {
			t.Errorf("validateBSLEndpoint(%q) succeeded, want error", endpoint)
		}
	}
	for _, endpoint := range []string{"http://minio:9000", "https://minio.example.com:9443"} {
		if err := validateBSLEndpoint(endpoint); err != nil {
			t.Errorf("validateBSLEndpoint(%q) failed: %v", endpoint, err)
		}
	}
}
