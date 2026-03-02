/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

// Package controller contains the functions in pgbouncer instance manager
// that reacts to changes in the Pooler resource.
package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	pgbouncerSpecs "github.com/cloudnative-pg/cloudnative-pg/pkg/specs/pgbouncer"
)

const (
	// endpointsWatchRetryInterval is the interval between retries when the
	// Endpoints watch fails (e.g. RBAC not ready at pod startup).
	endpointsWatchRetryInterval = 5 * time.Second
	// reloadBackoffDuration is the initial delay before retrying PgBouncer
	// RELOAD when the admin socket is not ready yet (e.g. right after startup).
	reloadBackoffDuration = 100 * time.Millisecond
	// reloadBackoffSteps is the max number of retries for RELOAD when the
	// socket is not ready.
	reloadBackoffSteps = 8
)

// PgBouncerReconciler reconciles the status of the Pooler resource with
// the one of this pgbouncer instance
type PgBouncerReconciler struct {
	client               ctrl.WithWatch
	poolerWatch          watch.Interface
	endpointsWatch       watch.Interface
	instance             PgBouncerInstanceInterface
	poolerNamespacedName types.NamespacedName
}

// NewPgBouncerReconciler creates a new pgbouncer reconciler
func NewPgBouncerReconciler(poolerNamespacedName types.NamespacedName) (*PgBouncerReconciler, error) {
	client, err := management.NewControllerRuntimeClient()
	if err != nil {
		return nil, err
	}

	return &PgBouncerReconciler{
		client:               client,
		instance:             NewPgBouncerInstance(),
		poolerNamespacedName: poolerNamespacedName,
	}, nil
}

// Run runs the reconciliation loop for this resource
func (r *PgBouncerReconciler) Run(ctx context.Context) {
	contextLogger := log.FromContext(ctx)

	for {
		// Retry with exponential back-off, unless it is a connection refused error
		err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
			contextLogger.Error(err, "Error calling Watch")
			return !utilnet.IsConnectionRefused(err)
		}, func() error {
			return r.watch(ctx)
		})
		if err != nil {
			// If this is "connection refused" error, it means that apiserver is probably not responsive.
			// If that's the case wait and resend watch request.
			time.Sleep(time.Second)
		}
	}
}

// watch contains the main reconciler loop
func (r *PgBouncerReconciler) watch(ctx context.Context) error {
	reconcilerWatchCtx, reconcilerWatchCancel := context.WithCancel(ctx)
	defer reconcilerWatchCancel()

	contextLogger := log.FromContext(ctx)

	var err error

	r.poolerWatch, err = r.client.Watch(reconcilerWatchCtx, &apiv1.PoolerList{}, &ctrl.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", r.poolerNamespacedName.Name),
		Namespace:     r.poolerNamespacedName.Namespace,
	})
	if err != nil {
		return fmt.Errorf("error watching pooler: %w", err)
	}
	defer r.Stop()

	headlessServiceName := r.poolerNamespacedName.Name + pgbouncerSpecs.HeadlessServiceSuffix
	r.endpointsWatch, err = r.client.Watch(reconcilerWatchCtx, &corev1.EndpointsList{}, &ctrl.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", headlessServiceName),
		Namespace:     r.poolerNamespacedName.Namespace,
	})

	// If the endpoints watch fails (e.g. RBAC not yet applied), start a
	// background goroutine that retries until it succeeds. This avoids
	// permanently losing peer discovery when the operator hasn't propagated
	// the Role yet at pod startup.
	var endpointsWatchCh <-chan watch.Interface
	if err != nil {
		contextLogger.Info("Cannot watch endpoints for peer discovery yet, will retry in background",
			"service", headlessServiceName, "error", err)
		ch := make(chan watch.Interface) // unbuffered: send only succeeds when loop is receiving, avoids orphaned watch
		endpointsWatchCh = ch
		go r.retryEndpointsWatch(reconcilerWatchCtx, headlessServiceName, ch)
	}

	poolerCh := r.poolerWatch.ResultChan()
	var endpointsCh <-chan watch.Event
	if r.endpointsWatch != nil {
		endpointsCh = r.endpointsWatch.ResultChan()
	}

	for {
		select {
		case event, ok := <-poolerCh:
			if !ok {
				return nil
			}
			receivedEvent := event
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				return r.Reconcile(ctx, &receivedEvent)
			})
			if err != nil {
				contextLogger.Error(err, "Reconciliation error")
			}

		case _, ok := <-endpointsCh:
			if !ok {
				endpointsCh = nil
				continue
			}
			if err := r.synchronizeConfigFromEndpoints(ctx); err != nil {
				contextLogger.Error(err, "Reconciliation error from endpoints change")
			}

		case w := <-endpointsWatchCh:
			r.endpointsWatch = w
			endpointsCh = w.ResultChan()
			endpointsWatchCh = nil
			contextLogger.Info("Endpoints watch established, peer discovery now active")
			if err := r.synchronizeConfigFromEndpoints(ctx); err != nil {
				contextLogger.Error(err, "Reconciliation error after endpoints watch established")
			}
		}
	}
}

