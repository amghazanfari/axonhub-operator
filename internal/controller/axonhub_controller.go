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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

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

	// AxonHubConditionReady is the condition type reporting overall readiness.
	AxonHubConditionReady = "Ready"

	// ReasonDeploymentReady / ReasonDeploymentNotReady explain the Ready condition.
	ReasonDeploymentReady    = "DeploymentReady"
	ReasonDeploymentNotReady = "DeploymentNotReady"
)

// AxonHubReconciler reconciles a AxonHub object.
type AxonHubReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs,verbs=get;list;watch
// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs/finalizers,verbs=update
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

		deployment.Labels = labelsForAxonHub(axonhub.Name)
		replicas := r.desiredReplicas(axonhub)
		deployment.Spec.Replicas = &replicas

		if deployment.CreationTimestamp.IsZero() {
			// Create path: the selector is immutable, so it can only be
			// set once; the full template is defined here.
			deployment.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: labelsForAxonHub(axonhub.Name),
			}
			deployment.Spec.Template = corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labelsForAxonHub(axonhub.Name),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            axonhubContainerName,
						Image:           axonhub.Spec.Image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Ports: []corev1.ContainerPort{{
							Name:          axonhubPortName,
							ContainerPort: axonhubContainerPort,
							Protocol:      corev1.ProtocolTCP,
						}},
					}},
				},
			}
			return nil
		}

		// Update path: mutate only the fields derived from the spec.
		deployment.Spec.Template.Labels = labelsForAxonHub(axonhub.Name)
		for i := range deployment.Spec.Template.Spec.Containers {
			if deployment.Spec.Template.Spec.Containers[i].Name == axonhubContainerName {
				deployment.Spec.Template.Spec.Containers[i].Image = axonhub.Spec.Image
			}
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

	ready := deployment.Status.ReadyReplicas >= r.desiredReplicas(axonhub)
	condition := metav1.Condition{
		Type:               AxonHubConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: axonhub.Generation,
		Reason:             ReasonDeploymentNotReady,
		Message:            "AxonHub deployment is not yet ready",
	}
	if ready {
		condition.Status = metav1.ConditionTrue
		condition.Reason = ReasonDeploymentReady
		condition.Message = "AxonHub deployment is ready"
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

// SetupWithManager sets up the controller with the Manager.
func (r *AxonHubReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&axonhubv1alpha1.AxonHub{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("axonhub").
		Complete(r)
}
