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

package managedclusters

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2021-05-01/containerservice"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/async"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/token"
	"sigs.k8s.io/cluster-api-provider-azure/util/reconciler"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

const (
	serviceName   = "managedcluster"
	aadResourceId = "6dae42f8-4368-4678-94ff-3960e28e3630"
)

// ManagedClusterScope defines the scope interface for a managed cluster.
type ManagedClusterScope interface {
	azure.Authorizer
	azure.AsyncStatusUpdater
	ManagedClusterSpec(context.Context) azure.ResourceSpecGetter
	SetControlPlaneEndpoint(clusterv1.APIEndpoint)
	MakeEmptyKubeConfigSecret() corev1.Secret
	GetAdminKubeConfigData() []byte
	SetAdminKubeConfigData([]byte)
	GetUserKubeConfigData() []byte
	SetUserKubeConfigData([]byte)
	IsAadEnabled() bool
	IsLocalAcountsDisabled() bool
}

// Service provides operations on azure resources.
type Service struct {
	Scope ManagedClusterScope
	async.Reconciler
	CredentialGetter
}

// New creates a new service.
func New(scope ManagedClusterScope) *Service {
	client := newClient(scope)
	return &Service{
		Scope:            scope,
		Reconciler:       async.New(scope, client, client),
		CredentialGetter: client,
	}
}

// Name returns the service name.
func (s *Service) Name() string {
	return serviceName
}

// Reconcile idempotently creates or updates a managed cluster, if possible.
func (s *Service) Reconcile(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "managedclusters.Service.Reconcile")
	defer done()

	ctx, cancel := context.WithTimeout(ctx, reconciler.DefaultAzureServiceReconcileTimeout)
	defer cancel()

	managedClusterSpec := s.Scope.ManagedClusterSpec(ctx)
	if managedClusterSpec == nil {
		return nil
	}

	result, resultErr := s.CreateResource(ctx, managedClusterSpec, serviceName)
	if resultErr == nil {
		managedCluster, ok := result.(containerservice.ManagedCluster)
		if !ok {
			return errors.Errorf("%T is not a containerservice.ManagedCluster", result)
		}
		// Update control plane endpoint.
		endpoint := clusterv1.APIEndpoint{
			Host: to.String(managedCluster.ManagedClusterProperties.Fqdn),
			Port: 443,
		}
		s.Scope.SetControlPlaneEndpoint(endpoint)

		// Update kubeconfig data
		// Always fetch credentials in case of rotation
		adminKubeConfigData, userKubeConfigData, err := s.ReconcileKubeconfig(ctx, managedClusterSpec)
		if err != nil {
			return errors.Wrap(err, "error while reconciling adminKubeConfigData")
		}

		s.Scope.SetAdminKubeConfigData(adminKubeConfigData)
		s.Scope.SetUserKubeConfigData(userKubeConfigData)
	}
	s.Scope.UpdatePutStatus(infrav1.ManagedClusterRunningCondition, serviceName, resultErr)
	return resultErr
}

// Delete deletes the managed cluster.
func (s *Service) Delete(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "managedclusters.Service.Delete")
	defer done()

	ctx, cancel := context.WithTimeout(ctx, reconciler.DefaultAzureServiceReconcileTimeout)
	defer cancel()

	managedClusterSpec := s.Scope.ManagedClusterSpec(ctx)
	if managedClusterSpec == nil {
		return nil
	}

	err := s.DeleteResource(ctx, managedClusterSpec, serviceName)
	s.Scope.UpdateDeleteStatus(infrav1.ManagedClusterRunningCondition, serviceName, err)
	return err
}

// IsManaged returns always returns true as CAPZ does not support BYO managed cluster.
func (s *Service) IsManaged(ctx context.Context) (bool, error) {
	return true, nil
}

func (s *Service) ReconcileKubeconfig(ctx context.Context, managedClusterSpec azure.ResourceSpecGetter) ([]byte, []byte, error) {
	var (
		userKubeConfigData  []byte
		adminKubeConfigData []byte
		err                 error
	)

	if s.Scope.IsAadEnabled() {
		if userKubeConfigData, err = s.GetUserKubeConfigData(ctx, managedClusterSpec); err != nil {
			return nil, nil, errors.Wrap(err, "error while trying to get user kubeconfig")
		}
	}

	if s.Scope.IsLocalAcountsDisabled() {
		userKubeconfigWithToken, err := s.GetUserKubeConfigWithToken(userKubeConfigData, ctx, managedClusterSpec)
		if err != nil {
			return nil, nil, errors.Wrap(err, "error while trying to get user kubeconfig with token")
		}
		return userKubeconfigWithToken, userKubeConfigData, nil
	}

	adminKubeConfigData, err = s.GetCredentials(ctx, managedClusterSpec.ResourceGroupName(), managedClusterSpec.ResourceName())
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get credentials for managed cluster")
	}
	return adminKubeConfigData, userKubeConfigData, nil
}

func (s *Service) GetUserKubeConfigData(ctx context.Context, managedClusterSpec azure.ResourceSpecGetter) ([]byte, error) {
	kubeConfigData, err := s.GetUserCredentials(ctx, managedClusterSpec.ResourceGroupName(), managedClusterSpec.ResourceName())
	if err != nil {
		return nil, errors.Wrap(err, "failed to get credentials for managed cluster")
	}
	return kubeConfigData, nil
}

func (s *Service) GetUserKubeConfigWithToken(userKubeConfigData []byte, ctx context.Context, managedClusterSpec azure.ResourceSpecGetter) ([]byte, error) {

	tokenClient, err := token.NewClient(s.Scope)
	if err != nil {
		return nil, errors.Wrap(err, "error while getting aad token client")
	}

	token, err := tokenClient.GetAzureActiveDirectoryToken(ctx, aadResourceId)
	if err != nil {
		return nil, errors.Wrap(err, "error while getting aad token for user kubeconfig")
	}

	return s.CreateUserKubeconfigWithToken(token, userKubeConfigData)
}

func (s *Service) CreateUserKubeconfigWithToken(token string, userKubeConfigData []byte) ([]byte, error) {
	config, err := clientcmd.Load(userKubeConfigData)
	if err != nil {
		return nil, errors.Wrap(err, "error while trying to unmarshal new user kubeconfig with token")
	}
	for _, auth := range config.AuthInfos {
		auth.Token = token
		auth.Exec = nil
	}
	kubeconfig, err := clientcmd.Write(*config)
	if err != nil {
		return nil, errors.Wrap(err, "error while trying to marshal new user kubeconfig with token")
	}
	return kubeconfig, nil
}
