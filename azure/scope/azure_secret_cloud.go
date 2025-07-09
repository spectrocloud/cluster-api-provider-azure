package scope

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	azurepkg "sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

const (
	AzureEnvironentFolderEnvName = "AZURE_ENVIRONMENT_FOLDER"
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
	log.Info("AzureSecretCloud initializing")
	defer done()
	path := os.Getenv(AzureEnvironentFolderEnvName)
	log.Info("Path is", path)
	if path == "" {
		return
	}
	files, err := os.ReadDir(path)
	if err != nil {
		log.Error(err, "error reading folder", "path", path)
		return
	}

	for _, file := range files {
		if !file.IsDir() && strings.EqualFold(filepath.Ext(file.Name()), ".json") {
			if env, err := azure.EnvironmentFromFile(filepath.Join(path, file.Name())); err == nil {
				azure.SetEnvironment(env.Name, env)
				log.Info("loaded Azure environment from file", "EnvName", env.Name)
				log.Info("loaded Azure environment from file", "filename", file.Name())
			} else {
				log.Error(err, "failed to load Azure environment from file", "filename", file.Name())
			}
		}
	}
}

// InitializeGlobalTransport initializes the global certificate pool and HTTP transport
// This should be called once during application startup after loading Azure environments
func InitializeGlobalTransport() error {
	globalTransportMutex.Lock()
	defer globalTransportMutex.Unlock()

	// Get certificate data from the environment or secret
	certData, err := getAzSecretCertificateData()
	if err != nil {
		return errors.Wrap(err, "failed to get certificate data")
	}

	// Initialize the global transport system
	return updateGlobalTransportLocked(certData)
}

// UpdateGlobalTransportWithCertificate updates the global transport with new certificate data
// This is called when certificates are discovered from identity secrets
func UpdateGlobalTransportWithCertificate(certData []byte) error {
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
			Proxy: http.ProxyFromEnvironment,
		}
		globalHTTPClient = &http.Client{
			Transport: globalTransport,
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
			RootCAs:            globalCertPool,
			InsecureSkipVerify: false,
		},
		Proxy: http.ProxyFromEnvironment,
	}

	// Create global HTTP client
	globalHTTPClient = &http.Client{
		Transport: globalTransport,
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

// getAzSecretCertificateData retrieves certificate data from environment or secrets
func getAzSecretCertificateData() ([]byte, error) {
	// Try multiple sources for certificate data, in order of preference:

	// 1. Environment variable pointing to certificate file
	certPath := os.Getenv("AZURE_SECRET_CERT_PATH")
	if certPath != "" {
		if certData, err := os.ReadFile(certPath); err == nil {
			return certData, nil
		}
	}

	// 2. Environment variable with certificate content directly
	certContent := os.Getenv("AZURE_SECRET_CERT_DATA")
	if certContent != "" {
		return []byte(certContent), nil
	}

	// 3. Check well-known certificate file locations
	wellKnownPaths := []string{
		"/home/ubuntu/combine-harbor-combined.crt",
	}

	for _, path := range wellKnownPaths {
		if certData, err := os.ReadFile(path); err == nil && len(certData) > 0 {
			return certData, nil
		}
	}

	// 4. No certificate found - this is fine for standard Azure environments
	return nil, nil
}