// retryEndpointsWatch periodically attempts to establish an Endpoints watch
// until it succeeds or the context is canceled.
func (r *PgBouncerReconciler) retryEndpointsWatch(
	ctx context.Context,
	headlessServiceName string,
	result chan<- watch.Interface,
) {
	contextLogger := log.FromContext(ctx)
	ticker := time.NewTicker(endpointsWatchRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w, err := r.client.Watch(ctx, &corev1.EndpointsList{}, &ctrl.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("metadata.name", headlessServiceName),
				Namespace:     r.poolerNamespacedName.Namespace,
			})
			if err != nil {
				contextLogger.Debug("Retrying endpoints watch", "error", err)
				continue
			}
			select {
			case result <- w:
				return
			case <-ctx.Done():
				w.Stop()
				return
			}
		}
	}
}

// Stop stops the controller
func (r *PgBouncerReconciler) Stop() {
	if r.poolerWatch != nil {
		r.poolerWatch.Stop()
	}
	if r.endpointsWatch != nil {
		r.endpointsWatch.Stop()
	}
}

// GetClient returns the dynamic client that is being used for a certain reconciler
func (r *PgBouncerReconciler) GetClient() ctrl.Client {
	return r.client
}

// Reconcile is the main reconciliation loop for the pgbouncer instance
func (r *PgBouncerReconciler) Reconcile(ctx context.Context, event *watch.Event) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Debug(
		"Reconciliation loop",
		"eventType", event.Type,
		"type", event.Object.GetObjectKind().GroupVersionKind())

	pooler, ok := event.Object.(*apiv1.Pooler)
	if !ok {
		return fmt.Errorf("error decoding pooler resource")
	}

	err := r.synchronizeConfig(ctx, pooler)
	if err != nil {
		return fmt.Errorf("while reconciling configuration: %w", err)
	}

	return r.synchronizePause(pooler)
}

// synchronizePause ensure that the pause flag inside the Pooler
// specification matches the PgBouncer status
func (r *PgBouncerReconciler) synchronizePause(pooler *apiv1.Pooler) error {
	isPaused := r.instance.Paused()
	shouldBePaused := pooler.Spec.PgBouncer.IsPaused()
	if shouldBePaused && !isPaused {
		if err := r.instance.Pause(); err != nil {
			return fmt.Errorf("while pausing instance: %w", err)
		}
	}
	if !shouldBePaused && isPaused {
		if err := r.instance.Resume(); err != nil {
			return fmt.Errorf("while resuming instance: %w", err)
		}
	}
	return nil
}

