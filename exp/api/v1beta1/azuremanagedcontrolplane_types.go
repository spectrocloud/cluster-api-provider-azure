/*
Copyright 2021 The Kubernetes Authors.

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

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

const (
	// PrivateDNSZoneModeSystem represents mode System for azuremanagedcontrolplane.
	PrivateDNSZoneModeSystem string = "System"

	// PrivateDNSZoneModeNone represents mode None for azuremanagedcontrolplane.
	PrivateDNSZoneModeNone string = "None"
)

// ManagedControlPlaneOutboundType enumerates the values for the managed control plane OutboundType.
type ManagedControlPlaneOutboundType string

const (
	// ManagedControlPlaneOutboundTypeLoadBalancer ...
	ManagedControlPlaneOutboundTypeLoadBalancer ManagedControlPlaneOutboundType = "loadBalancer"
	// ManagedControlPlaneOutboundTypeManagedNATGateway ...
	ManagedControlPlaneOutboundTypeManagedNATGateway ManagedControlPlaneOutboundType = "managedNATGateway"
	// ManagedControlPlaneOutboundTypeUserAssignedNATGateway ...
	ManagedControlPlaneOutboundTypeUserAssignedNATGateway ManagedControlPlaneOutboundType = "userAssignedNATGateway"
	// ManagedControlPlaneOutboundTypeUserDefinedRouting ...
	ManagedControlPlaneOutboundTypeUserDefinedRouting ManagedControlPlaneOutboundType = "userDefinedRouting"
)

// UpgradeChannel enumerates the values for upgrade channel.
type UpgradeChannel string

const (
	// UpgradeChannelNodeImage ...
	UpgradeChannelNodeImage UpgradeChannel = "node-image"
	// UpgradeChannelNone ...
	UpgradeChannelNone UpgradeChannel = "none"
	// UpgradeChannelPatch ...
	UpgradeChannelPatch UpgradeChannel = "patch"
	// UpgradeChannelRapid ...
	UpgradeChannelRapid UpgradeChannel = "rapid"
	// UpgradeChannelStable ...
	UpgradeChannelStable UpgradeChannel = "stable"
)

// KeyVaultNetworkAccessTypes enumerates the values for key vault network access types.
type KeyVaultNetworkAccessTypes string

const (
	// Private ...
	Private KeyVaultNetworkAccessTypes = "Private"
	// Public ...
	Public KeyVaultNetworkAccessTypes = "Public"
)

// AzureManagedControlPlaneSpec defines the desired state of AzureManagedControlPlane.
type AzureManagedControlPlaneSpec struct {
	// Version defines the desired Kubernetes version.
	// +kubebuilder:validation:MinLength:=2
	Version string `json:"version"`

	// ResourceGroupName is the name of the Azure resource group for this AKS Cluster.
	ResourceGroupName string `json:"resourceGroupName"`

	// NodeResourceGroupName is the name of the resource group
	// containing cluster IaaS resources. Will be populated to default
	// in webhook.
	// +optional
	NodeResourceGroupName string `json:"nodeResourceGroupName,omitempty"`

	// VirtualNetwork describes the vnet for the AKS cluster. Will be created if it does not exist.
	// +optional
	VirtualNetwork ManagedControlPlaneVirtualNetwork `json:"virtualNetwork,omitempty"`

	// SubscriptionID is the GUID of the Azure subscription to hold this cluster.
	// +optional
	SubscriptionID string `json:"subscriptionID,omitempty"`

	// Location is a string matching one of the canonical Azure region names. Examples: "westus2", "eastus".
	Location string `json:"location"`

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// AdditionalTags is an optional set of tags to add to Azure resources managed by the Azure provider, in addition to the
	// ones added by default.
	// +optional
	AdditionalTags infrav1.Tags `json:"additionalTags,omitempty"`

	// NetworkPlugin used for building Kubernetes network.
	// +kubebuilder:validation:Enum=azure;kubenet
	// +optional
	NetworkPlugin *string `json:"networkPlugin,omitempty"`

	// NetworkPolicy used for building Kubernetes network.
	// +kubebuilder:validation:Enum=azure;calico
	// +optional
	NetworkPolicy *string `json:"networkPolicy,omitempty"`

	// Outbound configuration used by Nodes.
	// +kubebuilder:validation:Enum=loadBalancer;managedNATGateway;userAssignedNATGateway;userDefinedRouting
	// +optional
	OutboundType *ManagedControlPlaneOutboundType `json:"outboundType,omitempty"`

	// DockerBridgeCidr - A CIDR notation IP range assigned to the Docker bridge network. It must not overlap with any Subnet IP ranges or the Kubernetes service address range.
	// +optional
	DockerBridgeCidr *string `json:"dockerBridgeCidr,omitempty"`

	// DNSPrefix - DNS prefix specified when creating the managed cluster.
	// +optional
	DNSPrefix *string `json:"dnsPrefix,omitempty"`

	// FqdnSubdomain - FQDN subdomain specified when creating private cluster with custom private dns zone.
	// +optional
	FqdnSubdomain *string `json:"fqdnSubdomain,omitempty"`

	// SSHPublicKey is a string literal containing an ssh public key base64 encoded.
	SSHPublicKey string `json:"sshPublicKey"`

	// DNSServiceIP is an IP address assigned to the Kubernetes DNS service.
	// It must be within the Kubernetes service address range specified in serviceCidr.
	// +optional
	DNSServiceIP *string `json:"dnsServiceIP,omitempty"`

	// LoadBalancerSKU is the SKU of the loadBalancer to be provisioned.
	// +kubebuilder:validation:Enum=Basic;Standard
	// +optional
	LoadBalancerSKU *string `json:"loadBalancerSKU,omitempty"`

	// IdentityRef is a reference to a AzureClusterIdentity to be used when reconciling this cluster
	// +optional
	IdentityRef *corev1.ObjectReference `json:"identityRef,omitempty"`

	// AadProfile is Azure Active Directory configuration to integrate with AKS for aad authentication.
	// +optional
	AADProfile *AADProfile `json:"aadProfile,omitempty"`

	// AddonProfiles are the profiles of managed cluster add-on.
	// +optional
	AddonProfiles []AddonProfile `json:"addonProfiles,omitempty"`

	// SKU is the SKU of the AKS to be provisioned.
	// +optional
	SKU *SKU `json:"sku,omitempty"`

	// LoadBalancerProfile is the profile of the cluster load balancer.
	// +optional
	LoadBalancerProfile *LoadBalancerProfile `json:"loadBalancerProfile,omitempty"`

	// APIServerAccessProfile is the access profile for AKS API server.
	// +optional
	APIServerAccessProfile *APIServerAccessProfile `json:"apiServerAccessProfile,omitempty"`

	// UserAssignedIdentities is a list of standalone Azure identities provided by the user to assign the cluster
	// +optional
	UserAssignedIdentities []infrav1.UserAssignedIdentity `json:"userAssignedIdentities,omitempty"`

	// AutoUpgradeProfile - Profile of auto upgrade configuration.
	// +optional
	AutoUpgradeProfile *ManagedClusterAutoUpgradeProfile `json:"autoUpgradeProfile,omitempty"`

	// SecurityProfile - Security profile for the managed cluster.
	// +optional
	SecurityProfile *ManagedClusterSecurityProfile `json:"securityProfile,omitempty"`

	// OidcIssuerProfile - The OIDC issuer profile of the Managed Cluster.
	// +optional
	OidcIssuerProfile *ManagedClusterOIDCIssuerProfile `json:"oidcIssuerProfile,omitempty"`

	// DisableLocalAccounts - If set to true, getting static credential will be disabled for this cluster. Expected to only be used for AAD clusters.
	// +optional
	DisableLocalAccounts *bool `json:"disableLocalAccounts,omitempty"`
}

// ManagedClusterOIDCIssuerProfile the OIDC issuer profile of the Managed Cluster.
type ManagedClusterOIDCIssuerProfile struct {
	// Enabled - Whether the OIDC issuer is enabled.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`
}

// ManagedClusterSecurityProfile security profile for the container service cluster.
type ManagedClusterSecurityProfile struct {
	// Defender - Microsoft Defender settings for the security profile.
	// +optional
	Defender *ManagedClusterSecurityProfileDefender `json:"defender,omitempty"`
	// WorkloadIdentity - [Workload Identity](https://azure.github.io/azure-workload-identity/docs/) settings for the security profile.
	// +optional
	WorkloadIdentity *ManagedClusterSecurityProfileWorkloadIdentity `json:"workloadIdentity,omitempty"`
}

// ManagedClusterSecurityProfileWorkloadIdentity workload Identity settings for the security profile.
type ManagedClusterSecurityProfileWorkloadIdentity struct {
	// Enabled - Whether to enable Workload Identity
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`
}

// ManagedClusterSecurityProfileDefender microsoft Defender settings for the security profile.
type ManagedClusterSecurityProfileDefender struct {
	// LogAnalyticsWorkspaceResourceID - Resource ID of the Log Analytics workspace to be associated with Microsoft Defender. When Microsoft Defender is enabled, this field is required and must be a valid workspace resource ID. When Microsoft Defender is disabled, leave the field empty.
	// +kubebuilder:validation:Required
	LogAnalyticsWorkspaceResourceID string `json:"logAnalyticsWorkspaceResourceId"`
	// SecurityMonitoring - Microsoft Defender threat detection for Cloud settings for the security profile.
	// +kubebuilder:validation:Required
	SecurityMonitoring *ManagedClusterSecurityProfileDefenderSecurityMonitoring `json:"securityMonitoring"`
}

// ManagedClusterSecurityProfileDefenderSecurityMonitoring microsoft Defender settings for the security
// profile threat detection.
type ManagedClusterSecurityProfileDefenderSecurityMonitoring struct {
	// Enabled - Whether to enable Defender threat detection
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`
}

// ManagedClusterAutoUpgradeProfile auto upgrade profile for a managed cluster.
type ManagedClusterAutoUpgradeProfile struct {
	// UpgradeChannel - upgrade channel for auto upgrade. Possible values include: "node-image","none","patch","rapid","stable"
	// +kubebuilder:validation:Enum=node-image;none;patch;rapid;stable
	// +kubebuilder:validation:Required
	UpgradeChannel UpgradeChannel `json:"upgradeChannel"`
}

// AADProfile - AAD integration managed by AKS.
type AADProfile struct {
	// Managed - Whether to enable managed AAD.
	// +kubebuilder:validation:Required
	Managed bool `json:"managed"`

	// AdminGroupObjectIDs - AAD group object IDs that will have admin role of the cluster.
	// +kubebuilder:validation:Required
	AdminGroupObjectIDs []string `json:"adminGroupObjectIDs"`
}

type AddonProfile struct {
	// Name- The name of managed cluster add-on.
	Name string `json:"name"`

	// Config - Key-value pairs for configuring an add-on.
	// +optional
	Config map[string]string `json:"config,omitempty"`

	// Enabled - Whether the add-on is enabled or not.
	Enabled bool `json:"enabled"`
}

// AzureManagedControlPlaneSkuTier - Tier of a managed cluster SKU.
// +kubebuilder:validation:Enum=Free;Paid
type AzureManagedControlPlaneSkuTier string

const (
	// FreeManagedControlPlaneTier is the free tier of AKS without corresponding SLAs.
	FreeManagedControlPlaneTier AzureManagedControlPlaneSkuTier = "Free"
	// PaidManagedControlPlaneTier is the paid tier of AKS with corresponding SLAs.
	PaidManagedControlPlaneTier AzureManagedControlPlaneSkuTier = "Paid"
)

// SKU - AKS SKU.
type SKU struct {
	// Tier - Tier of a managed cluster SKU.
	Tier AzureManagedControlPlaneSkuTier `json:"tier"`
}

// LoadBalancerProfile - Profile of the cluster load balancer.
type LoadBalancerProfile struct {
	// Load balancer profile must specify at most one of ManagedOutboundIPs, OutboundIPPrefixes and OutboundIPs.
	// By default the AKS cluster automatically creates a public IP in the AKS-managed infrastructure resource group and assigns it to the load balancer outbound pool.
	// Alternatively, you can assign your own custom public IP or public IP prefix at cluster creation time.
	// See https://docs.microsoft.com/en-us/azure/aks/load-balancer-standard#provide-your-own-outbound-public-ips-or-prefixes

	// ManagedOutboundIPs - Desired managed outbound IPs for the cluster load balancer.
	// +optional
	ManagedOutboundIPs *int32 `json:"managedOutboundIPs,omitempty"`

	// OutboundIPPrefixes - Desired outbound IP Prefix resources for the cluster load balancer.
	// +optional
	OutboundIPPrefixes []string `json:"outboundIPPrefixes,omitempty"`

	// OutboundIPs - Desired outbound IP resources for the cluster load balancer.
	// +optional
	OutboundIPs []string `json:"outboundIPs,omitempty"`

	// AllocatedOutboundPorts - Desired number of allocated SNAT ports per VM. Allowed values must be in the range of 0 to 64000 (inclusive). The default value is 0 which results in Azure dynamically allocating ports.
	// +optional
	AllocatedOutboundPorts *int32 `json:"allocatedOutboundPorts,omitempty"`

	// IdleTimeoutInMinutes - Desired outbound flow idle timeout in minutes. Allowed values must be in the range of 4 to 120 (inclusive). The default value is 30 minutes.
	// +optional
	IdleTimeoutInMinutes *int32 `json:"idleTimeoutInMinutes,omitempty"`
}

// APIServerAccessProfile - access profile for AKS API server.
type APIServerAccessProfile struct {
	// AuthorizedIPRanges - Authorized IP Ranges to kubernetes API server.
	// +optional
	AuthorizedIPRanges []string `json:"authorizedIPRanges,omitempty"`
	// EnablePrivateCluster - Whether to create the cluster as a private cluster or not.
	// +optional
	EnablePrivateCluster *bool `json:"enablePrivateCluster,omitempty"`
	// PrivateDNSZone - Private dns zone mode for private cluster.
	// +optional
	PrivateDNSZone *string `json:"privateDNSZone,omitempty"`
	// EnablePrivateClusterPublicFQDN - Whether to create additional public FQDN for private cluster or not.
	// +optional
	EnablePrivateClusterPublicFQDN *bool `json:"enablePrivateClusterPublicFQDN,omitempty"`
}

// ManagedControlPlaneVirtualNetwork describes a virtual network required to provision AKS clusters.
type ManagedControlPlaneVirtualNetwork struct {
	Name      string `json:"name"`
	CIDRBlock string `json:"cidrBlock"`
	// +optional
	Subnet ManagedControlPlaneSubnet `json:"subnet,omitempty"`
	// ResourceGroupName is the name of the Azure resource group for the VNet and Subnet.
	ResourceGroupName string `json:"resourceGroupName,omitempty"`
}

// ManagedControlPlaneSubnet describes a subnet for an AKS cluster.
type ManagedControlPlaneSubnet struct {
	Name      string `json:"name"`
	CIDRBlock string `json:"cidrBlock"`
}

// AzureManagedControlPlaneStatus defines the observed state of AzureManagedControlPlane.
type AzureManagedControlPlaneStatus struct {
	// Ready is true when the provider resource is ready.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Initialized is true when the control plane is available for initial contact.
	// This may occur before the control plane is fully ready.
	// In the AzureManagedControlPlane implementation, these are identical.
	// +optional
	Initialized bool `json:"initialized,omitempty"`

	// Conditions defines current service state of the AzureManagedControlPlane.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`

	// LongRunningOperationStates saves the states for Azure long-running operations so they can be continued on the
	// next reconciliation loop.
	// +optional
	LongRunningOperationStates infrav1.Futures `json:"longRunningOperationStates,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=azuremanagedcontrolplanes,scope=Namespaced,categories=cluster-api,shortName=amcp
// +kubebuilder:storageversion
// +kubebuilder:subresource:status

// AzureManagedControlPlane is the Schema for the azuremanagedcontrolplanes API.
type AzureManagedControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AzureManagedControlPlaneSpec   `json:"spec,omitempty"`
	Status AzureManagedControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AzureManagedControlPlaneList contains a list of AzureManagedControlPlane.
type AzureManagedControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AzureManagedControlPlane `json:"items"`
}

// GetConditions returns the list of conditions for an AzureManagedControlPlane API object.
func (m *AzureManagedControlPlane) GetConditions() clusterv1.Conditions {
	return m.Status.Conditions
}

// SetConditions will set the given conditions on an AzureManagedControlPlane object.
func (m *AzureManagedControlPlane) SetConditions(conditions clusterv1.Conditions) {
	m.Status.Conditions = conditions
}

// GetFutures returns the list of long running operation states for an AzureManagedControlPlane API object.
func (m *AzureManagedControlPlane) GetFutures() infrav1.Futures {
	return m.Status.LongRunningOperationStates
}

// SetFutures will set the given long running operation states on an AzureManagedControlPlane object.
func (m *AzureManagedControlPlane) SetFutures(futures infrav1.Futures) {
	m.Status.LongRunningOperationStates = futures
}

func init() {
	SchemeBuilder.Register(&AzureManagedControlPlane{}, &AzureManagedControlPlaneList{})
}
