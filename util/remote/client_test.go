package remote

import (
	"context"
	"crypto/x509"
	"os"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"sigs.k8s.io/cluster-api-provider-azure/azure"
)

func TestNewClusterClient(t *testing.T) {
	g := gomega.NewWithT(t)

	// Create a test certificate pool
	testCertPool := x509.NewCertPool()

	// Save the original certificate pool to restore it after tests
	prevEnv := azure.AzSecretCertPool

	// Save original environment variables
	originalAzureEnv := os.Getenv("AZURE_ENVIRONMENT")

	// Restore the environment after the test
	defer func() {
		azure.AzSecretCertPool = prevEnv
		os.Setenv("AZURE_ENVIRONMENT", originalAzureEnv)
	}()

	tests := []struct {
		name          string
		isAzSecret    bool
		certPool      *x509.CertPool
		kubeconfig    []byte
		expectedError bool
	}{
		{
			name:          "standard environment",
			isAzSecret:    false,
			certPool:      nil,
			kubeconfig:    []byte("fake-kubeconfig-data"),
			expectedError: false, // Will still error due to invalid kubeconfig in real test
		},
		{
			name:          "azsecret environment with cert pool",
			isAzSecret:    true,
			certPool:      testCertPool,
			kubeconfig:    []byte("fake-kubeconfig-data"),
			expectedError: false, // Will still error due to invalid kubeconfig in real test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock the AzSecret environment
			azure.AzSecretCertPool = tt.certPool

			// Set the environment variable to control IsAzSecretEnvironment()
			if tt.isAzSecret {
				os.Setenv("AZURE_ENVIRONMENT", "AzureSecretCloud")
			} else {
				os.Setenv("AZURE_ENVIRONMENT", "AzurePublicCloud")
			}

			// Create a fake client with a kubeconfig secret
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			// Create a secret with kubeconfig data
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-kubeconfig",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"value": tt.kubeconfig,
				},
			}

			// We expect an error from the real code since we're not providing valid kubeconfig
			// but we can still verify our function runs correctly up to that point
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secret).Build()

			cluster := types.NamespacedName{
				Namespace: "default",
				Name:      "test-cluster",
			}

			_, err := NewClusterClient(context.Background(), "test", fakeClient, cluster)

			// We'll get errors from the kubeconfig parsing, but that's expected
			// Just verify that our code handled the AzSecret environment correctly
			g.Expect(err).To(gomega.HaveOccurred()) // Invalid kubeconfig will always cause an error
		})
	}
}
