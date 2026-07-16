/*
Copyright 2019 The Kubernetes Authors.

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

package controllers

import (
	"context"
	"fmt"
	"math"

	"github.com/pkg/errors"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/aksextensions"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/fleetsmembers"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/groups"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/managedclusters"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/privateendpoints"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/resourcehealth"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/subnets"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/virtualnetworks"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

// azureManagedControlPlaneService contains the services required by the cluster controller.
type azureManagedControlPlaneService struct {
	kubeclient client.Client
	scope      managedclusters.ManagedClusterScope
	services   []azure.ServiceReconciler
}

// newAzureManagedControlPlaneReconciler populates all the services based on input scope.
func newAzureManagedControlPlaneReconciler(scope *scope.ManagedControlPlaneScope) (*azureManagedControlPlaneService, error) {
	resourceHealthSvc, err := resourcehealth.New(scope)
	if err != nil {
		return nil, err
	}
	return &azureManagedControlPlaneService{
		kubeclient: scope.Client,
		scope:      scope,
		services: []azure.ServiceReconciler{
			groups.New(scope),
			virtualnetworks.New(scope),
			subnets.New(scope),
			managedclusters.New(scope),
			privateendpoints.New(scope),
			fleetsmembers.New(scope),
			aksextensions.New(scope),
			resourceHealthSvc,
		},
	}, nil
}

// Reconcile reconciles all the services in a predetermined order.
func (r *azureManagedControlPlaneService) Reconcile(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "controllers.azureManagedControlPlaneService.Reconcile")
	defer done()

	for _, service := range r.services {
		if err := service.Reconcile(ctx); err != nil {
			return errors.Wrapf(err, "failed to reconcile AzureManagedControlPlane service %s", service.Name())
		}
	}

	if err := r.reconcileKubeconfig(ctx); err != nil {
		return errors.Wrap(err, "failed to reconcile kubeconfig secret")
	}

	return nil
}

// Pause pauses all components making up the cluster.
func (r *azureManagedControlPlaneService) Pause(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "controllers.azureManagedControlPlaneService.Pause")
	defer done()

	for _, service := range r.services {
		pauser, ok := service.(azure.Pauser)
		if !ok {
			continue
		}
		if err := pauser.Pause(ctx); err != nil {
			return errors.Wrapf(err, "failed to pause AzureManagedControlPlane service %s", service.Name())
		}
	}

	return nil
}

// Delete reconciles all the services in a predetermined order.
func (r *azureManagedControlPlaneService) Delete(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "controllers.azureManagedControlPlaneService.Delete")
	defer done()

	// Delete services in reverse order of creation.
	for i := len(r.services) - 1; i >= 0; i-- {
		if err := r.services[i].Delete(ctx); err != nil {
			return errors.Wrapf(err, "failed to delete AzureManagedControlPlane service %s", r.services[i].Name())
		}
	}

	return nil
}

func (r *azureManagedControlPlaneService) reconcileKubeconfig(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "controllers.azureManagedControlPlaneService.reconcileKubeconfig")
	defer done()

	kubeConfigs := [][]byte{r.scope.GetAdminKubeconfigData(), r.scope.GetUserKubeconfigData()}

	for i, kubeConfigData := range kubeConfigs {
		if len(kubeConfigData) == 0 {
			continue
		}

		// PATCH POINT: Inject custom CA certificate data into kubeconfig before storing in secret.
		// Only fires for the AzureSecret (air-gapped custom Azure) environment, where the global
		// AzSecret cert data is populated during client initialization; a no-op otherwise.
		logger := log.FromContext(ctx)
		logger.V(4).Info("Processing kubeconfig", "index", i, "type", map[int]string{0: "admin", 1: "user"}[i])
		logger.V(4).Info("Original kubeconfig size", "bytes", len(kubeConfigData))

		if customCACert := getCustomCACertificateForAMCP(r.scope.ClusterName()); customCACert != nil {
			logger.V(4).Info("Custom CA certificate found", "size_bytes", len(customCACert))

			// Parse kubeconfig
			kubeconfig, err := clientcmd.Load(kubeConfigData)
			if err != nil {
				logger.V(4).Error(err, "Failed to parse kubeconfig")
			} else {
				patchedCount := 0
				for clusterName, clusterInfo := range kubeconfig.Clusters {
					logger.V(4).Info("Processing cluster", "name", clusterName)
					logger.V(4).Info("Original CA data length", "bytes", len(clusterInfo.CertificateAuthorityData))

					if clusterInfo.CertificateAuthorityData != nil && len(clusterInfo.CertificateAuthorityData) > 0 {
						logger.V(4).Info("Combining original CA with custom CA")
						// Combine: original + newline + custom CA
						combinedCA := make([]byte, 0, len(clusterInfo.CertificateAuthorityData)+1+len(customCACert))
						combinedCA = append(combinedCA, clusterInfo.CertificateAuthorityData...)

						// Add newline separator if original doesn't end with newline
						if clusterInfo.CertificateAuthorityData[len(clusterInfo.CertificateAuthorityData)-1] != '\n' {
							combinedCA = append(combinedCA, '\n')
						}

						combinedCA = append(combinedCA, customCACert...)
						clusterInfo.CertificateAuthorityData = combinedCA

						logger.V(4).Info("Combined CA data",
							"total_bytes", len(combinedCA),
							"original_bytes", len(clusterInfo.CertificateAuthorityData)-len(customCACert)-1,
							"custom_bytes", len(customCACert))
					} else {
						logger.V(4).Info("No original CA data, using only custom CA")
						// No original CA, just use custom
						clusterInfo.CertificateAuthorityData = customCACert
						logger.V(4).Info("Set CA data length", "bytes", len(clusterInfo.CertificateAuthorityData))
					}
					patchedCount++
				}
				logger.V(4).Info("Combined CA certificates", "clusters_patched", patchedCount)

				// Write back the modified kubeconfig
				if patchedKubeconfig, err := clientcmd.Write(*kubeconfig); err != nil {
					logger.V(4).Error(err, "Failed to write patched kubeconfig")
				} else {
					kubeConfigData = patchedKubeconfig
					logger.V(4).Info("Successfully wrote combined kubeconfig", "bytes", len(kubeConfigData))
				}
			}
		} else {
			logger.V(4).Info("No custom CA certificate found, keeping original kubeconfig")
		}

		kubeConfigSecret := r.scope.MakeEmptyKubeConfigSecret()
		if i == 1 {
			// 2nd kubeconfig is the user kubeconfig
			kubeConfigSecret.Name = fmt.Sprintf("%s-user", kubeConfigSecret.Name)
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.kubeclient, &kubeConfigSecret, func() error {
			kubeConfigSecret.Data = map[string][]byte{
				secret.KubeconfigDataName: kubeConfigData,
			}

			// When upgrading from an older version of CAPI, the kubeconfig secret may not have the required
			// cluster name label. Add it here to avoid kubeconfig issues during upgrades.
			if _, ok := kubeConfigSecret.Labels[clusterv1.ClusterNameLabel]; !ok {
				if kubeConfigSecret.Labels == nil {
					kubeConfigSecret.Labels = make(map[string]string)
				}
				kubeConfigSecret.Labels[clusterv1.ClusterNameLabel] = r.scope.ClusterName()
			}
			return nil
		}); err != nil {
			return errors.Wrap(err, "failed to reconcile kubeconfig secret for cluster")
		}
	}

	// store cluster-info for the cluster with the admin kubeconfig.
	kubeconfigFile, err := clientcmd.Load(kubeConfigs[0])
	if err != nil {
		return errors.Wrap(err, "failed to turn aks credentials into kubeconfig file struct")
	}

	cluster := kubeconfigFile.Contexts[kubeconfigFile.CurrentContext].Cluster
	caData := kubeconfigFile.Clusters[cluster].CertificateAuthorityData
	caSecret := r.scope.MakeClusterCA()
	if _, err := controllerutil.CreateOrUpdate(ctx, r.kubeclient, caSecret, func() error {
		caSecret.Data = map[string][]byte{
			secret.TLSCrtDataName: caData,
			secret.TLSKeyDataName: []byte("foo"),
		}
		return nil
	}); err != nil {
		return errors.Wrapf(err, "failed to reconcile certificate authority data secret for cluster")
	}

	if err := r.scope.StoreClusterInfo(ctx, caData); err != nil {
		return errors.Wrap(err, "failed to construct cluster-info")
	}

	return nil
}

// getCustomCACertificateForAMCP returns custom CA certificate data for Azure Managed Control Plane clusters
// This function leverages the same certificate that is used for Azure authentication
// by checking the global AzSecretCertPool that gets populated during Azure client initialization
func getCustomCACertificateForAMCP(clusterName string) []byte {
	logger := log.FromContext(context.Background()) // Changed to context.Background() to avoid context leak
	logger.V(4).Info("getCustomCACertificateForAMCP called", "cluster_name", clusterName)

	// Debug the condition checks
	isConfigured := azure.IsAzSecretCertConfigured()
	poolNotNil := azure.AzSecretCertPool != nil
	dataLength := len(azure.AzSecretCertData)

	logger.V(4).Info("Condition checks",
		"is_configured", isConfigured,
		"pool_not_nil", poolNotNil,
		"data_length", dataLength)

	// Check if we have a certificate in the global AzSecretCertPool
	// This is the same certificate pool used for Azure authentication
	if azure.IsAzSecretCertConfigured() && azure.AzSecretCertPool != nil {
		logger.V(4).Info("Passed first condition check (IsConfigured && PoolNotNil)")
		// Return the raw certificate data stored in AzSecretCertData
		// This contains the original PEM data that was used to populate the certificate pool
		if len(azure.AzSecretCertData) > 0 {
			logger.V(4).Info("Found certificate data, returning", "bytes", len(azure.AzSecretCertData))
			logger.V(4).Info("Certificate data preview (first 100 chars)", "preview",
				string(azure.AzSecretCertData[:int(math.Min(100, float64(len(azure.AzSecretCertData))))]))
			return azure.AzSecretCertData
		}
		logger.V(4).Info("Certificate data is empty, returning nil")
	} else {
		logger.V(4).Info("Failed condition check",
			"is_configured", isConfigured,
			"pool_not_nil", poolNotNil)
	}

	logger.V(4).Info("getCustomCACertificateForAMCP returning nil")
	return nil
}
