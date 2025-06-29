/*
Copyright 2020 The Kubernetes Authors.

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

package scope

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"reflect"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/tracing/azotel"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

// AzureSecretKey is the value for they client secret key.
const AzureSecretKey = "clientSecret"

// AzureSecretCertKey is the key for the Azure Secret certificate in the Secret.
const AzureSecretCertKey = "azureSecretCert"

// CredentialsProvider defines the behavior for azure identity based credential providers.
type CredentialsProvider interface {
	GetClientID() string
	GetClientSecret(ctx context.Context) (string, error)
	GetTenantID() string
	GetTokenCredential(ctx context.Context, resourceManagerEndpoint, activeDirectoryEndpoint, tokenAudience string) (azcore.TokenCredential, error)
	Type() infrav1.IdentityType
	GetAzSecretCertificate(ctx context.Context) ([]byte, error)
}

// AzureCredentialsProvider represents a credential provider with azure cluster identity.
type AzureCredentialsProvider struct {
	Client   client.Client
	Identity *infrav1.AzureClusterIdentity

	cache azure.CredentialCache
}

// NewAzureCredentialsProvider creates a new AzureClusterCredentialsProvider from the supplied inputs.
func NewAzureCredentialsProvider(ctx context.Context, cache azure.CredentialCache, kubeClient client.Client, identityRef *corev1.ObjectReference, defaultNamespace string) (*AzureCredentialsProvider, error) {
	if identityRef == nil {
		return nil, errors.New("failed to generate new AzureClusterCredentialsProvider from empty identityName")
	}

	// if the namespace isn't specified then assume it's in the same namespace as the AzureCluster
	namespace := identityRef.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	identity := &infrav1.AzureClusterIdentity{}
	key := client.ObjectKey{Name: identityRef.Name, Namespace: namespace}
	if err := kubeClient.Get(ctx, key, identity); err != nil {
		return nil, errors.Errorf("failed to retrieve AzureClusterIdentity external object %q/%q: %v", key.Namespace, key.Name, err)
	}

	return &AzureCredentialsProvider{
		Client:   kubeClient,
		Identity: identity,
		cache:    cache,
	}, nil
}

// GetTokenCredential returns an Azure TokenCredential based on the provided azure identity.
func (p *AzureCredentialsProvider) GetTokenCredential(ctx context.Context, resourceManagerEndpoint, activeDirectoryEndpoint, tokenAudience string) (azcore.TokenCredential, error) {
	ctx, log, done := tele.StartSpanWithLogger(ctx, "azure.scope.AzureCredentialsProvider.GetTokenCredential")
	defer done()

	var authErr error
	var cred azcore.TokenCredential

	tracingProvider := azotel.NewTracingProvider(otel.GetTracerProvider(), nil)

	// Check if we're using AzSecret cloud and set the correct endpoints
	isAzSecret := false

	// Check if we're using resource manager endpoint for AzSecret
	if strings.Contains(resourceManagerEndpoint, "scombine.scloud") {
		isAzSecret = true
		log.Info("Detected AzSecret cloud environment, using custom endpoints")

		// Override the AD endpoint with the one from AzureSecretConfig
		activeDirectoryEndpoint = azure.AzureSecretConfig.ActiveDirectoryAuthorityHost

		log.Info("Using AzSecret AD endpoint",
			"endpoint", activeDirectoryEndpoint)
	}

	// If this is AzSecret, get the certificate for TLS configuration
	var certPool *x509.CertPool
	if isAzSecret {
		// Get the certificate from palette-fusion Secret
		azSecretCert, err := p.GetAzSecretCertificate(ctx)
		if err != nil {
			log.Error(err, "Failed to get AzSecret certificate")
			return nil, errors.Wrap(err, "failed to get AzSecret certificate")
		}

		if len(azSecretCert) > 0 {
			log.Info("Retrieved AzSecret certificate from identity Secret",
				"certLength", len(azSecretCert))

			// Create a cert pool and add the certificate
			systemPool, err := x509.SystemCertPool()
			if err != nil {
				log.Info("Failed to get system cert pool, creating new one")
				certPool = x509.NewCertPool()
			} else {
				certPool = systemPool
			}

			if ok := certPool.AppendCertsFromPEM(azSecretCert); !ok {
				log.Error(errors.New("failed to append certificate"), "Failed to append AzSecret certificate to pool")
				return nil, errors.New("failed to append AzSecret certificate to pool")
			}
			log.Info("Successfully added certificate from identity Secret to pool")

			// Store the certificate in the global pool for other clients to use
			azure.AzSecretCertPool = certPool
			log.Info("Stored certificate in global AzSecretCertPool for use by all Azure clients")

			// Also store the raw certificate data for kubeconfig injection
			azure.AzSecretCertData = azSecretCert
			log.Info("Stored raw certificate data in global AzSecretCertData for kubeconfig injection")
		} else {
			log.Info("No AzSecret certificate found in identity Secret")
		}
	}

	switch p.Identity.Spec.Type {
	case infrav1.WorkloadIdentity:
		cred, authErr = p.cache.GetOrStoreWorkloadIdentity(&azidentity.WorkloadIdentityCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				TracingProvider: tracingProvider,
			},
			TenantID:      p.Identity.Spec.TenantID,
			ClientID:      p.Identity.Spec.ClientID,
			TokenFilePath: GetProjectedTokenPath(),
		})

	case infrav1.ManualServicePrincipal:
		log.Info("Identity type ManualServicePrincipal is deprecated and will be removed in a future release. See https://capz.sigs.k8s.io/topics/identities to find a supported identity type.")
		fallthrough
	case infrav1.ServicePrincipal:
		clientSecret, err := p.GetClientSecret(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get client secret")
		}

		options := azidentity.ClientSecretCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				TracingProvider: tracingProvider,
				Cloud: cloud.Configuration{
					ActiveDirectoryAuthorityHost: activeDirectoryEndpoint,
					Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
						cloud.ResourceManager: {
							Audience: tokenAudience,
							Endpoint: resourceManagerEndpoint,
						},
					},
				},
			},
		}

		// For AzSecret environments, we also need to set DisableInstanceDiscovery
		if isAzSecret {
			options.DisableInstanceDiscovery = true
			log.Info("Disabled instance discovery for AzSecret environment")

			// Configure TLS with the certificate if available
			if certPool != nil {
				log.Info("Configuring transport for ClientSecretCredential with certificate from palette-fusion",
					"transportConfigured", true)
				options.ClientOptions.Transport = &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							RootCAs:            certPool,
							InsecureSkipVerify: false, // Explicitly set to false to ensure certificate verification
						},
						// Use default proxy and other settings
						Proxy: http.ProxyFromEnvironment,
					},
				}
			}
		}

		cred, authErr = p.cache.GetOrStoreClientSecret(p.GetTenantID(), p.Identity.Spec.ClientID, clientSecret, &options)

	case infrav1.ServicePrincipalCertificate:
		var certsContent []byte
		if p.Identity.Spec.CertPath != "" {
			var err error
			certsContent, err = os.ReadFile(p.Identity.Spec.CertPath)
			if err != nil {
				return nil, errors.Wrap(err, "failed to read certificate file")
			}
		} else {
			clientSecret, err := p.GetClientSecret(ctx)
			if err != nil {
				return nil, errors.Wrap(err, "failed to get client secret")
			}
			certsContent = []byte(clientSecret)
		}

		certOptions := &azidentity.ClientCertificateCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				TracingProvider: tracingProvider,
				Cloud: cloud.Configuration{
					ActiveDirectoryAuthorityHost: activeDirectoryEndpoint,
					Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
						cloud.ResourceManager: {
							Audience: tokenAudience,
							Endpoint: resourceManagerEndpoint,
						},
					},
				},
			},
		}

		// For AzSecret environments, disable instance discovery
		if isAzSecret {
			certOptions.DisableInstanceDiscovery = true
			log.Info("Disabled instance discovery for AzSecret environment with certificate")

			// Configure TLS with the certificate if available
			if certPool != nil {
				log.Info("Configuring transport for ClientCertificateCredential with certificate from palette-fusion",
					"transportConfigured", true)
				certOptions.ClientOptions.Transport = &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							RootCAs:            certPool,
							InsecureSkipVerify: false, // Explicitly set to false to ensure certificate verification
						},
						// Use default proxy and other settings
						Proxy: http.ProxyFromEnvironment,
					},
				}
			}
		}

		cred, authErr = p.cache.GetOrStoreClientCert(p.GetTenantID(), p.Identity.Spec.ClientID, certsContent, nil, certOptions)

	case infrav1.UserAssignedMSI:
		options := azidentity.ManagedIdentityCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				TracingProvider: tracingProvider,
				Cloud: cloud.Configuration{
					ActiveDirectoryAuthorityHost: activeDirectoryEndpoint,
					Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
						cloud.ResourceManager: {
							Audience: tokenAudience,
							Endpoint: resourceManagerEndpoint,
						},
					},
				},
			},
			ID: azidentity.ClientID(p.Identity.Spec.ClientID),
		}

		// For AzSecret environments, configure TLS with custom certificate
		if isAzSecret && certPool != nil {
			log.Info("Configuring transport for ManagedIdentityCredential with certificate from palette-fusion",
				"transportConfigured", true)
			options.ClientOptions.Transport = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs:            certPool,
						InsecureSkipVerify: false, // Explicitly set to false to ensure certificate verification
					},
					// Use default proxy and other settings
					Proxy: http.ProxyFromEnvironment,
				},
			}
		}

		cred, authErr = p.cache.GetOrStoreManagedIdentity(&options)

	default:
		return nil, errors.Errorf("identity type %s not supported", p.Identity.Spec.Type)
	}

	if authErr != nil {
		return nil, errors.Errorf("failed to create credential: %v", authErr)
	}

	return cred, nil
}

// GetClientID returns the Client ID associated with the AzureCredentialsProvider's Identity.
func (p *AzureCredentialsProvider) GetClientID() string {
	return p.Identity.Spec.ClientID
}

// GetClientSecret returns the Client Secret associated with the AzureCredentialsProvider's Identity.
// NOTE: this only works if the Identity references a Service Principal Client Secret.
// If using another type of credentials, such a Certificate, we return an empty string.
func (p *AzureCredentialsProvider) GetClientSecret(ctx context.Context) (string, error) {
	if p.hasClientSecret() {
		secretRef := p.Identity.Spec.ClientSecret
		key := types.NamespacedName{
			Namespace: secretRef.Namespace,
			Name:      secretRef.Name,
		}
		secret := &corev1.Secret{}

		if err := p.Client.Get(ctx, key, secret); err != nil {
			return "", errors.Wrap(err, "Unable to fetch ClientSecret")
		}
		return string(secret.Data[AzureSecretKey]), nil
	}
	return "", nil
}

// GetTenantID returns the Tenant ID associated with the AzureCredentialsProvider's Identity.
func (p *AzureCredentialsProvider) GetTenantID() string {
	return p.Identity.Spec.TenantID
}

// Type returns the auth mechanism used.
func (p *AzureCredentialsProvider) Type() infrav1.IdentityType {
	return p.Identity.Spec.Type
}

// hasClientSecret returns true if the identity has a Service Principal Client Secret.
// This does not include managed identities.
func (p *AzureCredentialsProvider) hasClientSecret() bool {
	switch p.Identity.Spec.Type {
	case infrav1.ServicePrincipal, infrav1.ManualServicePrincipal:
		return true
	case infrav1.ServicePrincipalCertificate:
		return p.Identity.Spec.CertPath == ""
	default:
		return false
	}
}

// IsClusterNamespaceAllowed indicates if the cluster namespace is allowed.
func IsClusterNamespaceAllowed(ctx context.Context, k8sClient client.Client, allowedNamespaces *infrav1.AllowedNamespaces, namespace string) bool {
	if allowedNamespaces == nil {
		return false
	}

	// empty value matches with all namespaces
	if reflect.DeepEqual(*allowedNamespaces, infrav1.AllowedNamespaces{}) {
		return true
	}

	for _, v := range allowedNamespaces.NamespaceList {
		if v == namespace {
			return true
		}
	}

	// Check if clusterNamespace is in the namespaces selected by the identity's allowedNamespaces selector.
	namespaces := &corev1.NamespaceList{}
	selector, err := metav1.LabelSelectorAsSelector(allowedNamespaces.Selector)
	if err != nil {
		return false
	}

	// If a Selector has a nil or empty selector, it should match nothing.
	if selector.Empty() {
		return false
	}

	if err := k8sClient.List(ctx, namespaces, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return false
	}

	for _, n := range namespaces.Items {
		if n.Name == namespace {
			return true
		}
	}

	return false
}

// GetAzSecretCertificate fetches the Azure Secret certificate from the Secret when using AzSecret cloud.
func (p *AzureCredentialsProvider) GetAzSecretCertificate(ctx context.Context) ([]byte, error) {
	ctx, log, done := tele.StartSpanWithLogger(ctx, "azure.scope.AzureCredentialsProvider.GetAzSecretCertificate")
	defer done()

	// Simply check the identity's referenced Secret for the certificate
	// This is the same Secret that contains the clientSecret
	if p.Identity.Spec.ClientSecret.Name == "" {
		log.Info("No ClientSecret reference set in AzureClusterIdentity")
		return nil, nil
	}

	secretRef := p.Identity.Spec.ClientSecret
	key := types.NamespacedName{
		Namespace: secretRef.Namespace,
		Name:      secretRef.Name,
	}

	log.Info("Checking identity Secret for certificate",
		"secretName", secretRef.Name,
		"namespace", secretRef.Namespace)

	secret := &corev1.Secret{}
	if err := p.Client.Get(ctx, key, secret); err != nil {
		log.Error(err, "Unable to fetch identity Secret")
		return nil, errors.Wrap(err, "Unable to fetch identity Secret")
	}

	// Look for the certificate in the Secret (checking the standard key first)
	if certData, ok := secret.Data["azureSecretCert"]; ok && len(certData) > 0 {
		log.Info("Found certificate in identity Secret",
			"key", "azureSecretCert",
			"certLength", len(certData))
		return certData, nil
	}

	// Certificate not found in standard key, try common alternatives
	log.Info("Certificate not found under 'azureSecretCert' key, checking alternatives")
	certKeys := []string{"ca.crt", "tls.crt", "certificate", "cert"}
	for _, keyName := range certKeys {
		if certData, ok := secret.Data[keyName]; ok && len(certData) > 0 {
			log.Info("Found certificate using alternative key",
				"key", keyName,
				"certLength", len(certData))
			return certData, nil
		}
	}

	log.Info("No certificate found in identity Secret")
	return nil, nil
}
