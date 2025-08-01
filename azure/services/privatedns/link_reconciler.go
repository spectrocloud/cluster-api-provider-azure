/*
Copyright 2022 The Kubernetes Authors.

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

package privatedns

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

// extractResourceGroupFromID extracts the resource group from a resource ID string.
func extractResourceGroupFromID(id string) string {
	// Example ID: /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/privateDnsZones/{zone}
	parts := strings.Split(id, "/")
	for i, part := range parts {
		if part == "resourceGroups" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func (s *Service) reconcileLinks(ctx context.Context, links []azure.ResourceSpecGetter) (managed bool, err error) {
	ctx, log, done := tele.StartSpanWithLogger(ctx, "privatedns.Service.reconcileLinks")
	defer done()

	var resErr error

	for _, linkSpec := range links {
		isLinkManaged, err := s.isVnetLinkManaged(ctx, linkSpec)
		if err != nil {
			if azure.ResourceNotFound(err) {
				isLinkManaged = true
			} else {
				return managed, err
			}
		}

		if !isLinkManaged {
			log.V(2).Info("Skipping vnet link reconciliation for unmanaged vnet link", "vnet link",
				linkSpec.ResourceName(), "private dns zone", linkSpec.OwnerResourceName())
			continue
		}

		// Enhanced logic: check all zones with the same name across all resource groups to prevent duplicate VNet links.
		// This prevents DNS resolution conflicts by ensuring a VNet is not linked to multiple private DNS zones
		// with the same name, even if they exist in different resource groups.
		zoneName := linkSpec.OwnerResourceName()
		vnetID := azure.VNetID(linkSpec.(LinkSpec).SubscriptionID, linkSpec.(LinkSpec).VNetResourceGroup, linkSpec.(LinkSpec).VNetName)
		zones, err := s.zonesClient.ListAllZonesByName(ctx, zoneName)
		if err != nil {
			return managed, err
		}
		alreadyLinked := false
		for _, zone := range zones {
			if zone.Name == nil || zone.ID == nil {
				continue
			}
			zoneRG := extractResourceGroupFromID(*zone.ID)
			links, err := s.vnetLinkClient.ListByZone(ctx, zoneRG, *zone.Name)
			if err != nil {
				return managed, err
			}
			for _, link := range links {
				if link.Properties != nil && link.Properties.VirtualNetwork != nil && link.Properties.VirtualNetwork.ID != nil && *link.Properties.VirtualNetwork.ID == vnetID {
					log.V(1).Info("Skipping vnet link creation as VNet is already linked to a zone with the same name across resource groups",
						"vnet link", linkSpec.ResourceName(),
						"private dns zone", zoneName,
						"vnet", linkSpec.(LinkSpec).VNetName,
						"resource group", linkSpec.ResourceGroupName(),
						"zone resource group", zoneRG)
					alreadyLinked = true
					break
				}
			}
			if alreadyLinked {
				break
			}
		}
		if alreadyLinked {
			managed = true
			continue
		}

		// Fallback: check if link exists in the current zone/resource group
		existingLink, err := s.vnetLinkClient.Get(ctx, linkSpec)
		if err != nil && !azure.ResourceNotFound(err) {
			return managed, err
		}
		if existingLink != nil {
			log.V(1).Info("Skipping vnet link creation as it already exists",
				"vnet link", linkSpec.ResourceName(),
				"private dns zone", linkSpec.OwnerResourceName(),
				"vnet", linkSpec.(LinkSpec).VNetName,
				"resource group", linkSpec.ResourceGroupName())
			managed = true
			continue
		}

		managed = true
		if _, err := s.vnetLinkReconciler.CreateOrUpdateResource(ctx, linkSpec, serviceName); err != nil {
			if !azure.IsOperationNotDoneError(err) || resErr == nil {
				resErr = err
			}
		}
	}

	return managed, resErr
}

func (s *Service) deleteLinks(ctx context.Context, links []azure.ResourceSpecGetter) (managed bool, err error) {
	ctx, log, done := tele.StartSpanWithLogger(ctx, "privatedns.Service.deleteLinks")
	defer done()

	var resErr error

	// We go through the list of links to delete each one, independently of the result of the previous one.
	// If multiple errors occur, we return the most pressing one.
	// Order of precedence (highest -> lowest) is: error that is not an operationNotDoneError (i.e. error creating) -> operationNotDoneError (i.e. creating in progress) -> no error (i.e. created)
	for _, linkSpec := range links {
		// If the virtual network link is not managed by capz, skip its reconciliation
		isVnetLinkManaged, err := s.isVnetLinkManaged(ctx, linkSpec)
		if err != nil {
			if azure.ResourceNotFound(err) {
				// already deleted or doesn't exist, cleanup status and return.
				s.Scope.DeleteLongRunningOperationState(linkSpec.ResourceName(), serviceName, infrav1.DeleteFuture)
				continue
			}
			return managed, errors.Wrapf(err, "could not get vnet link state of %s in resource group %s",
				linkSpec.OwnerResourceName(), linkSpec.ResourceGroupName())
		}

		if !isVnetLinkManaged {
			log.V(2).Info("Skipping vnet link deletion for unmanaged vnet link", "vnet link",
				linkSpec.ResourceName(), "private dns zone", linkSpec.OwnerResourceName())
			continue
		}

		// if we reach here, it means that this vnet link is managed by capz.
		managed = true

		if err := s.vnetLinkReconciler.DeleteResource(ctx, linkSpec, serviceName); err != nil {
			if !azure.IsOperationNotDoneError(err) || resErr == nil {
				resErr = err
			}
		}
	}

	return managed, resErr
}
