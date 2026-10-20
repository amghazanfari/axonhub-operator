/*
Copyright 2026.

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

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	axonhubv1alpha1 "github.com/amghazanfari/axonhub-operator/api/v1alpha1"
	"github.com/go-logr/logr"
)

const (
	// axonhubContainerName is the name of the AxonHub container in the pod.
	axonhubContainerName = "axonhub"

	// axonhubContainerPort is the port AxonHub's HTTP server listens on.
	axonhubContainerPort = 8090

	// axonhubPortName is the name given to the HTTP port on both the
	// container and the Service.
	axonhubPortName = "http"

	// mutableTagSuffix is the tag suffix (or absence of a tag) that marks an
	// image reference as mutable, requiring an Always pull policy.
	mutableTagSuffix = "latest"

	// axonhubUID is the non-root UID/GID the AxonHub container runs as. The
	// upstream image has no USER directive (root), so an explicit identity
	// is required for restricted-PSS compliance.
	axonhubUID = int64(1000)

	// AxonHubConditionReady is the condition type reporting overall readiness.
	AxonHubConditionReady = "Ready"

	// Reasons explaining the Ready condition.
	ReasonDeploymentReady                = "DeploymentReady"
	ReasonDeploymentStatusNotObservedYet = "DeploymentStatusNotObservedYet"
	ReasonDeploymentGenerationStale      = "DeploymentGenerationStale"
	ReasonDeploymentUnavailable          = "DeploymentUnavailable"
	ReasonDeploymentReplicasNotReady     = "DeploymentReplicasNotReady"
	ReasonDeploymentAvailableUnknown     = "DeploymentAvailableUnknown"
	ReasonDeploymentRolloutInProgress    = "DeploymentRolloutInProgress"

	// ReasonReconcileFailed reports a reconcile error that keeps retrying.
	// It is stage-neutral (deployment or service) so tooling keyed on
	// reasons is not misled by a Deployment-named reason on a Service
	// failure; the message names the failing stage.
	ReasonReconcileFailed = "ReconcileFailed"

	// ReasonChildObjectAlreadyOwned reports that a pre-existing child object
	// (Deployment or Service) with the same name as the AxonHub resource is
	// already controlled by another controller, so it cannot be adopted.
	ReasonChildObjectAlreadyOwned = "ChildObjectAlreadyOwned"

	// alreadyOwnedRequeueInterval is the recovery policy for an
	// AlreadyOwned conflict: instead of retrying with the controller's
	// exponential backoff on every failure (a hot retry loop), the
	// reconcile requeues periodically to detect when the conflicting
	// object has been removed.
	alreadyOwnedRequeueInterval = 2 * time.Minute
)

// AxonHubReconciler reconciles a AxonHub object.
type AxonHubReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Record emits warning Events on the AxonHub resource for conditions
	// that need operator attention (e.g. child-object ownership conflicts).
	// It may be nil in tests; event emission is skipped then.
	Record record.EventRecorder
}

// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs,verbs=get;list;watch
// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs/status,verbs=get;update;patch
// axonhubs/finalizers is not unused: SetControllerReference sets
// blockOwnerDeletion on the child objects, and with
// OwnerReferencesPermissionEnforcement enabled (e.g. OpenShift) creating or
// updating owned children is forbidden without update permission on the
// owner's finalizers subresource.
// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch
// The reconciler emits warning Events on the AxonHub resource when a child
// object cannot be adopted (AlreadyOwned conflicts).
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile moves the cluster state closer to the state described by the
// AxonHub custom resource.
func (r *AxonHubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("axonhub", req.NamespacedName)

	var axonhub axonhubv1alpha1.AxonHub
	if err := r.Get(ctx, req.NamespacedName, &axonhub); err != nil {
		if apierrors.IsNotFound(err) {
			// The resource was deleted; owned children are garbage-collected.
			logger.Info("AxonHub resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	deployment, err := r.reconcileDeployment(ctx, &axonhub)
	if err != nil {
		// A pre-existing child object already controlled by another
		// controller is a configuration conflict the operator cannot
		// resolve on its own: surface it on the status and as an Event,
		// and back off instead of hot-retrying on every failure.
		var owned *controllerutil.AlreadyOwnedError
		if errors.As(err, &owned) {
			return r.reportChildConflict(ctx, logger, &axonhub, "Deployment", owned)
		}
		// Surface the failure on the status before returning the error,
		// so the Ready condition does not freeze at a stale value while
		// the reconcile keeps retrying (e.g. a drifted Deployment that
		// cannot be reconciled).
		if statusErr := r.reportReconcileFailure(ctx, &axonhub, "deployment", err); statusErr != nil {
			logger.Error(statusErr, "failed to report reconcile failure on the AxonHub status")
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcileService(ctx, &axonhub); err != nil {
		var owned *controllerutil.AlreadyOwnedError
		if errors.As(err, &owned) {
			return r.reportChildConflict(ctx, logger, &axonhub, "Service", owned)
		}
		// Mirror the Deployment error path so the Ready condition does not
		// freeze at a stale value while the Service reconcile keeps
		// retrying.
		if statusErr := r.reportReconcileFailure(ctx, &axonhub, "service", err); statusErr != nil {
			logger.Error(statusErr, "failed to report reconcile failure on the AxonHub status")
		}
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, &axonhub, deployment); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating AxonHub status: %w", err)
	}

	return ctrl.Result{}, nil
}

// setReadyCondition sets the Ready condition on the given (working copy of
// the) status and persists it if the status changed (including incidental
// changes such as an observedGeneration bump, so the status never drifts
// from the resource).
func (r *AxonHubReconciler) setReadyCondition(ctx context.Context, axonhub *axonhubv1alpha1.AxonHub, status *axonhubv1alpha1.AxonHubStatus, condition metav1.Condition) error {
	status.ObservedGeneration = axonhub.Generation
	meta.SetStatusCondition(&status.Conditions, condition)

	if equality.Semantic.DeepEqual(axonhub.Status, *status) {
		return nil
	}
	axonhub.Status = *status
	return r.Status().Update(ctx, axonhub)
}

// reportReconcileFailure sets the Ready condition to False with a
// ReconcileFailed reason, so reconcile errors that keep retrying (such as a
// drifted owned Deployment) are visible on the status instead of silently
// freezing it at stale values. The stage names the part of the reconcile
// that failed ("deployment" or "service").
func (r *AxonHubReconciler) reportReconcileFailure(ctx context.Context, axonhub *axonhubv1alpha1.AxonHub, stage string, reconcileErr error) error {
	status := axonhub.DeepCopy().Status
	err := r.setReadyCondition(ctx, axonhub, &status, metav1.Condition{
		Type:               AxonHubConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: axonhub.Generation,
		Reason:             ReasonReconcileFailed,
		Message:            fmt.Sprintf("Reconciling the AxonHub %s failed: %s", stage, reconcileErr.Error()),
	})
	return err
}

// reportChildConflict surfaces an AlreadyOwned conflict — a pre-existing
// child object (Deployment or Service) already controlled by another
// controller — as a Ready=False condition with an actionable message and a
// warning Event, then schedules a periodic requeue instead of returning an
// error, so the controller does not hot-retry. The conflict resolves itself
// once the conflicting object is deleted or the AxonHub resource renamed;
// the periodic requeue picks that up (the conflict object is not owned by
// this controller, so its deletion does not trigger a watch event —
// recovery can lag up to the full requeue interval). The warning Event is
// emitted only when the Ready condition itself transitions (not on
// incidental status changes such as an observedGeneration bump) to avoid
// spamming.
func (r *AxonHubReconciler) reportChildConflict(ctx context.Context, logger logr.Logger, axonhub *axonhubv1alpha1.AxonHub, kind string, conflict *controllerutil.AlreadyOwnedError) (ctrl.Result, error) {
	message := fmt.Sprintf(
		"%s %s/%s is already controlled by %s %q (uid %s); delete the conflicting object or rename the AxonHub resource so it can be recreated",
		kind, conflict.Object.GetNamespace(), conflict.Object.GetName(),
		conflict.Owner.Kind, conflict.Owner.Name, conflict.Owner.UID,
	)

	status := axonhub.DeepCopy().Status
	// The managed Deployment is not reconciled while the conflict persists,
	// so any previously reported readyReplicas would be stale; zero it to
	// keep the status coherent with Ready=False.
	status.ReadyReplicas = 0
	previous := meta.FindStatusCondition(status.Conditions, AxonHubConditionReady)
	condition := metav1.Condition{
		Type:               AxonHubConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: axonhub.Generation,
		Reason:             ReasonChildObjectAlreadyOwned,
		Message:            message,
	}
	// A transition means the condition itself changed — not just any status
	// write (e.g. an observedGeneration bump after a spec edit must not
	// re-fire the Event while the conflict persists).
	transitioned := previous == nil ||
		previous.Status != condition.Status ||
		previous.Reason != condition.Reason ||
		previous.Message != condition.Message

	if err := r.setReadyCondition(ctx, axonhub, &status, condition); err != nil {
		return ctrl.Result{}, fmt.Errorf("reporting ownership conflict on the AxonHub status: %w", err)
	}

	if r.Record != nil {
		if transitioned {
			r.Record.Eventf(axonhub, corev1.EventTypeWarning, ReasonChildObjectAlreadyOwned, "%s", message)
		}
	} else {
		logger.Info("child object ownership conflict (no event recorder configured)", "message", message)
	}

	logger.Info("child object ownership conflict; backing off", "requeueAfter", alreadyOwnedRequeueInterval, "message", message)
	return ctrl.Result{RequeueAfter: alreadyOwnedRequeueInterval}, nil
}

// reconcileDeployment ensures a Deployment owned by the AxonHub resource
// exists and matches the desired spec.
//
// The mutate callback merges our labels into any existing ones and assigns
// the fields the operator owns. The immutable pod-template selector is set
// on creation only; the axonhub container's spec (image, pull policy, env,
// ports, security context, resources) is fully derived from the AxonHub
// resource on every reconcile, so drifted or adopted Deployments are
// repaired. A Deployment without an axonhub container cannot be reconciled
// and fails loudly instead of silently no-oping.
func (r *AxonHubReconciler) reconcileDeployment(ctx context.Context, axonhub *axonhubv1alpha1.AxonHub) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      axonhub.Name,
			Namespace: axonhub.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		// Set the owner reference inside the mutate callback so that a
		// pre-existing same-named Deployment is adopted too.
		if err := ctrl.SetControllerReference(axonhub, deployment, r.Scheme); err != nil {
			return fmt.Errorf("setting controller reference: %w", err)
		}

		// Merge our labels into any labels other actors may have set, so
		// they are preserved instead of replaced wholesale.
		if deployment.Labels == nil {
			deployment.Labels = map[string]string{}
		}
		mergeLabels(deployment.Labels, labelsForAxonHub(axonhub.Name))
		replicas := r.desiredReplicas(axonhub)
		deployment.Spec.Replicas = &replicas

		if deployment.CreationTimestamp.IsZero() {
			// Create path: the selector is immutable, so it can only be
			// set once; the template skeleton is defined here.
			deployment.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: labelsForAxonHub(axonhub.Name),
			}
			deployment.Spec.Template = corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: axonhubContainerName,
					}},
				},
			}
		}
		if deployment.Spec.Template.Labels == nil {
			deployment.Spec.Template.Labels = map[string]string{}
		}
		mergeLabels(deployment.Spec.Template.Labels, labelsForAxonHub(axonhub.Name))

		// Applied on every reconcile so drifted or adopted Deployments are
		// also brought up to restricted-PSS-compatible defaults.
		deployment.Spec.Template.Spec.SecurityContext = podSecurityContext()

		// Both paths converge here: the axonhub container's spec is fully
		// derived from the AxonHub resource on every reconcile.
		if err := ensureAxonHubContainer(deployment, axonhub); err != nil {
			return fmt.Errorf("ensuring axonhub container: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ensuring deployment: %w", err)
	}
	return deployment, nil
}

// desiredServicePorts returns the Service port exposing the AxonHub HTTP
// server.
func desiredServicePorts() []corev1.ServicePort {
	return []corev1.ServicePort{{
		Name:       axonhubPortName,
		Port:       axonhubContainerPort,
		TargetPort: intstr.FromString(axonhubPortName),
		Protocol:   corev1.ProtocolTCP,
	}}
}

// reconcileService ensures a ClusterIP Service owned by the AxonHub resource
// exists, routing traffic to the managed Deployment's pods on the AxonHub
// HTTP port.
//
// As with the Deployment, the mutate callback only assigns fields the
// operator owns: the selector is immutable so it is set on creation only;
// on updates just the labels and the ports are mutated so steady-state
// reconciles remain no-ops. A pre-existing Service with a conflicting
// immutable selector is surfaced as an error instead of being silently
// misrouted.
func (r *AxonHubReconciler) reconcileService(ctx context.Context, axonhub *axonhubv1alpha1.AxonHub) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      axonhub.Name,
			Namespace: axonhub.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		// Set the owner reference inside the mutate callback so that a
		// pre-existing same-named Service is adopted too.
		if err := ctrl.SetControllerReference(axonhub, service, r.Scheme); err != nil {
			return fmt.Errorf("setting controller reference: %w", err)
		}

		service.Labels = labelsForAxonHub(axonhub.Name)

		if service.CreationTimestamp.IsZero() {
			// Create path: the selector is immutable, so it can only be
			// set once.
			service.Spec.Selector = labelsForAxonHub(axonhub.Name)
			service.Spec.Type = corev1.ServiceTypeClusterIP
			service.Spec.Ports = desiredServicePorts()
			return nil
		}

		// Update path: the selector is immutable, so a pre-existing
		// Service that selects different pods can never route to the
		// managed Deployment — fail loudly instead of reporting success.
		if !equality.Semantic.DeepEqual(service.Spec.Selector, labelsForAxonHub(axonhub.Name)) {
			return fmt.Errorf("service %s/%s has a conflicting immutable selector; delete it or rename the AxonHub resource so it can be recreated",
				service.Namespace, service.Name)
		}

		// Update path: mutate only the fields derived from the spec.
		// (An adopted Service of another type, e.g. NodePort or
		// LoadBalancer, keeps its type: the API server forbids changing
		// it back to ClusterIP.)
		if service.Spec.Type == "" {
			service.Spec.Type = corev1.ServiceTypeClusterIP
		}
		service.Spec.Ports = desiredServicePorts()
		return nil
	})
	if err != nil {
		return fmt.Errorf("ensuring service: %w", err)
	}
	return nil
}

// dbEnvForAxonHub returns the container environment variables configuring
// the AxonHub database, derived from spec.db. A nil DB returns nil so
// AxonHub's own defaults apply.
//
// When spec.db.addressSecret is set, the DSN is injected through an
// env.valueFrom.secretKeyRef instead of a literal value, so credentials
// never live in the AxonHub resource (which is readable by anyone with CR
// read access and is often logged or audited). The API validation guarantees
// that exactly one of address and addressSecret is set. The Secret is not
// read by the controller (and is therefore not required to exist at reconcile
// time); the kubelet resolves it when starting the pod, so a missing Secret
// surfaces as a pod-level CreateContainerConfigError.
func dbEnvForAxonHub(axonhub *axonhubv1alpha1.AxonHub) []corev1.EnvVar {
	if axonhub.Spec.DB == nil {
		return nil
	}
	dsn := corev1.EnvVar{Name: "AXONHUB_DB_DSN"}
	if secret := axonhub.Spec.DB.AddressSecret; secret != nil {
		dsn.ValueFrom = &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name},
				Key:                  secret.Key,
			},
		}
	} else {
		dsn.Value = axonhub.Spec.DB.Address
	}
	return []corev1.EnvVar{
		{Name: "AXONHUB_DB_DIALECT", Value: axonhub.Spec.DB.Type},
		dsn,
	}
}

// ensureAxonHubContainer reconciles the axonhub container of the Deployment's
// pod template with the desired spec. The container must already exist: a
// drifted or adopted Deployment without it cannot be reconciled silently.
func ensureAxonHubContainer(deployment *appsv1.Deployment, axonhub *axonhubv1alpha1.AxonHub) error {
	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]
		if container.Name != axonhubContainerName {
			continue
		}
		container.Image = axonhub.Spec.Image
		container.ImagePullPolicy = pullPolicyFor(axonhub.Spec.Image)
		container.Env = dbEnvForAxonHub(axonhub)
		container.Ports = []corev1.ContainerPort{{
			Name:          axonhubPortName,
			ContainerPort: axonhubContainerPort,
			Protocol:      corev1.ProtocolTCP,
		}}
		container.SecurityContext = containerSecurityContext()
		container.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		}
		return nil
	}
	return fmt.Errorf("deployment %s/%s has no %q container; refusing to mutate a drifted or adopted Deployment",
		deployment.Namespace, deployment.Name, axonhubContainerName)
}

// pullPolicyFor returns the image pull policy for the given image reference.
// Mutable tags (":latest" or no tag at all) must always be pulled, otherwise
// re-pushed images would never reach nodes that already cached the tag.
// Digest references (img@sha256:...) and pinned tags are immutable, so
// IfNotPresent is safe for them.
func pullPolicyFor(image string) corev1.PullPolicy {
	if strings.Contains(image, "@") {
		return corev1.PullIfNotPresent
	}
	tag := image[strings.LastIndex(image, "/")+1:]
	if i := strings.LastIndex(tag, ":"); i >= 0 {
		tag = tag[i+1:]
	} else {
		tag = "" // no tag means :latest
	}
	if tag == "" || tag == mutableTagSuffix {
		return corev1.PullAlways
	}
	return corev1.PullIfNotPresent
}

// podSecurityContext returns the pod-level security context, compatible with
// the restricted Pod Security Standard. The upstream looplj/axonhub image
// does not declare a USER and defaults to root, so an explicit non-root
// UID/GID is required for RunAsNonRoot to hold. fsGroup makes mounted PVCs
// group-writable so AxonHub can persist its SQLite database.
func podSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsUser:    ptr.To[int64](axonhubUID),
		RunAsGroup:   ptr.To[int64](axonhubUID),
		RunAsNonRoot: ptr.To(true),
		FSGroup:      ptr.To[int64](axonhubUID),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// containerSecurityContext returns the container-level security context,
// compatible with the restricted Pod Security Standard.
func containerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// mergeLabels merges src into dst without removing keys already present in
// dst that are not managed by the operator.
func mergeLabels(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

// desiredReplicas returns the effective replica count for the resource.
func (r *AxonHubReconciler) desiredReplicas(axonhub *axonhubv1alpha1.AxonHub) int32 {
	if axonhub.Spec.Replicas != nil {
		return *axonhub.Spec.Replicas
	}
	return 1
}

// labelsForAxonHub returns the labels attached to every object owned by the
// given AxonHub resource.
func labelsForAxonHub(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "axonhub",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/managed-by": "axonhub-operator",
	}
}

// updateStatus reports the observed state of the managed Deployment on the
// AxonHub resource's status.
func (r *AxonHubReconciler) updateStatus(ctx context.Context, axonhub *axonhubv1alpha1.AxonHub, deployment *appsv1.Deployment) error {
	status := axonhub.DeepCopy().Status
	status.ReadyReplicas = deployment.Status.ReadyReplicas

	ready, reason, message := deploymentReadiness(deployment, r.desiredReplicas(axonhub))
	condition := metav1.Condition{
		Type:               AxonHubConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: axonhub.Generation,
		Reason:             reason,
		Message:            message,
	}
	if ready {
		condition.Status = metav1.ConditionTrue
		condition.Reason = ReasonDeploymentReady
		condition.Message = "AxonHub deployment is available"
	}

	err := r.setReadyCondition(ctx, axonhub, &status, condition)
	return err
}

// deploymentReadiness reports whether the Deployment has finished rolling
// out and is serving the desired replica count.
//
// Checking only ReadyReplicas >= desired is not sufficient: during a rolling
// update with surge pods the ready count can reach the desired value while
// old pods are still terminating and the rollout is in progress. The
// Available condition alone is not sufficient either: it stays True
// mid-rollout to preserve minimum availability, and ObservedGeneration is
// set as soon as the Deployment controller observes the new spec, not when
// the rollout completes. Therefore readiness requires all of: the latest
// generation observed, Available=True, ReadyReplicas >= desired, and
// UpdatedReplicas >= desired (all replicas running the current template).
func deploymentReadiness(deployment *appsv1.Deployment, desiredReplicas int32) (bool, string, string) {
	if deployment.Status.ObservedGeneration == 0 {
		return false, ReasonDeploymentStatusNotObservedYet, "Deployment status has not been observed yet"
	}
	if deployment.Status.ObservedGeneration < deployment.Generation {
		return false, ReasonDeploymentGenerationStale,
			fmt.Sprintf("Deployment controller has not observed generation %d yet", deployment.Generation)
	}

	available := findDeploymentCondition(deployment.Status.Conditions, appsv1.DeploymentAvailable)

	// Note on precedence: when the Available condition is False, that is
	// reported even if the ready replica count is also below the desired
	// value; the Deployment controller's own condition is the more
	// actionable signal.
	if available != nil && available.Status == corev1.ConditionFalse {
		return false, ReasonDeploymentUnavailable,
			fmt.Sprintf("Deployment Available condition is False: %s", available.Message)
	}

	if deployment.Status.ReadyReplicas < desiredReplicas {
		return false, ReasonDeploymentReplicasNotReady,
			fmt.Sprintf("Deployment has %d/%d ready replicas", deployment.Status.ReadyReplicas, desiredReplicas)
	}

	if deployment.Status.UpdatedReplicas < desiredReplicas {
		// Mid rolling update: enough ready pods, but some still run the
		// previous pod template (Available stays True to keep minimum
		// availability). The rollout is not complete yet.
		return false, ReasonDeploymentRolloutInProgress,
			fmt.Sprintf("Deployment rollout in progress: %d/%d replicas updated to the current template",
				deployment.Status.UpdatedReplicas, desiredReplicas)
	}

	if available == nil || available.Status != corev1.ConditionTrue {
		// Replicas are ready but the Deployment controller has not (yet)
		// published an Available condition (or published it as Unknown);
		// treat the rollout as still in progress rather than reporting
		// ready prematurely.
		return false, ReasonDeploymentAvailableUnknown,
			"Deployment Available condition is missing or not True yet"
	}

	return true, ReasonDeploymentReady, "AxonHub deployment is available"
}

// findDeploymentCondition returns the condition of the given type from the
// Deployment's status conditions, or nil if it is not present.
func findDeploymentCondition(conditions []appsv1.DeploymentCondition, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AxonHubReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&axonhubv1alpha1.AxonHub{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("axonhub").
		Complete(r)
}
