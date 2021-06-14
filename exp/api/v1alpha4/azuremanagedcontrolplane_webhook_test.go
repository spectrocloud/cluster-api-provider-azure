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

package v1alpha4

import (
	"testing"

	"github.com/Azure/go-autorest/autorest/to"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func TestDefaultingWebhook(t *testing.T) {
	g := NewWithT(t)

	t.Logf("Testing amcp defaulting webhook with no baseline")
	amcp := &AzureManagedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fooName",
		},
		Spec: AzureManagedControlPlaneSpec{
			ResourceGroupName: "fooRg",
			Location:          "fooLocation",
			Version:           "1.17.5",
		},
	}
	amcp.Default()
	g.Expect(*amcp.Spec.NetworkPlugin).To(Equal("azure"))
	g.Expect(*amcp.Spec.LoadBalancerSKU).To(Equal("Standard"))
	g.Expect(*amcp.Spec.NetworkPolicy).To(Equal("calico"))
	g.Expect(amcp.Spec.Version).To(Equal("v1.17.5"))
	g.Expect(amcp.Spec.SSHPublicKey).NotTo(BeEmpty())
	g.Expect(amcp.Spec.NodeResourceGroupName).To(Equal("MC_fooRg_fooName_fooLocation"))
	g.Expect(amcp.Spec.VirtualNetwork.Name).To(Equal("fooName"))
	g.Expect(amcp.Spec.VirtualNetwork.Subnet.Name).To(Equal("fooName"))

	t.Logf("Testing amcp defaulting webhook with baseline")
	netPlug := "kubenet"
	lbSKU := "Basic"
	netPol := "azure"
	amcp.Spec.NetworkPlugin = &netPlug
	amcp.Spec.LoadBalancerSKU = &lbSKU
	amcp.Spec.NetworkPolicy = &netPol
	amcp.Spec.Version = "9.99.99"
	amcp.Spec.SSHPublicKey = ""
	amcp.Spec.NodeResourceGroupName = "fooNodeRg"
	amcp.Spec.VirtualNetwork.Name = "fooVnetName"
	amcp.Spec.VirtualNetwork.Subnet.Name = "fooSubnetName"
	amcp.Default()
	g.Expect(*amcp.Spec.NetworkPlugin).To(Equal(netPlug))
	g.Expect(*amcp.Spec.LoadBalancerSKU).To(Equal(lbSKU))
	g.Expect(*amcp.Spec.NetworkPolicy).To(Equal(netPol))
	g.Expect(amcp.Spec.Version).To(Equal("v9.99.99"))
	g.Expect(amcp.Spec.SSHPublicKey).NotTo(BeEmpty())
	g.Expect(amcp.Spec.NodeResourceGroupName).To(Equal("fooNodeRg"))
	g.Expect(amcp.Spec.VirtualNetwork.Name).To(Equal("fooVnetName"))
	g.Expect(amcp.Spec.VirtualNetwork.Subnet.Name).To(Equal("fooSubnetName"))
}

