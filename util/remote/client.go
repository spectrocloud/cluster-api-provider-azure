package remote

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/cluster-api-provider-azure/azure"
)

// NewClusterClient creates a new client to access a remote cluster using kubeconfig secret
// stored in the management cluster. For AzSecret environments, it adds the certificate from
// the global AzSecretCertPool to the client's transport.
func NewClusterClient(ctx context.Context, sourceName string, c client.Client, cluster client.ObjectKey) (client.Client, error) {
	// Get the kubeconfig bytes from the secret
	kubeconfigBytes, err := kubeconfig.FromSecret(ctx, c, cluster)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve kubeconfig secret for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	// Create the REST config
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create REST configuration for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	restConfig.UserAgent = remote.DefaultClusterAPIUserAgent(sourceName)
	customCertsCount := len(azure.AzSecretCertPool.Subjects())
	fmt.Printf("Certificate counts - AzSecretCertPool: %d\n",
		customCertsCount)
	// Check if we're in an AzSecret environment and have certificates in the global pool
	//isAzSecret := azure.IsAzSecretEnvironment()
	if azure.AzSecretCertPool != nil {
		fmt.Printf("Using AzSecretCertPool for remote cluster client: %s/%s\n", cluster.Namespace, cluster.Name)

		// Create a custom HTTP transport with the certificate pool
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            azure.AzSecretCertPool,
				InsecureSkipVerify: false, // Ensure we validate certificates
			},
			// Use default proxy and other settings
			Proxy: http.ProxyFromEnvironment,
		}

		// Set the transport in the REST config
		restConfig.Transport = transport // transport implements http.RoundTripper
		restConfig.TLSClientConfig = rest.TLSClientConfig{
			CAData: nil, // We'll use the transport's RootCAs instead
		}
	}

	// Create and return the client
	ret, err := client.New(restConfig, client.Options{Scheme: c.Scheme()})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create client for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	return ret, nil
}
