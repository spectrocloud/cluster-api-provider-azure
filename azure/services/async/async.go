/*
Copyright 2023 The Kubernetes Authors.

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

package async

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/converters"
	"sigs.k8s.io/cluster-api-provider-azure/util/reconciler"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

const (
	// DefaultPollerFrequency is how often a poller should check for completion, in seconds.
	DefaultPollerFrequency = 1 * time.Second
)

// Service handles asynchronous creation and deletion of resources. It implements the Reconciler interface.
type Service[C, D any] struct {
	Scope FutureScope
	Creator[C]
	Deleter[D]
}

// New creates an async Service.
func New[C, D any](scope FutureScope, createClient Creator[C], deleteClient Deleter[D]) *Service[C, D] {
	return &Service[C, D]{
		Scope:   scope,
		Creator: createClient,
		Deleter: deleteClient,
	}
}

// CreateOrUpdateResource creates a new resource or updates an existing one asynchronously.
func (s *Service[C, D]) CreateOrUpdateResource(ctx context.Context, spec azure.ResourceSpecGetter, serviceName string) (result interface{}, err error) {
	ctx, log, done := tele.StartSpanWithLogger(ctx, "async.Service.CreateOrUpdateResource")
	defer done()

	resourceName := spec.ResourceName()
	rgName := spec.ResourceGroupName()
	futureType := infrav1.PutFuture

	// Check if there is an ongoing long-running operation.
	resumeToken := ""
	if future := s.Scope.GetLongRunningOperationState(resourceName, serviceName, futureType); future != nil {
		t, err := converters.FutureToResumeToken(*future)
		if err != nil {
			s.Scope.DeleteLongRunningOperationState(resourceName, serviceName, futureType)
			return "", errors.Wrap(err, "could not decode future data, resetting long-running operation state")
		}
		resumeToken = t
	}

	// Only when no long running operation is currently in progress do we need to get the parameters.
	// The polling implemented by the SDK does not use parameters when a resume token exists.
	var parameters interface{}
	if resumeToken == "" {
		// Get the resource if it already exists, and use it to construct the desired resource parameters.
		var existingResource interface{}
		if existing, err := s.Creator.Get(ctx, spec); err != nil && !azure.ResourceNotFound(err) {
			errWrapped := errors.Wrapf(err, "failed to get existing resource %s/%s (service: %s)", rgName, resourceName, serviceName)
			return nil, azure.WithTransientError(errWrapped, getRetryAfterFromError(err))
		} else if err == nil {
			existingResource = existing
			log.V(2).Info("successfully got existing resource", "service", serviceName, "resource", resourceName, "resourceGroup", rgName)
		}

		// Construct parameters using the resource spec and information from the existing resource, if there is one.
		parameters, err = spec.Parameters(ctx, existingResource)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get desired parameters for resource %s/%s (service: %s)", rgName, resourceName, serviceName)
		} else if parameters == nil {
			// Nothing to do, don't create or update the resource and return the existing resource.
			log.V(2).Info("resource up to date", "service", serviceName, "resource", resourceName, "resourceGroup", rgName)
			return existingResource, nil
		}

		// Create or update the resource with the desired parameters.
		if existingResource != nil {
			log.V(2).Info("updating resource", "service", serviceName, "resource", resourceName, "resourceGroup", rgName)
		} else {
			log.V(2).Info("creating resource", "service", serviceName, "resource", resourceName, "resourceGroup", rgName)
		}
	}

	result, poller, err := s.Creator.CreateOrUpdateAsync(ctx, spec, resumeToken, parameters)
	errWrapped := errors.Wrapf(err, "failed to create or update resource %s/%s (service: %s)", rgName, resourceName, serviceName)
	if poller != nil && azure.IsContextDeadlineExceededOrCanceledError(err) {
		future, err := converters.PollerToFuture(poller, infrav1.PutFuture, serviceName, resourceName, rgName)
		if err != nil {
			return nil, errWrapped
		}
		s.Scope.SetLongRunningOperationState(future)
		return nil, azure.WithTransientError(azure.NewOperationNotDoneError(future), requeueTime(s.Scope))
	}

	// Once the operation is done, delete the long-running operation state. Even if the operation ended with
	// an error, clear out any lingering state to try the operation again.
	s.Scope.DeleteLongRunningOperationState(resourceName, serviceName, futureType)

	if err != nil {
		return nil, errWrapped
	}

	log.V(2).Info("successfully created or updated resource", "service", serviceName, "resource", resourceName, "resourceGroup", rgName)
	return result, nil
}

// DeleteResource deletes a resource asynchronously with idempotent DELETE pattern.
func (s *Service[C, D]) DeleteResource(ctx context.Context, spec azure.ResourceSpecGetter, serviceName string) (err error) {
	ctx, log, done := tele.StartSpanWithLogger(ctx, "async.Service.DeleteResource")
	defer done()

	resourceName := spec.ResourceName()
	rgName := spec.ResourceGroupName()
	futureType := infrav1.DeleteFuture

	// Check for an ongoing long-running operation.
	resumeToken := ""
	if future := s.Scope.GetLongRunningOperationState(resourceName, serviceName, futureType); future != nil {
		t, err := converters.FutureToResumeToken(*future)
		if err != nil {
			s.Scope.DeleteLongRunningOperationState(resourceName, serviceName, futureType)
			return errors.Wrap(err, "could not decode future data, resetting long-running operation state")
		}
		resumeToken = t
	}

	// Attempt to delete the resource.
	log.V(2).Info("deleting resource", "service", serviceName, "resource", resourceName, "resourceGroup", rgName)
	poller, err := s.Deleter.DeleteAsync(ctx, spec, resumeToken)
	if poller != nil && azure.IsContextDeadlineExceededOrCanceledError(err) {
		future, err := converters.PollerToFuture(poller, infrav1.DeleteFuture, serviceName, resourceName, rgName)
		if err != nil {
			return errors.Wrap(err, "failed to convert poller to future")
		}
		s.Scope.SetLongRunningOperationState(future)
		return azure.WithTransientError(azure.NewOperationNotDoneError(future), requeueTime(s.Scope))
	}

	// Once the operation is done, delete the long-running operation state.
	s.Scope.DeleteLongRunningOperationState(resourceName, serviceName, futureType)

	// Handle DELETE result with idempotent pattern
	if err != nil {
		// If DELETE failed, verify existence to determine if operation was actually successful
		log.V(2).Info("delete operation reported error, verifying resource existence",
			"service", serviceName, "resource", resourceName, "resourceGroup", rgName, "error", err.Error())

		return s.handleDeleteWithVerification(ctx, spec, serviceName, log, err)
	}

	log.V(2).Info("successfully deleted resource", "service", serviceName, "resource", resourceName, "resourceGroup", rgName)
	return nil
}

// handleDeleteWithVerification implements the idempotent DELETE pattern:
// 1. Check if resource exists (GET request)
// 2. If 404 (not found) → DELETE was successful, continue
// 3. If 200/204 (exists) → Resource still exists, do bounded re-check
// 4. Only surface error if resource still exists after bounded check
func (s *Service[C, D]) handleDeleteWithVerification(ctx context.Context, spec azure.ResourceSpecGetter, serviceName string, log logr.Logger, originalErr error) error {
	resourceName := spec.ResourceName()
	rgName := spec.ResourceGroupName()

	log.V(2).Info("verifying resource existence after failed delete",
		"service", serviceName, "resource", resourceName, "resourceGroup", rgName)

	// Check if resource still exists
	exists, err := s.checkResourceExists(ctx, spec, serviceName, log)
	if err != nil {
		// If we can't determine existence, return the original error
		log.V(2).Info("could not verify resource existence, returning original error",
			"service", serviceName, "resource", resourceName, "error", err.Error())
		return originalErr
	}

	if !exists {
		// Resource doesn't exist (404) → DELETE was successful
		log.V(2).Info("resource confirmed deleted (404 on GET) - treating delete as successful",
			"service", serviceName, "resource", resourceName, "resourceGroup", rgName)
		return nil
	}

	// Resource still exists (200/204) → Do bounded re-check
	log.V(2).Info("resource still exists after delete failure, starting bounded re-check",
		"service", serviceName, "resource", resourceName, "resourceGroup", rgName)

	return s.boundedRecheck(ctx, spec, serviceName, log, originalErr)
}

// checkResourceExists performs a GET request to check if resource exists
// Returns: (exists bool, error)
func (s *Service[C, D]) checkResourceExists(ctx context.Context, spec azure.ResourceSpecGetter, serviceName string, log logr.Logger) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second) // Short timeout for verification
	defer cancel()

	log.V(4).Info("checking resource existence", "service", serviceName, "resource", spec.ResourceName())

	_, err := s.Creator.Get(ctx, spec)
	if err != nil {
		if azure.ResourceNotFound(err) {
			log.V(4).Info("resource does not exist (404)", "service", serviceName, "resource", spec.ResourceName())
			return false, nil // Resource doesn't exist
		}
		// Other errors (500, network issues, etc.)
		log.V(4).Info("error checking resource existence", "service", serviceName, "resource", spec.ResourceName(), "error", err.Error())
		return false, err // Can't determine existence
	}

	log.V(4).Info("resource exists (200/204)", "service", serviceName, "resource", spec.ResourceName())
	return true, nil // Resource exists
}

// boundedRecheck performs 8 rounds of 5-second checks (40 seconds total)
// Only surfaces error if resource still exists after all checks
func (s *Service[C, D]) boundedRecheck(ctx context.Context, spec azure.ResourceSpecGetter, serviceName string, log logr.Logger, originalErr error) error {
	resourceName := spec.ResourceName()
	rgName := spec.ResourceGroupName()

	const (
		checkInterval = 5 * time.Second
		maxRounds     = 8
		totalTime     = maxRounds * checkInterval // 40 seconds
	)

	log.V(2).Info("starting bounded re-check for resource deletion",
		"service", serviceName, "resource", resourceName, "resourceGroup", rgName,
		"checkInterval", checkInterval, "maxRounds", maxRounds, "totalTime", totalTime)

	for round := 1; round <= maxRounds; round++ {
		log.V(3).Info("bounded re-check round",
			"service", serviceName, "resource", resourceName, "round", round, "maxRounds", maxRounds)

		exists, err := s.checkResourceExists(ctx, spec, serviceName, log)
		if err != nil {
			// If we can't check existence, continue to next round unless it's the final round
			if round == maxRounds {
				log.V(2).Info("final round failed to check existence, treating as deletion failure",
					"service", serviceName, "resource", resourceName, "round", round, "error", err.Error())
				return originalErr
			}

			log.V(3).Info("existence check failed, continuing to next round",
				"service", serviceName, "resource", resourceName, "round", round, "error", err.Error())
			time.Sleep(checkInterval)
			continue
		}

		if !exists {
			// Resource is gone! DELETE was successful
			log.V(2).Info("resource confirmed deleted during bounded re-check",
				"service", serviceName, "resource", resourceName, "resourceGroup", rgName,
				"round", round, "elapsedTime", time.Duration(round)*checkInterval)
			return nil
		}

		// Resource still exists
		if round == maxRounds {
			// Final round - resource still exists, surface the original error
			log.V(2).Info("resource still exists after bounded re-check, surfacing original error",
				"service", serviceName, "resource", resourceName, "resourceGroup", rgName,
				"rounds", maxRounds, "totalTime", totalTime)
			return originalErr
		}

		log.V(3).Info("resource still exists, continuing to next round",
			"service", serviceName, "resource", resourceName, "round", round)
		time.Sleep(checkInterval)
	}

	// This should never be reached, but just in case
	return originalErr
}

// requeueTime returns the time to wait before requeuing a reconciliation.
// It would be ideal to use the "retry-after" header from the API response, but
// that is not readily accessible in the SDK v2 Poller framework.
func requeueTime(timeouts azure.AsyncReconciler) time.Duration {
	return timeouts.DefaultedReconcilerRequeue()
}

// getRetryAfterFromError returns the time.Duration from the http.Response in the azcore.ResponseError.
// If there is no Response object, or if there is no meaningful Retry-After header data, it returns a default.
func getRetryAfterFromError(err error) time.Duration {
	// In case we aren't able to introspect Retry-After from the error type, we'll return this default
	ret := reconciler.DefaultReconcilerRequeue
	var responseError *azcore.ResponseError
	// if we have a strongly typed azcore.ResponseError then we can introspect the HTTP response data
	if errors.As(err, &responseError) && responseError.RawResponse != nil {
		// If we have Retry-After HTTP header data for any reason, prefer it
		if retryAfter := responseError.RawResponse.Header.Get("Retry-After"); retryAfter != "" {
			// This handles the case where Retry-After data is in the form of units of seconds
			if rai, err := strconv.Atoi(retryAfter); err == nil {
				ret = time.Duration(rai) * time.Second
				// This handles the case where Retry-After data is in the form of absolute time
			} else if t, err := time.Parse(time.RFC1123, retryAfter); err == nil {
				ret = time.Until(t)
			}
			// If we didn't find Retry-After HTTP header data but the response type is 429,
			// we'll have to come up with our sane default.
		} else if responseError.RawResponse.StatusCode == http.StatusTooManyRequests {
			ret = reconciler.DefaultHTTP429RetryAfter
		}
	}
	return ret
}