func TestValidatingWebhook(t *testing.T) {
	tests := []struct {
		name      string
		amcp      AzureManagedControlPlane
		expectErr bool
	}{
		{
			name: "Testing valid DNSServiceIP",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: pointer.StringPtr("192.168.0.0"),
					Version:      "v1.17.8",
				},
			},
			expectErr: false,
		},
		{
			name: "Testing invalid DNSServiceIP",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: pointer.StringPtr("192.168.0.0.3"),
					Version:      "v1.17.8",
				},
			},
			expectErr: true,
		},
		{
			name: "Invalid Version",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: pointer.StringPtr("192.168.0.0"),
					Version:      "honk",
				},
			},
			expectErr: true,
		},
		{
			name: "not following the kuberntes Version pattern",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: pointer.StringPtr("192.168.0.0"),
					Version:      "1.19.0",
				},
			},
			expectErr: true,
		},
		{
			name: "Version not set",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: pointer.StringPtr("192.168.0.0"),
					Version:      "",
				},
			},
			expectErr: true,
		},
		{
			name: "Valid Version",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: pointer.StringPtr("192.168.0.0"),
					Version:      "v1.17.8",
				},
			},
			expectErr: false,
		},
		{
			name: "Valid Managed AADProfile",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.17.8",
					AADProfile: &ManagedClusterAADProfile{
						Managed: to.BoolPtr(true),
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "Invalid Managed AADProfile AdminGroupObjectIDs not provided",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.17.8",
					AADProfile: &ManagedClusterAADProfile{
						Managed:             to.BoolPtr(true),
						AdminGroupObjectIDs: &[]string{},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "Invalid Managed AADProfile AdminGroupObjectIDs nil",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.17.8",
					AADProfile: &ManagedClusterAADProfile{
						Managed: to.BoolPtr(true),
					},
				},
			},
			expectErr: true,
		},
		{
			name: "Invalid Managed AADProfile, did not set managed field",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.17.8",
					AADProfile: &ManagedClusterAADProfile{
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "Valid Legacy AADProfile",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.17.8",
					AADProfile: &ManagedClusterAADProfile{
						ClientAppID:     to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppID:     to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppSecret: to.StringPtr("286******************"),
						TenantID:        to.StringPtr("678977a8-5db7-4c98-b856-b34619afg75h"),
					},
				},
			},
			expectErr: false,
		},
		{
			name: "Invalid Legacy AADProfile, missing Legacy AAD fields",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.17.8",
					AADProfile: &ManagedClusterAADProfile{
						ClientAppID: to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
						TenantID:    to.StringPtr("678977a8-5db7-4c98-b856-b34619afg75h"),
					},
				},
			},
			expectErr: true,
		},
		{
			name: "Invalid AADProfile, conflicting values",
			amcp: AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.17.8",
					AADProfile: &ManagedClusterAADProfile{
						Managed: to.BoolPtr(true),
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
						ClientAppID:     to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppID:     to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppSecret: to.StringPtr("286******************"),
						TenantID:        to.StringPtr("678977a8-5db7-4c98-b856-b34619afg75h"),
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			t.Parallel()

			if tt.expectErr {
				g.Expect(tt.amcp.ValidateCreate()).NotTo(Succeed())
			} else {
				g.Expect(tt.amcp.ValidateCreate()).To(Succeed())
			}
		})
	}
}

