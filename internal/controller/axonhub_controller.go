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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"strings"

	axonhubv1alpha1 "github.com/amghazanfari/axonhub-operator/api/v1alpha1"
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
)

// AxonHubReconciler reconciles a AxonHub object.
type AxonHubReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs,verbs=get;list;watch
// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch

// Reconcile moves the cluster state closer to the state described by the
// AxonHub custom resource.
func (r *AxonHubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("axonhub", req.NamespacedName)

	var axonhub axonhubv1alpha1.AxonHub
	if err := r.Get(ctx, req.NamespacedName, &axonhub); err != nil {
		if apierrors.IsNotFound(err) {
			// The resource was deleted; owned children are garbage-collected.
			log.Info("AxonHub resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	deployment, err := r.reconcileDeployment(ctx, &axonhub)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileService(ctx, &axonhub); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, &axonhub, deployment); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating AxonHub status: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcileDeployment ensures a Deployment owned by the AxonHub resource
// exists and matches the desired spec.
//
// The mutate callback only assigns fields the operator owns. The full
// pod template is set on creation only; on updates just the desired
// replicas and image are mutated so that API-server-defaulted fields are
// preserved and steady-state reconciles are true no-ops.
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
func dbEnvForAxonHub(axonhub *axonhubv1alpha1.AxonHub) []corev1.EnvVar {
	if axonhub.Spec.DB == nil {
		return nil
	}
	return []corev1.EnvVar{
		{Name: "AXONHUB_DB_DIALECT", Value: axonhub.Spec.DB.Type},
		{Name: "AXONHUB_DB_DSN", Value: axonhub.Spec.DB.Address},
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
	if tag == mutableTagSuffix {
		return corev1.PullAlways
	}
	return corev1.PullIfNotPresent
}

// podSecurityContext returns the pod-level security context, compatible with
// the restricted Pod Security Standard.
func podSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
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
	status.ObservedGeneration = axonhub.Generation
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
	meta.SetStatusCondition(&status.Conditions, condition)

	if !equality.Semantic.DeepEqual(axonhub.Status, status) {
		axonhub.Status = status
		if err := r.Status().Update(ctx, axonhub); err != nil {
			return err
		}
	}
	return nil
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
