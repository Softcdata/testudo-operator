package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("DefaultBSL runtime settings", func() {
	It("Should align BSL path style and CA bundle with StorageRepository settings", func() {
		ctx := context.Background()
		cases := []struct {
			name            string
			style           disasterv1.StorageRepositoryAddressingStyle
			withCA          bool
			expectPathStyle string
			expectCACert    string
		}{
			{name: "path style without CA", style: disasterv1.StorageRepositoryAddressingStylePathStyle, expectPathStyle: "true"},
			{name: "path style with CA", style: disasterv1.StorageRepositoryAddressingStylePathStyle, withCA: true, expectPathStyle: "true", expectCACert: "ca-path"},
			{name: "virtual hosted without CA", style: disasterv1.StorageRepositoryAddressingStyleVirtualHostedStyle, expectPathStyle: "false"},
			{name: "virtual hosted with CA", style: disasterv1.StorageRepositoryAddressingStyleVirtualHostedStyle, withCA: true, expectPathStyle: "false", expectCACert: "ca-virtual"},
		}

		for _, tc := range cases {
			scheme := runtime.NewScheme()
			Expect(disasterv1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			Expect(velerov1.AddToScheme(scheme)).To(Succeed())

			sr := &disasterv1.StorageRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "repo",
					Namespace: "disaster-system",
				},
				Spec: disasterv1.StorageRepositorySpec{
					Bucket:          "bucket-a",
					Region:          "us-east-1",
					Endpoint:        "https://s3.example.com",
					AccessKey:       "ak",
					SecretKey:       "sk",
					AddressingStyle: tc.style,
				},
			}

			var sourceObjects []runtime.Object
			if tc.withCA {
				sr.Spec.CASecretRef = &corev1.LocalObjectReference{Name: "repo-ca"}
				sourceObjects = append(sourceObjects, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "repo-ca",
						Namespace: "disaster-system",
					},
					Data: map[string][]byte{
						disasterv1.StorageRepositoryCASecretKey: []byte(tc.expectCACert),
					},
				})
			}

			sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(sourceObjects...).Build()
			targetClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			bsl := &DefaultBSL{}
			err := bsl.ApplyStorageRepository(ctx, sourceClient, targetClient, sr, "repo-cluster-a", "cluster-a")
			Expect(err).To(MatchError("BackupStorageLocation repo-cluster-a is in Unavailable status"), tc.name)

			actual := &velerov1.BackupStorageLocation{}
			Expect(targetClient.Get(ctx, types.NamespacedName{Name: "repo-cluster-a", Namespace: VeleroNamespace}, actual)).To(Succeed(), tc.name)
			Expect(actual.Spec.Config["s3ForcePathStyle"]).To(Equal(tc.expectPathStyle), tc.name)
			Expect(string(actual.Spec.StorageType.ObjectStorage.CACert)).To(Equal(tc.expectCACert), tc.name)
		}
	})
})
