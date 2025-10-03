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
	"time"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/availabilitysets"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/disks"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/inboundnatrules"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/networkinterfaces"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/resourceskus"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/roleassignments"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/tags"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/virtualmachines"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/vmextensions"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

// azureMachineService is the group of services called by the AzureMachine controller.
type azureMachineService struct {
	scope *scope.MachineScope
	// services is the list of services to be reconciled.
	// The order of the services is important as it determines the order in which the services are reconciled.
	services  []azure.ServiceReconciler
	skuCache  *resourceskus.Cache
	Reconcile func(context.Context) error
	Pause     func(context.Context) error
	Delete    func(context.Context) error
}

// newAzureMachineService populates all the services based on input scope.
func newAzureMachineService(machineScope *scope.MachineScope) (*azureMachineService, error) {
	cache, err := resourceskus.GetCache(machineScope, machineScope.Location())
	if err != nil {
		return nil, errors.Wrap(err, "failed creating a NewCache")
	}
	availabilitySetsSvc, err := availabilitysets.New(machineScope, cache)
	if err != nil {
		return nil, errors.Wrap(err, "failed creating availabilitysets service")
	}
	disksSvc, err := disks.New(machineScope)
	if err != nil {
		return nil, errors.Wrap(err, "failed creating disks service")
	}
	inboundnatrulesSvc, err := inboundnatrules.New(machineScope)
	if err != nil {
		return nil, errors.Wrap(err, "failed creating inboundnatrules service")
	}
	publicIPsSvc, err := publicips.New(machineScope)
	if err != nil {
		return nil, errors.Wrap(err, "failed creating publicips service")
	}
	roleAssignmentsSvc, err := roleassignments.New(machineScope)
	if err != nil {
		return nil, errors.Wrap(err, "failed creating roleassignments service")
	}
	tagsSvc, err := tags.New(machineScope)
	if err != nil {
		return nil, errors.Wrap(err, "failed creating tags service")
	}
	virtualmachinesSvc, err := virtualmachines.New(machineScope)
	if err != nil {
		return nil, errors.Wrap(err, "failed creating virtualmachines service")
	}
	vmextensionsSvc, err := vmextensions.New(machineScope)
	if err != nil {
		return nil, errors.Wrap(err, "failed creating vmextensions service")
	}
	networkInterfacesSvc, err := networkinterfaces.New(machineScope, cache)
	if err != nil {
		return nil, errors.Wrap(err, "failed creating networkinterfaces service")
	}
	ams := &azureMachineService{
		scope: machineScope,
		services: []azure.ServiceReconciler{
			publicIPsSvc,
			inboundnatrulesSvc,
			networkInterfacesSvc,
			availabilitySetsSvc,
			disksSvc,
			virtualmachinesSvc,
			roleAssignmentsSvc,
			vmextensionsSvc,
			tagsSvc,
		},
		skuCache: cache,
	}
	ams.Reconcile = ams.reconcile
	ams.Pause = ams.pause
	ams.Delete = ams.delete

	return ams, nil
}

// reconcile reconciles all the services in a predetermined order.
func (s *azureMachineService) reconcile(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "controllers.azureMachineService.reconcile")
	defer done()

	// Ensure that the deprecated networking field values have been migrated to the new NetworkInterfaces field.
	s.scope.AzureMachine.Spec.SetNetworkInterfacesDefaults()

	if err := s.scope.SetSubnetName(); err != nil {
		return errors.Wrap(err, "failed defaulting subnet name")
	}

	for _, service := range s.services {
		if err := service.Reconcile(ctx); err != nil {
			return errors.Wrapf(err, "failed to reconcile AzureMachine service %s", service.Name())
		}
	}

	return nil
}

// pause pauses all components making up the machine.
func (s *azureMachineService) pause(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "controllers.azureMachineService.pause")
	defer done()

	for _, service := range s.services {
		pauser, ok := service.(azure.Pauser)
		if !ok {
			continue
		}
		if err := pauser.Pause(ctx); err != nil {
			return errors.Wrapf(err, "failed to pause AzureMachine service %s", service.Name())
		}
	}

	return nil
}

