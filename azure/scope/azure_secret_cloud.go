package scope

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	azurepkg "sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ConfigMap names for Azure secret cloud configuration
	AzureEnvConfigMapName  = "azure-capz-env-config"
	AzureCertConfigMapName = "azure-capz-cert-config"

	// ConfigMap keys
	AzureEnvConfigKey  = "azure-capz-env.json"
	AzureCertConfigKey = "azure-ca.crt"
)

var (
	// Global certificate pool for custom Azure environments
	globalCertPool *x509.CertPool
	// Global HTTP transport for custom Azure environments
	globalTransport *http.Transport
	// Global HTTP client for custom Azure environments
	globalHTTPClient *http.Client
	// Mutex to protect concurrent access to global transport resources
	globalTransportMutex sync.RWMutex
)

func init() {
	_, log, done := tele.StartSpanWithLogger(context.Background(), "scope.AzureScope.init")
	defer done()

	log.Info("Azure secret cloud init completed - ConfigMap initialization will be performed during controller setup")
}

// InitializeAzureConfigForCluster initializes Azure environment and certificates from ConfigMaps
// in the specified cluster namespace
func InitializeAzureConfigForCluster(ctx context.Context, kubeClient client.Client, namespace, azureEnvironment string) error {
	_, log, done := tele.StartSpanWithLogger(ctx, "scope.InitializeAzureConfigForCluster")
	defer done()

	log.Info("Initializing Azure config for cluster", "namespace", namespace, "azureEnvironment", azureEnvironment)

	// Try to read environment ConfigMap from cluster namespace
	envConfigMap := &corev1.ConfigMap{}
	envKey := client.ObjectKey{Namespace: namespace, Name: AzureEnvConfigMapName}

	if err := kubeClient.Get(ctx, envKey, envConfigMap); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Azure environment ConfigMap not found in cluster namespace, using default configuration", "namespace", namespace)
			return nil // No custom config, use defaults
		}
		return errors.Wrap(err, "failed to read Azure environment ConfigMap from cluster namespace")
	}

	// ConfigMap found, process environment JSON
	envJSON, hasEnvJSON := envConfigMap.Data[AzureEnvConfigKey]
	if !hasEnvJSON || envJSON == "" {
		return errors.New("Azure environment ConfigMap found but JSON data is missing")
	}

	log.Info("Processing Azure environment JSON from cluster ConfigMap")
	if err := processAzureEnvironmentJSON(envJSON); err != nil {
		return errors.Wrap(err, "failed to process Azure environment JSON")
	}

	// Try to read certificate ConfigMap from cluster namespace
	certConfigMap := &corev1.ConfigMap{}
	certKey := client.ObjectKey{Namespace: namespace, Name: AzureCertConfigMapName}

	if err := kubeClient.Get(ctx, certKey, certConfigMap); err != nil {
		if apierrors.IsNotFound(err) {
			return errors.New("Azure environment ConfigMap found but certificate ConfigMap is missing - custom Azure environments require certificates")
		}
		return errors.Wrap(err, "failed to read Azure certificate ConfigMap from cluster namespace")
	}

	// Certificate ConfigMap found, process certificate data
	certPEM, hasCertPEM := certConfigMap.Data[AzureCertConfigKey]
	if !hasCertPEM || certPEM == "" {
		return errors.New("Azure certificate ConfigMap found but certificate data is missing")
	}

	certData := []byte(certPEM)
	log.Info("Successfully loaded certificate from cluster ConfigMap", "bytes", len(certData))

	// Initialize global transport with certificate data
	if err := initializeGlobalTransportWithCertData(certData); err != nil {
		return errors.Wrap(err, "failed to initialize global transport with certificate")
	}

	log.Info("Azure config initialization completed successfully for cluster", "namespace", namespace)
	return nil
}

