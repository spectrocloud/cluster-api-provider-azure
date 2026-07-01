/*
Copyright 2026 The Kubernetes Authors.

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

// Package sus1748verify isolates the SUS-1748 / SCS-4830 (PCP-6938) characterization
// test from the converters package's own _test.go files, which are pre-existing-broken
// on spectro-master (they import the old v1api20231001 ASO types while the production
// converter was bumped to v1api20240901). This external package imports the exported
// converter directly, so it compiles and runs regardless of that breakage.
package sus1748verify

import (
	"testing"

	asohub "github.com/Azure/azure-service-operator/v2/api/containerservice/v1api20240901/storage"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cluster-api-provider-azure/azure/converters"
)

// SUS-1748 / SCS-4830 (PCP-6938): a system (first) AKS node pool created with OS SKU
// "AzureLinux" comes up as Ubuntu. Palette injects osSKU via
// asoManagedClustersAgentPoolPatches onto the AzureManagedMachinePool; that JSON merge
// patch IS applied to the ManagedClustersAgentPool object (aso.PatchedParameters ->
// applyPatches) before the system pool is embedded into ManagedCluster.Spec.
// AgentPoolProfiles at creation (azure/services/managedclusters/spec.go). So by the
// time AgentPoolToManagedClusterAgentPoolProfile runs, pool.Spec.OsSKU == "AzureLinux".
// The converter copied a fixed field list that OMITTED OsSKU, so the embedded profile
// reached AKS with no osSKU and Azure defaulted the node image to Ubuntu.
//
// This asserts the FIXED behavior: the converter forwards OsSKU. It FAILS on the
// unpatched converter (OsSKU nil) and PASSES once `OsSKU: properties.OsSKU` is added.
func Test_ConverterForwardsOsSKU_SUS1748(t *testing.T) {
	g := NewWithT(t)

	pool := &asohub.ManagedClustersAgentPool{
		Spec: asohub.ManagedClustersAgentPool_Spec{
			AzureName: "haeckipool",
			OsType:    ptr.To("Linux"),
			OsSKU:     ptr.To("AzureLinux"),
			Mode:      ptr.To("System"),
		},
	}

	profile := converters.AgentPoolToManagedClusterAgentPoolProfile(pool)

	// Sanity: fields already in the converter survive.
	g.Expect(profile.OsType).To(Equal(ptr.To("Linux")))
	g.Expect(profile.Mode).To(Equal(ptr.To("System")))

	// The fix: OsSKU now carried onto the embedded system-pool profile.
	g.Expect(profile.OsSKU).To(Equal(ptr.To("AzureLinux")),
		"PCP-6938: converter must forward OsSKU to the embedded system pool")
}