// delete deletes all the services in a predetermined order.
func (s *azureMachineService) delete(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "controllers.azureMachineService.delete")
	defer done()

	// Delete services in reverse order of creation.
	for i := len(s.services) - 1; i >= 0; i-- {
		if err := s.services[i].Delete(ctx); err != nil {
			// Handle AzureUSSecretCloud 500 error for VM service specifically
			if s.services[i].Name() == "virtualmachine" && azure.IsTransientServerError(err) {
				log := ctrl.LoggerFrom(ctx)
				log.V(2).Info("VM service returned server error, starting bounded verification",
					"service", s.services[i].Name(), "error", err.Error())

				// For VM service 500 errors, perform bounded retry verification (10 rounds, 5 seconds each)
				if s.verifyVMDeletionWithBoundedRetry(ctx) {
					log.V(2).Info("VM confirmed deleted after bounded verification despite server error, continuing with remaining services",
						"service", s.services[i].Name())
					// Continue with next service instead of returning error
					continue
				} else {
					log.V(2).Info("VM still exists after bounded verification, treating as actual failure",
						"service", s.services[i].Name())
					// VM still exists after all retries, return error as normal
				}
			}
			return errors.Wrapf(err, "failed to delete AzureMachine service %s", s.services[i].Name())
		}
	}

	return nil
}

// verifyVMDeletionWithBoundedRetry performs bounded retry verification for VM deletion
// It checks up to 10 rounds with 5-second intervals to handle async deletion scenarios
func (s *azureMachineService) verifyVMDeletionWithBoundedRetry(ctx context.Context) bool {
	log := ctrl.LoggerFrom(ctx)

	const maxRetries = 10
	const retryInterval = 5 * time.Second

	log.V(2).Info("starting bounded VM deletion verification",
		"maxRetries", maxRetries, "interval", retryInterval)

	for round := 1; round <= maxRetries; round++ {
		log.V(2).Info("VM deletion verification round",
			"round", round, "maxRetries", maxRetries)

		// Check if VM is deleted in this round
		if s.isVMActuallyDeleted(ctx) {
			log.V(2).Info("VM deletion confirmed in bounded verification",
				"round", round)
			return true
		}

		// If this is not the last round, wait before next check
		if round < maxRetries {
			log.V(2).Info("VM still exists, waiting before next verification round",
				"round", round, "waitTime", retryInterval)

			// Create a timeout context for the sleep
			sleepCtx, cancel := context.WithTimeout(ctx, retryInterval+time.Second)
			defer cancel()

			select {
			case <-time.After(retryInterval):
				// Continue to next round
			case <-sleepCtx.Done():
				log.V(2).Info("bounded verification cancelled due to context timeout",
					"round", round)
				return false
			}
		}
	}

	log.V(2).Info("bounded verification completed, VM still exists after all rounds",
		"totalRounds", maxRetries)
	return false
}

// isVMActuallyDeleted verifies if the VM has been successfully deleted by attempting to get it from Azure
// This is a single-shot verification used by the bounded retry mechanism
func (s *azureMachineService) isVMActuallyDeleted(ctx context.Context) bool {
	log := ctrl.LoggerFrom(ctx)

	// Create a short timeout context for verification (shorter since we're doing multiple rounds)
	verifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Create VM client to check VM existence
	vmClient, err := virtualmachines.NewClient(s.scope, s.scope.DefaultedAzureCallTimeout())
	if err != nil {
		log.V(2).Info("VM verification: failed to create VM client", "error", err.Error())
		// If we can't create the client, assume VM still exists to be safe
		return false
	}

	// Try to get the VM - if it returns ResourceNotFound, the VM is deleted
	vmSpec := s.scope.VMSpec()

	_, getErr := vmClient.Get(verifyCtx, vmSpec)
	if getErr != nil {
		if azure.ResourceNotFound(getErr) {
			log.V(2).Info("VM verification: VM not found (successfully deleted)")
			return true
		}
		log.V(2).Info("VM verification: error checking VM existence", "error", getErr.Error())
		// If we can't verify due to other errors, assume VM still exists to be safe
		return false
	}

	log.V(2).Info("VM verification: VM still exists")
	return false
}
