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

package v1alpha3

import (
	"errors"
	"net"
	"reflect"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
)

// log is for logging in this package.
var azuremanagedcontrolplanelog = logf.Log.WithName("azuremanagedcontrolplane-resource")

var kubeSemver = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)([-0-9a-zA-Z_\.+]*)?$`)

// SetupWebhookWithManager sets up and registers the webhook with the manager.
func (r *AzureManagedControlPlane) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-exp-infrastructure-cluster-x-k8s-io-v1alpha3-azuremanagedcontrolplane,mutating=true,failurePolicy=fail,groups=exp.infrastructure.cluster.x-k8s.io,resources=azuremanagedcontrolplanes,verbs=create;update,versions=v1alpha3,name=azuremanagedcontrolplane.kb.io

var _ webhook.Defaulter = &AzureManagedControlPlane{}

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (r *AzureManagedControlPlane) Default() {
	azuremanagedcontrolplanelog.Info("default", "name", r.Name)

	if r.Spec.NetworkPlugin == nil {
		networkPlugin := "azure"
		r.Spec.NetworkPlugin = &networkPlugin
	}
	if r.Spec.LoadBalancerSKU == nil {
		loadBalancerSKU := "Standard"
		r.Spec.LoadBalancerSKU = &loadBalancerSKU
	}
	if r.Spec.NetworkPolicy == nil {
		NetworkPolicy := "calico"
		r.Spec.NetworkPolicy = &NetworkPolicy
	}

	if r.Spec.Version != "" && !strings.HasPrefix(r.Spec.Version, "v") {
		normalizedVersion := "v" + r.Spec.Version
		r.Spec.Version = normalizedVersion
	}

	err := r.setDefaultSSHPublicKey()
	if err != nil {
		azuremanagedcontrolplanelog.Error(err, "SetDefaultSshPublicKey failed")
	}

	r.setDefaultNodeResourceGroupName()
	r.setDefaultVirtualNetwork()
	r.setDefaultSubnet()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-exp-infrastructure-cluster-x-k8s-io-v1alpha3-azuremanagedcontrolplane,mutating=false,failurePolicy=fail,groups=exp.infrastructure.cluster.x-k8s.io,resources=azuremanagedcontrolplanes,versions=v1alpha3,name=azuremanagedcontrolplane.kb.io

var _ webhook.Validator = &AzureManagedControlPlane{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *AzureManagedControlPlane) ValidateCreate() error {
	azuremanagedcontrolplanelog.Info("validate create", "name", r.Name)

	return r.Validate()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *AzureManagedControlPlane) ValidateUpdate(oldRaw runtime.Object) error {
	azuremanagedcontrolplanelog.Info("validate update", "name", r.Name)
	var allErrs field.ErrorList
	old := oldRaw.(*AzureManagedControlPlane)

	if r.Spec.SubscriptionID != old.Spec.SubscriptionID {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("Spec", "SubscriptionID"),
				r.Spec.SubscriptionID,
				"field is immutable"))
	}

	if r.Spec.ResourceGroupName != old.Spec.ResourceGroupName {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("Spec", "ResourceGroupName"),
				r.Spec.ResourceGroupName,
				"field is immutable"))
	}

	if r.Spec.NodeResourceGroupName != old.Spec.NodeResourceGroupName {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("Spec", "NodeResourceGroupName"),
				r.Spec.NodeResourceGroupName,
				"field is immutable"))
	}

	if r.Spec.Location != old.Spec.Location {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("Spec", "Location"),
				r.Spec.Location,
				"field is immutable"))
	}

	if old.Spec.SSHPublicKey != "" {
		// Prevent SSH key modification if it was already set to some value
		if r.Spec.SSHPublicKey != old.Spec.SSHPublicKey {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "SSHPublicKey"),
					r.Spec.SSHPublicKey,
					"field is immutable"))
		}
	}

	if old.Spec.DNSServiceIP != nil {
		// Prevent DNSServiceIP modification if it was already set to some value
		if r.Spec.DNSServiceIP == nil {
			// unsetting the field is not allowed
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "DNSServiceIP"),
					r.Spec.DNSServiceIP,
					"field is immutable, unsetting is not allowed"))
		} else if *r.Spec.DNSServiceIP != *old.Spec.DNSServiceIP {
			// changing the field is not allowed
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "DNSServiceIP"),
					*r.Spec.DNSServiceIP,
					"field is immutable"))
		}
	}

	if old.Spec.NetworkPlugin != nil {
		// Prevent NetworkPlugin modification if it was already set to some value
		if r.Spec.NetworkPlugin == nil {
			// unsetting the field is not allowed
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "NetworkPlugin"),
					r.Spec.NetworkPlugin,
					"field is immutable, unsetting is not allowed"))
		} else if *r.Spec.NetworkPlugin != *old.Spec.NetworkPlugin {
			// changing the field is not allowed
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "NetworkPlugin"),
					*r.Spec.NetworkPlugin,
					"field is immutable"))
		}
	}

	if old.Spec.NetworkPolicy != nil {
		// Prevent NetworkPolicy modification if it was already set to some value
		if r.Spec.NetworkPolicy == nil {
			// unsetting the field is not allowed
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "NetworkPolicy"),
					r.Spec.NetworkPolicy,
					"field is immutable, unsetting is not allowed"))
		} else if *r.Spec.NetworkPolicy != *old.Spec.NetworkPolicy {
			// changing the field is not allowed
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "NetworkPolicy"),
					*r.Spec.NetworkPolicy,
					"field is immutable"))
		}
	}

	if old.Spec.LoadBalancerSKU != nil {
		// Prevent LoadBalancerSKU modification if it was already set to some value
		if r.Spec.LoadBalancerSKU == nil {
			// unsetting the field is not allowed
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "LoadBalancerSKU"),
					r.Spec.LoadBalancerSKU,
					"field is immutable, unsetting is not allowed"))
		} else if *r.Spec.LoadBalancerSKU != *old.Spec.LoadBalancerSKU {
			// changing the field is not allowed
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "LoadBalancerSKU"),
					*r.Spec.LoadBalancerSKU,
					"field is immutable"))
		}
	}

	if old.Spec.DefaultPoolRef.Name != "" {
		if r.Spec.DefaultPoolRef.Name != old.Spec.DefaultPoolRef.Name {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "DefaultPoolRef", "Name"),
					r.Spec.DefaultPoolRef.Name,
					"field is immutable"))
		}
	}

	errs := r.validateUpdateAadProfile(old.Spec.AADProfile)
	allErrs = append(allErrs, errs...)

	if len(allErrs) == 0 {
		return r.Validate()
	}

	return apierrors.NewInvalid(GroupVersion.WithKind("AzureManagedControlPlane").GroupKind(), r.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *AzureManagedControlPlane) ValidateDelete() error {
	azuremanagedcontrolplanelog.Info("validate delete", "name", r.Name)

	return nil
}

// Validate the Azure Machine Pool and return an aggregate error.
func (r *AzureManagedControlPlane) Validate() error {
	validators := []func() error{
		r.validateVersion,
		r.validateDNSServiceIP,
		r.validateSSHKey,
		r.validateAadProfile,
	}

	var errs []error
	for _, validator := range validators {
		if err := validator(); err != nil {
			errs = append(errs, err)
		}
	}

	return kerrors.NewAggregate(errs)
}

// validate DNSServiceIP.
func (r *AzureManagedControlPlane) validateDNSServiceIP() error {
	if r.Spec.DNSServiceIP != nil {
		if net.ParseIP(*r.Spec.DNSServiceIP) == nil {
			return errors.New("DNSServiceIP must be a valid IP")
		}
	}

	return nil
}

func (r *AzureManagedControlPlane) validateVersion() error {
	if !kubeSemver.MatchString(r.Spec.Version) {
		return errors.New("must be a valid semantic version")
	}

	return nil
}

// ValidateSSHKey validates an SSHKey
func (r *AzureManagedControlPlane) validateSSHKey() error {
	if r.Spec.SSHPublicKey != "" {
		sshKey := r.Spec.SSHPublicKey
		if errs := infrav1.ValidateSSHKey(sshKey, field.NewPath("sshKey")); len(errs) > 0 {
			agg := kerrors.NewAggregate(errs.ToAggregate().Errors())
			azuremachinepoollog.Info("Invalid sshKey: %s", agg.Error())
			return agg
		}
	}

	return nil
}

func (r *AzureManagedControlPlane) validateAadProfile() error {
	if r.Spec.AADProfile != nil {
		managedAad, err := r.validateManagedAadProfile()
		if err != nil {
			return err
		}
		legacyAad := r.validateLegacyAadProfile()
		if managedAad && legacyAad {
			return errors.New("conflicting values provided in AADProfile")
		}
	}
	return nil
}

func (r *AzureManagedControlPlane) validateManagedAadProfile() (bool, error) {
	if r.Spec.AADProfile.ManagedAAD != nil {
		aad := r.Spec.AADProfile.ManagedAAD

		if *aad.Managed && len(*aad.AdminGroupObjectIDs) == 0 {
			return false, errors.New("require atleast one groupObjectID in Spec.AADProfile.ManagedAAD.AdminGroupObjectID")
		}

		if len(*aad.AdminGroupObjectIDs) != 0 && !*aad.Managed {
			return false, errors.New("managed field has to be true to enable aad integration with aks")
		}
	}
	return r.Spec.AADProfile.ManagedAAD != nil, nil
}

func (r *AzureManagedControlPlane) validateLegacyAadProfile() bool {
	return r.Spec.AADProfile.LegacyAAD != nil
}

func (r *AzureManagedControlPlane) validateUpdateAadProfile(old *AADProfile) field.ErrorList {
	allErrs := field.ErrorList{}
	new := r.Spec.AADProfile

	if old != nil && new == nil {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("Spec", "AADProfile"),
				new,
				"field cannot be nil, cannot disable AADProfile"))
	}

	if old == nil && new != nil {
		if new.LegacyAAD != nil {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "AADProfile", "LegacyAAD"),
					new.LegacyAAD,
					"field is immutable"))
		}
	}

	if old != nil && new != nil {
		if old.ManagedAAD != nil && new.ManagedAAD == nil {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("Spec", "AADProfile", "ManagedAAD"),
					new.ManagedAAD,
					"AADProfile cannot be disabled or migrated to Legacy"))
		}

		if old.ManagedAAD != nil && new.ManagedAAD != nil {
			if !*new.ManagedAAD.Managed {
				allErrs = append(allErrs,
					field.Invalid(
						field.NewPath("Spec", "AADProfile", "ManagedAAD"),
						new.ManagedAAD.Managed,
						"field cannot be set to false"))
			}
			if len(*new.ManagedAAD.AdminGroupObjectIDs) == 0 {
				allErrs = append(allErrs,
					field.Invalid(
						field.NewPath("Spec", "AADProfile", "AdminGroupObjectIDs"),
						new.ManagedAAD.AdminGroupObjectIDs,
						"need atleast one AdminGroupObjectID"))
			}
		}

		if new.ManagedAAD == nil {
			if !reflect.DeepEqual(new.LegacyAAD, old.LegacyAAD) {
				allErrs = append(allErrs,
					field.Invalid(
						field.NewPath("Spec", "AADProfile", "LegacyAAD"),
						new.LegacyAAD,
						"field is immutable"))
			}
		}
	}
	return allErrs
}