func TestAzureManagedControlPlane_ValidateCreate(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name     string
		amcp     *AzureManagedControlPlane
		wantErr  bool
		errorLen int
	}{
		{
			name: "all valid",
			amcp: createAzureManagedControlPlane(t, "192.168.0.0", "v1.18.0", generateSSHPublicKey(true), &ManagedClusterAADProfile{
				Managed: to.BoolPtr(true),
				AdminGroupObjectIDs: &[]string{
					"616077a8-5db7-4c98-b856-b34619afg75h",
				},
			}),
			wantErr: false,
		},
		{
			name: "all valid",
			amcp: createAzureManagedControlPlane(t, "192.168.0.0", "v1.18.0", generateSSHPublicKey(true), &ManagedClusterAADProfile{
				ClientAppID:     to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
				ServerAppID:     to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
				ServerAppSecret: to.StringPtr("286******************"),
				TenantID:        to.StringPtr("678977a8-5db7-4c98-b856-b34619afg75h"),
			}),
			wantErr: false,
		},
		{
			name:     "invalid DNSServiceIP",
			amcp:     createAzureManagedControlPlane(t, "192.168.0.0.3", "v1.18.0", generateSSHPublicKey(true), nil),
			wantErr:  true,
			errorLen: 1,
		},
		{
			name:     "invalid sshKey",
			amcp:     createAzureManagedControlPlane(t, "192.168.0.0", "v1.18.0", generateSSHPublicKey(false), nil),
			wantErr:  true,
			errorLen: 1,
		},
		{
			name:     "invalid sshKey with a simple text and invalid DNSServiceIP",
			amcp:     createAzureManagedControlPlane(t, "192.168.0.0.3", "v1.18.0", "invalid_sshkey_honk", nil),
			wantErr:  true,
			errorLen: 2,
		},
		{
			name:     "invalid version",
			amcp:     createAzureManagedControlPlane(t, "192.168.0.0", "honk.version", generateSSHPublicKey(true), nil),
			wantErr:  true,
			errorLen: 1,
		},
		{
			name: "invalid legacy AADProfile",
			amcp: createAzureManagedControlPlane(t, "192.168.0.0", "v1.18.0", generateSSHPublicKey(true), &ManagedClusterAADProfile{
				ClientAppID: to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
				ServerAppID: to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
			}),
			wantErr:  true,
			errorLen: 1,
		},
		{
			name: "invalid managed AADProfile",
			amcp: createAzureManagedControlPlane(t, "192.168.0.0", "v1.18.0", generateSSHPublicKey(true), &ManagedClusterAADProfile{
				Managed: to.BoolPtr(true),
			}),
			wantErr:  true,
			errorLen: 1,
		},
		{
			name: "all invalid version",
			amcp: createAzureManagedControlPlane(t, "192.168.0.0.5", "honk.version", "invalid_sshkey_honk", &ManagedClusterAADProfile{
				ClientAppID:     to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
				ServerAppID:     to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
				ServerAppSecret: to.StringPtr("286******************"),
			}),
			wantErr:  true,
			errorLen: 4,
		},
		{
			name: "all invalid version",
			amcp: createAzureManagedControlPlane(t, "192.168.0.0.5", "honk.version", "invalid_sshkey_honk", &ManagedClusterAADProfile{
				AdminGroupObjectIDs: &[]string{
					"616077a8-5db7-4c98-b856-b34619afg75h",
				},
			}),
			wantErr:  true,
			errorLen: 4,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.amcp.ValidateCreate()
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(HaveLen(tc.errorLen))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestAzureManagedControlPlane_ValidateUpdate(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name    string
		oldAMCP *AzureManagedControlPlane
		amcp    *AzureManagedControlPlane
		wantErr bool
	}{
		{
			name:    "AzureManagedControlPlane with valid SSHPublicKey",
			oldAMCP: createAzureManagedControlPlane(t, "192.168.0.0", "v1.18.0", "", nil),
			amcp:    createAzureManagedControlPlane(t, "192.168.0.0", "v1.18.0", generateSSHPublicKey(true), nil),
			wantErr: false,
		},
		{
			name:    "AzureManagedControlPlane with invalid SSHPublicKey",
			oldAMCP: createAzureManagedControlPlane(t, "192.168.0.0", "v1.18.0", "", nil),
			amcp:    createAzureManagedControlPlane(t, "192.168.0.0", "v1.18.0", generateSSHPublicKey(false), nil),
			wantErr: true,
		},
		{
			name:    "AzureManagedControlPlane with invalid serviceIP",
			oldAMCP: createAzureManagedControlPlane(t, "", "v1.18.0", "", nil),
			amcp:    createAzureManagedControlPlane(t, "192.168.0.0.3", "v1.18.0", generateSSHPublicKey(true), nil),
			wantErr: true,
		},
		{
			name:    "AzureManagedControlPlane with invalid version",
			oldAMCP: createAzureManagedControlPlane(t, "", "v1.18.0", "", nil),
			amcp:    createAzureManagedControlPlane(t, "192.168.0.0", "1.999.9", generateSSHPublicKey(true), nil),
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane SubscriptionID is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:   to.StringPtr("192.168.0.0"),
					SubscriptionID: "212ec1q8",
					Version:        "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:   to.StringPtr("192.168.0.0"),
					SubscriptionID: "212ec1q9",
					Version:        "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane ResourceGroupName is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:      to.StringPtr("192.168.0.0"),
					ResourceGroupName: "hello-1",
					Version:           "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:      to.StringPtr("192.168.0.0"),
					ResourceGroupName: "hello-2",
					Version:           "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane NodeResourceGroupName is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:          to.StringPtr("192.168.0.0"),
					NodeResourceGroupName: "hello-1",
					Version:               "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:          to.StringPtr("192.168.0.0"),
					NodeResourceGroupName: "hello-2",
					Version:               "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane Location is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					Location:     "westeurope",
					Version:      "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					Location:     "eastus",
					Version:      "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane SSHPublicKey is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					SSHPublicKey: generateSSHPublicKey(true),
					Version:      "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					SSHPublicKey: generateSSHPublicKey(true),
					Version:      "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane DNSServiceIP is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					Version:      "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.1"),
					Version:      "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane DNSServiceIP is immutable, unsetting is not allowed",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					Version:      "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane NetworkPlugin is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:  to.StringPtr("192.168.0.0"),
					NetworkPlugin: to.StringPtr("azure"),
					Version:       "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:  to.StringPtr("192.168.0.0"),
					NetworkPlugin: to.StringPtr("kubenet"),
					Version:       "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane NetworkPlugin is immutable, unsetting is not allowed",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:  to.StringPtr("192.168.0.0"),
					NetworkPlugin: to.StringPtr("azure"),
					Version:       "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					Version:      "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane NetworkPolicy is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:  to.StringPtr("192.168.0.0"),
					NetworkPolicy: to.StringPtr("azure"),
					Version:       "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:  to.StringPtr("192.168.0.0"),
					NetworkPolicy: to.StringPtr("calico"),
					Version:       "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane NetworkPolicy is immutable, unsetting is not allowed",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:  to.StringPtr("192.168.0.0"),
					NetworkPolicy: to.StringPtr("azure"),
					Version:       "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					Version:      "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane LoadBalancerSKU is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:    to.StringPtr("192.168.0.0"),
					LoadBalancerSKU: to.StringPtr("Standard"),
					Version:         "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:    to.StringPtr("192.168.0.0"),
					LoadBalancerSKU: to.StringPtr("Basic"),
					Version:         "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane LoadBalancerSKU is immutable, unsetting is not allowed",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP:    to.StringPtr("192.168.0.0"),
					LoadBalancerSKU: to.StringPtr("Standard"),
					Version:         "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					Version:      "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane DefaultPoolRef.Name is immutable",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DefaultPoolRef: v1.LocalObjectReference{
						Name: "pool-1",
					},
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					Version:      "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					DefaultPoolRef: v1.LocalObjectReference{
						Name: "pool-2",
					},
					DNSServiceIP: to.StringPtr("192.168.0.0"),
					Version:      "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane Managed Aad can be set after cluster creation",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						Managed: to.BoolPtr(true),
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "AzureManagedControlPlane Managed Aad cannot be disabled",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						Managed: to.BoolPtr(true),
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version:    "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{},
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane Managed Aad cannot be disabled",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						Managed: to.BoolPtr(true),
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane Managed Aad cannot be disabled",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						Managed: to.BoolPtr(true),
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane Managed Aad cannot be mirgrated to Legacy Aad",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						Managed: to.BoolPtr(true),
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						ClientAppID:     to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppID:     to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppSecret: to.StringPtr("286******************"),
						TenantID:        to.StringPtr("678977a8-5db7-4c98-b856-b34619afg75h"),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane Legacy Aad can be set after cluster creation",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						ClientAppID:     to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppID:     to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppSecret: to.StringPtr("286******************"),
						TenantID:        to.StringPtr("678977a8-5db7-4c98-b856-b34619afg75h"),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "AzureManagedControlPlane Legacy Aad can be migrated to Managed Aad",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						ClientAppID:     to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppID:     to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppSecret: to.StringPtr("286******************"),
						TenantID:        to.StringPtr("678977a8-5db7-4c98-b856-b34619afg75h"),
					},
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						Managed: to.BoolPtr(true),
						AdminGroupObjectIDs: &[]string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "AzureManagedControlPlane Legacy Aad cannot be disabled",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						ClientAppID:     to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppID:     to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppSecret: to.StringPtr("286******************"),
						TenantID:        to.StringPtr("678977a8-5db7-4c98-b856-b34619afg75h"),
					},
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
				},
			},
			wantErr: true,
		},
		{
			name: "AzureManagedControlPlane Legacy Aad cannot be disabled",
			oldAMCP: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version: "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{
						ClientAppID:     to.StringPtr("959d77a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppID:     to.StringPtr("967077a8-5db7-4c98-b856-b34619afg75h"),
						ServerAppSecret: to.StringPtr("286******************"),
						TenantID:        to.StringPtr("678977a8-5db7-4c98-b856-b34619afg75h"),
					},
				},
			},
			amcp: &AzureManagedControlPlane{
				Spec: AzureManagedControlPlaneSpec{
					Version:    "v1.18.0",
					AADProfile: &ManagedClusterAADProfile{},
				},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.amcp.ValidateUpdate(tc.oldAMCP)
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func createAzureManagedControlPlane(t *testing.T, serviceIP, version, sshKey string, aadProfile *ManagedClusterAADProfile) *AzureManagedControlPlane {
	return &AzureManagedControlPlane{
		Spec: AzureManagedControlPlaneSpec{
			SSHPublicKey: sshKey,
			DNSServiceIP: to.StringPtr(serviceIP),
			Version:      version,
			AADProfile:   aadProfile,
		},
	}
}
