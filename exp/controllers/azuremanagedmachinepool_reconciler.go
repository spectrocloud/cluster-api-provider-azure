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

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2021-04-01/compute"
	"github.com/pkg/errors"

	apicore "k8s.io/api/core/v1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/agentpools"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/scalesets"
	infraexpv1 "sigs.k8s.io/cluster-api-provider-azure/exp/api/v1alpha4"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	layout = "2006-01-02 15:04:05 -0700 MST"
)

type (
	// azureManagedMachinePoolService contains the services required by the cluster controller.
	azureManagedMachinePoolService struct {
		scope         agentpools.ManagedMachinePoolScope
		agentPoolsSvc azure.Reconciler
		scaleSetsSvc  NodeLister
	}

	// AgentPoolVMSSNotFoundError represents a reconcile error when the VMSS for an agent pool can't be found.
	AgentPoolVMSSNotFoundError struct {
		NodeResourceGroup string
		PoolName          string
	}

	// NodeLister is a service interface for returning generic lists.
	NodeLister interface {
		ListInstances(context.Context, string, string) ([]compute.VirtualMachineScaleSetVM, error)
		List(context.Context, string) ([]compute.VirtualMachineScaleSet, error)
		DeleteInstance(context.Context, string, string, string, string) error
	}
)

// NewAgentPoolVMSSNotFoundError creates a new AgentPoolVMSSNotFoundError.
func NewAgentPoolVMSSNotFoundError(nodeResourceGroup, poolName string) *AgentPoolVMSSNotFoundError {
	return &AgentPoolVMSSNotFoundError{
		NodeResourceGroup: nodeResourceGroup,
		PoolName:          poolName,
	}
}

func (a *AgentPoolVMSSNotFoundError) Error() string {
	msgFmt := "failed to find vm scale set in resource group %s matching pool named %s"
	return fmt.Sprintf(msgFmt, a.NodeResourceGroup, a.PoolName)
}

// Is returns true if the target error is an `AgentPoolVMSSNotFoundError`.
func (a *AgentPoolVMSSNotFoundError) Is(target error) bool {
	var err *AgentPoolVMSSNotFoundError
	ok := errors.As(target, &err)
	return ok
}

// newAzureManagedMachinePoolService populates all the services based on input scope.
func newAzureManagedMachinePoolService(scope *scope.ManagedControlPlaneScope) *azureManagedMachinePoolService {
	return &azureManagedMachinePoolService{
		scope:         scope,
		agentPoolsSvc: agentpools.New(scope),
		scaleSetsSvc:  scalesets.NewClient(scope),
	}
}

// Reconcile reconciles all the services in a predetermined order.
func (s *azureManagedMachinePoolService) Reconcile(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "controllers.azureManagedMachinePoolService.Reconcile")
	defer done()

	s.scope.Info("reconciling machine pool")
	agentPoolName := s.scope.AgentPoolSpec().Name
	nodeResourceGroup := s.scope.NodeResourceGroup() // get the node resource group of the type SPC_MC_<resource group name>_<clustername>_<region>

	// set annotation with current time if agent pool needs update ; setting it in reconcile
	// agentpool svc takes over
	if err := s.agentPoolsSvc.Reconcile(ctx); err != nil {
		s.scope.Error(err, "error while reconciling agentpoool, checking if node drain timeout set and reached")
		// if error check if node timeout has passed 
		// on timeout go through each node and if the config does not match delete that node using vmssvm
		// on delete vm remove the annotation
		ndt := s.scope.GetNodeDrainTimeout()
		//if node drain timeout is not set then we do not have to do anything and return the err
		if ndt.Seconds() == 0 {
			s.scope.Info("machine pool has no node drain timeout available", "machinepool", agentPoolName)
			return err
		}
		annotations := s.scope.GetAgentPoolAnnotations()
		ts, ok := annotations[infraexpv1.NodeDrainTimeoutAnnotation] 
		// add annotation when agentpool reconciler returns error and the annotation is missing
		if !ok {
			s.scope.Info("NodeDrainTimeoutAnnotation missing")
			s.scope.SetAgentPoolAnnotations(infraexpv1.NodeDrainTimeoutAnnotation, time.Now().UTC().String())
			//return error as annotation was added recently
			return errors.Wrapf(err, "failed to reconcile machine pool %s", agentPoolName)
		}
		t, terr := time.Parse(layout, ts)	
		if terr != nil {
			s.scope.Error(terr, "unable to parse time from nodedraintimeout annotation", "timestring", ts)
			return errors.Wrapf(err, "failed to reconcile machine pool %s", agentPoolName)
		}
		now := time.Now()
		diff := now.Sub(t)
		// reconcile individual nodes on timeout exceeded
		if diff.Seconds() > ndt.Seconds() {
			s.scope.Info("reconciling agentpool failed, node timeout exceeded")
			return s.reconcileNodes(ctx, nodeResourceGroup, agentPoolName)
		}
		return azure.WithTransientError(errors.Wrapf(err, "failed to reconcile machine pool %s", agentPoolName),20*time.Second)
	}

	// get vm scale set from the node resource group, match will have the right agent pool 
	match, err := s.getVMScaleSet(ctx, nodeResourceGroup, agentPoolName)
	if err != nil {
		return err
	}

	if match == nil {
		return azure.WithTransientError(NewAgentPoolVMSSNotFoundError(nodeResourceGroup, agentPoolName), 20*time.Second)
	}

	instances, err := s.scaleSetsSvc.ListInstances(ctx, nodeResourceGroup, *match.Name) //get all the vm instances in the vmss
	if err != nil {
		return errors.Wrapf(err, "failed to reconcile machine pool %s", agentPoolName)
	}

	var providerIDs = make([]string, len(instances))
	for i := 0; i < len(instances); i++ {
		providerIDs[i] = strings.ToLower(azure.ProviderIDPrefix + *instances[i].ID)
	}

	s.scope.SetAgentPoolProviderIDList(providerIDs)
	s.scope.SetAgentPoolReplicas(int32(len(providerIDs)))
	s.scope.SetAgentPoolReady(true)
	s.scope.DeleteAgentPoolAnnotation(infraexpv1.NodeDrainTimeoutAnnotation)

	s.scope.Info("reconciled machine pool successfully")
	return nil
}

