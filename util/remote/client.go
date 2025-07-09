package remote

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/cluster-api-provider-azure/azure"
)

// NewClusterClient creates a new client to access a remote cluster using kubeconfig secret
// stored in the management cluster. It automatically uses the global transport configuration
// which includes any custom certificates if available.
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

	// Use centralized transport configuration
	if globalClient := azure.GetGlobalHTTPClient(); globalClient != nil {
		restConfig.Transport = globalClient.Transport
		fmt.Printf("Using centralized transport for remote cluster client: %s/%s\n", cluster.Namespace, cluster.Name)
	}

	// Create and return the client
	ret, err := client.New(restConfig, client.Options{Scheme: c.Scheme()})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create client for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	return ret, nil
}
