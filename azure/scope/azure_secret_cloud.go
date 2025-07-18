package scope

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
		// If no environment folder is set, treat as Public/Gov cloud (no custom cert)
		return
	}
	files, err := os.ReadDir(path)
	if err != nil {
		log.Error(err, "error reading folder", "path", path)
		return
	}

	var certData []byte
	for _, file := range files {
		if !file.IsDir() {
			filePath := filepath.Join(path, file.Name())
			fileExt := filepath.Ext(file.Name())

			// Load Azure environment JSON files
			if strings.EqualFold(fileExt, ".json") {
				// Read and log the file contents for debugging
				if jsonData, err := os.ReadFile(filePath); err == nil {
					//fmt.Printf("CAPZ: Loading Azure environment JSON file: %s\n", file.Name())
					fmt.Printf("CAPZ: JSON file contents: %s\n", string(jsonData))
				}

				if env, err := azure.EnvironmentFromFile(filePath); err == nil {
					azure.SetEnvironment(env.Name, env)
					//fmt.Printf("CAPZ: Successfully loaded Azure environment: %s\n", env.Name)
					//fmt.Printf("CAPZ: ResourceManagerEndpoint: %s\n", env.ResourceManagerEndpoint)
					//fmt.Printf("CAPZ: ActiveDirectoryEndpoint: %s\n", env.ActiveDirectoryEndpoint)
					log.Info("loaded Azure environment from file", "EnvName", env.Name)
					log.Info("loaded Azure environment from file", "filename", file.Name())
				} else {
					fmt.Printf("CAPZ: Failed to load Azure environment from file %s: %v\n", file.Name(), err)
					log.Error(err, "failed to load Azure environment from file", "filename", file.Name())
				}
			}

			// Load certificate files
			if strings.EqualFold(fileExt, ".crt") || strings.EqualFold(fileExt, ".pem") {
				if data, err := os.ReadFile(filePath); err == nil && len(data) > 0 {
					certData = data
					fmt.Printf("CAPZ: Successfully loaded certificate from file: %s (%d bytes)\n", file.Name(), len(data))
					log.Info("loaded certificate from file", "filename", file.Name())
				} else {
					log.Error(err, "failed to load certificate from file", "filename", file.Name())
				}
			}
		}
	}

	// Initialize global transport with certificate data if found
	if len(certData) > 0 {
		if err := initializeGlobalTransportWithCertData(certData); err != nil {
			log.Error(err, "failed to initialize global transport with certificate")
		}
	} else {
		// If environment folder is set but no certificates found, log error
		log.Error(errors.New("no certificate files found in environment folder"), "expected certificate files (.crt or .pem) but none found", "path", path)
	}
	// If no certificate found in environment folder, treat as Public/Gov cloud (no custom cert)
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