// processAzureEnvironmentJSON processes the Azure environment JSON configuration
func processAzureEnvironmentJSON(envJSON string) error {
	// Create a temporary file for azure.EnvironmentFromFile to read
	tmpFile, err := os.CreateTemp("", "azure-env-*.json")
	if err != nil {
		return errors.Wrap(err, "failed to create temporary file for Azure environment")
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write JSON content to temporary file
	if _, err := tmpFile.WriteString(envJSON); err != nil {
		return errors.Wrap(err, "failed to write Azure environment JSON to temporary file")
	}

	// Parse Azure environment from file
	env, err := azure.EnvironmentFromFile(tmpFile.Name())
	if err != nil {
		return errors.Wrap(err, "failed to parse Azure environment JSON")
	}

	azure.SetEnvironment(env.Name, env)
	fmt.Printf("CAPZ: Loaded Azure environment: %s\n", env.Name)
	return nil
}

// initializeDefaultTransport initializes the default transport for public clouds
func initializeDefaultTransport() error {
	globalTransportMutex.Lock()
	defer globalTransportMutex.Unlock()

	return updateGlobalTransportLocked(nil)
}

// initializeGlobalTransportWithCertData initializes the global transport with provided certificate data
// This is called during init() when certificate files are found in the environment folder
func initializeGlobalTransportWithCertData(certData []byte) error {
	globalTransportMutex.Lock()
	defer globalTransportMutex.Unlock()

	return updateGlobalTransportLocked(certData)
}

// updateGlobalTransportLocked updates the global transport with certificate data
// This function assumes the caller holds the globalTransportMutex lock
func updateGlobalTransportLocked(certData []byte) error {
	if len(certData) == 0 {
		// No custom certificate, use system defaults
		globalCertPool = nil
		globalTransport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		globalHTTPClient = &http.Client{
			Transport: globalTransport,
			Timeout:   60 * time.Second,
		}
		azurepkg.GlobalHTTPClient = globalHTTPClient
		return nil
	}

	// Create certificate pool with custom certificate
	systemPool, err := x509.SystemCertPool()
	if err != nil {
		globalCertPool = x509.NewCertPool()
	} else {
		globalCertPool = systemPool
	}

	if ok := globalCertPool.AppendCertsFromPEM(certData); !ok {
		return errors.New("failed to append certificate to pool")
	}

	// Create global transport with certificate pool
	globalTransport = &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			RootCAs:            globalCertPool,
			InsecureSkipVerify: false,
		},
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Create global HTTP client
	globalHTTPClient = &http.Client{
		Transport: globalTransport,
		Timeout:   60 * time.Second,
	}

	// Store in azure package globals for backward compatibility
	azurepkg.AzSecretCertPool = globalCertPool
	azurepkg.AzSecretCertData = certData
	azurepkg.GlobalHTTPClient = globalHTTPClient

	return nil
}

// GetGlobalCertPool returns the global certificate pool for custom Azure environments
func GetGlobalCertPool() *x509.CertPool {
	globalTransportMutex.RLock()
	defer globalTransportMutex.RUnlock()
	return globalCertPool
}

// GetGlobalTransport returns the global HTTP transport for custom Azure environments
func GetGlobalTransport() *http.Transport {
	globalTransportMutex.RLock()
	defer globalTransportMutex.RUnlock()
	return globalTransport
}

// GetGlobalHTTPClient returns the global HTTP client for custom Azure environments
func GetGlobalHTTPClient() *http.Client {
	globalTransportMutex.RLock()
	defer globalTransportMutex.RUnlock()
	return globalHTTPClient
}

// ConfigureAzureClientOptions configures Azure SDK client options with global transport
func ConfigureAzureClientOptions(opts *azcore.ClientOptions) {
	globalTransportMutex.RLock()
	defer globalTransportMutex.RUnlock()

	if globalHTTPClient != nil {
		opts.Transport = globalHTTPClient
	}
}

// ConfigureARMClientOptions configures ARM client options with global transport
func ConfigureARMClientOptions(opts *arm.ClientOptions) {
	globalTransportMutex.RLock()
	defer globalTransportMutex.RUnlock()

	if globalHTTPClient != nil {
		opts.ClientOptions.Transport = globalHTTPClient
	}
}

// ConfigureAzIdentityOptions configures Azure Identity options with global transport
func ConfigureAzIdentityOptions(opts *azcore.ClientOptions) {
	globalTransportMutex.RLock()
	defer globalTransportMutex.RUnlock()

	if globalHTTPClient != nil {
		opts.Transport = globalHTTPClient
	}
}

// ConfigureRestConfig configures Kubernetes REST config with global transport
func ConfigureRestConfig(config *rest.Config) {
	globalTransportMutex.RLock()
	defer globalTransportMutex.RUnlock()

	if globalTransport != nil {
		config.Transport = globalTransport
	}
}