// synchronizeConfig ensure that the configuration derived from
// the pooler specification matches the one loaded in PgBouncer
func (r *PgBouncerReconciler) synchronizeConfig(ctx context.Context, pooler *apiv1.Pooler) error {
	var (
		configurationChanged bool
		err                  error
	)

	if configurationChanged, err = r.writePgBouncerConfig(ctx, pooler); err != nil {
		return fmt.Errorf("while writing PgBouncer configuration: %w", err)
	}

	if !configurationChanged {
		return nil
	}

	// PgBouncer may not have created its Unix socket yet (e.g. right after startup
	// or when an Endpoints event fires before PgBouncer is ready). Retry briefly.
	reloadBackoff := retry.DefaultRetry
	reloadBackoff.Duration = reloadBackoffDuration
	reloadBackoff.Steps = reloadBackoffSteps
	if err = retry.OnError(reloadBackoff, isSocketNotReady, func() error {
		return r.instance.Reload()
	}); err != nil {
		return fmt.Errorf("while reloading configuration due to change: %w", err)
	}

	return nil
}

// isSocketNotReady returns true when the error indicates PgBouncer's admin socket
// is not available yet (process still starting), so the caller can retry.
func isSocketNotReady(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "no such file or directory") ||
		strings.Contains(s, "connection refused")
}

// synchronizeConfigFromEndpoints re-reads the Pooler resource and
// synchronizes the PgBouncer configuration when endpoints change.
// This ensures the [peers] section is updated when pods are added or removed.
func (r *PgBouncerReconciler) synchronizeConfigFromEndpoints(ctx context.Context) error {
	var pooler apiv1.Pooler
	if err := r.GetClient().Get(ctx, r.poolerNamespacedName, &pooler); err != nil {
		return fmt.Errorf("while getting pooler for endpoints reconciliation: %w", err)
	}
	return r.synchronizeConfig(ctx, &pooler)
}

// writePgBouncerConfig writes the PgBouncer configuration files given the Pooler
// specification, returning a boolean flag indicating if the configuration has
// changed or not
func (r *PgBouncerReconciler) writePgBouncerConfig(ctx context.Context, pooler *apiv1.Pooler) (bool, error) {
	var (
		secrets     *config.Secrets
		configFiles config.ConfigurationFiles

		err error
	)

	// If this is the first reconciliation loop the API server may
	// not have applied the RBAC rules created by the controller.
	// In this case we still don't have the permissions to read
	// the secrets we require.
	// This is why we are retrying the loading of the secrets.
	if err := retry.OnError(retry.DefaultBackoff, apierrs.IsForbidden, func() error {
		secrets, err = getSecrets(ctx, r.GetClient(), pooler)
		return err
	}); err != nil {
		return false, fmt.Errorf("while reading secrets: %w", err)
	}

	if configFiles, err = config.BuildConfigurationFiles(pooler, secrets, r.getPeeringInfo(ctx)); err != nil {
		return false, fmt.Errorf("while generating pgbouncer configuration: %w", err)
	}

	return refreshConfigurationFiles(ctx, configFiles)
}

// Init ensures that all PgBouncer requirement are met.
//
// In detail:
// 1. create the pgbouncer configuration and the required secrets
// 2. ensure that every needed folder is existent
func (r *PgBouncerReconciler) Init(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	var pooler apiv1.Pooler

	// Get the pooler from the API Server
	if err := r.GetClient().Get(ctx, r.poolerNamespacedName, &pooler); err != nil {
		return fmt.Errorf("while getting pooler for the first time: %w", err)
	}

	// Write the startup configuration for PgBouncer
	if _, err := r.writePgBouncerConfig(ctx, &pooler); err != nil {
		return err
	}

	// Ensure we have the directory to store the controlling socket
	if err := fileutils.EnsureDirectoryExists(config.PgBouncerSocketDir); err != nil {
		contextLogger.Error(err, "while checking socket directory existed", "dir", config.PgBouncerSocketDir)
		return err
	}

	return nil
}