// Delete reconciles all the services in a predetermined order.
func (s *azureManagedMachinePoolService) Delete(ctx context.Context) error {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "controllers.azureManagedMachinePoolService.Delete")
	defer done()

	if err := s.agentPoolsSvc.Delete(ctx); err != nil {
		return errors.Wrapf(err, "failed to delete machine pool %s", s.scope.AgentPoolSpec().Name)
	}

	return nil
}

func (s *azureManagedMachinePoolService) getVMScaleSet(ctx context.Context, nodeResourceGroup string, agentPoolName string) (*compute.VirtualMachineScaleSet, error) {
	vmss, err := s.scaleSetsSvc.List(ctx, nodeResourceGroup)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list vmss in resource group %s", nodeResourceGroup)
	}
	
	var match *compute.VirtualMachineScaleSet
	for _, ss := range vmss {
		ss := ss
		if ss.Tags["poolName"] != nil && *ss.Tags["poolName"] == agentPoolName {
			match = &ss
			break
		}
		
		if ss.Tags["aks-managed-poolName"] != nil && *ss.Tags["aks-managed-poolName"] == agentPoolName {
			match = &ss
			break
		}
	}
	return match, nil
}

// reconcileNodes gets scale set version, instances in the scale set and k8s node version in that scaleset; 
// and deletes the nodes that do not match the version.
func (s *azureManagedMachinePoolService) reconcileNodes(ctx context.Context, nodeResourceGroup, agentPoolName string) error {
	//get the vm scaleset details using agentpool name
	scaleset, err := s.getVMScaleSet(ctx,nodeResourceGroup, agentPoolName)
	if err != nil {
		return err
	}
	
	if scaleset == nil {
		return azure.WithTransientError(NewAgentPoolVMSSNotFoundError(nodeResourceGroup, agentPoolName), 20*time.Second)
	}
	
	//version stores the k8s version for the scaleset
	var version string = ""
	versionTag := scaleset.Tags["aks-managed-orchestrator"]
	if versionTag != nil {
		strs := strings.Split(*versionTag, ":")
		if len(strs) > 1 {
			version = strs[1]
		}
	}
	
	if len(version) == 0 {
		return azure.WithTransientError(errors.New("version tag aks-managed-orchestrator not available on scaleset"), 20 * time.Second)
	}
	s.scope.Info("version tag of scaleset", "scaleset k8s version", version)

	//get all the vm's in the vm scaleset
	vmssvms, err := s.scaleSetsSvc.ListInstances(ctx, nodeResourceGroup, *scaleset.Name)
	if err != nil || len(vmssvms) == 0 {
		s.scope.Error(err, "unable to get instances in scaleset", "scaleset", scaleset.Name)
		return azure.WithTransientError(err, 20*time.Second)
	}
	
	//scalesetVMMap is a map of vm computer name and scaleset vm
	var scalesetVMMap = make(map[string]compute.VirtualMachineScaleSetVM)
	for _, vmssvm := range vmssvms {
		if vmssvm.OsProfile.ComputerName != nil {
			scalesetVMMap[*vmssvm.OsProfile.ComputerName] = vmssvm
		}
	}
	
	// get the k8s client from the managed machine pool scope
	k8sClient := s.scope.GetInfraClient()
	//nodelist will have list of k8s nodes for the given agentpool 
	var nodelist apicore.NodeList
	listOpts := ctrlclient.MatchingLabels(map[string]string{"agentpool": agentPoolName})
	//fetch all the nodes matching the label 
	if err = k8sClient.List(ctx, &nodelist, listOpts); err != nil {
		return errors.Wrapf(err, "unable to get agent pool node")
	}
	
	var nodesToDelete []string
	for _, node := range nodelist.Items {
		if node.Status.NodeInfo.KubeletVersion[1:] != version {
			nodesToDelete = append(nodesToDelete, node.Name)
		}	
	}
		
	if  len(nodesToDelete) == 0{
		// all the nodes have been updated and we need not to clean the pool
		s.scope.Info("no nodes to delete")
		s.scope.DeleteAgentPoolAnnotation(infraexpv1.NodeDrainTimeoutAnnotation)
		return nil 
	}

	s.scope.Info("deleting nodes", "nodesToDelete", nodesToDelete)
	
	for _ , name := range nodesToDelete {
		s.scope.Info("deleting scaleset vm", "nodeName", name)
		err = s.scaleSetsSvc.DeleteInstance(ctx, nodeResourceGroup, *scaleset.Name, name, *scalesetVMMap[name].InstanceID) 
		if err != nil {
			s.scope.Error(err,fmt.Sprintf("failed to delete the scaleset vm %s",name))
		}
	}
	//if after delete any error exist we should reconcile.
	if err != nil {
		err := errors.New("unable to delete all the nodes")
		return azure.WithTransientError(err, 20*time.Second)
	}
	s.scope.DeleteAgentPoolAnnotation(infraexpv1.NodeDrainTimeoutAnnotation)
	return nil
}