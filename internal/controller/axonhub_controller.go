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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	axonhubv1alpha1 "github.com/amghazanfari/axonhub-operator/api/v1alpha1"
)

const (
	// axonhubContainerPort is the port AxonHub's HTTP server listens on.
	axonhubContainerPort = 8090
)

// AxonHubReconciler reconciles a AxonHub object.
type AxonHubReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=axonhub.onrender.com,resources=axonhubs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

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

	if err := r.updateStatus(ctx, &axonhub, deployment); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileDeployment ensures a Deployment owned by the AxonHub resource
// exists and matches the desired spec.
func (r *AxonHubReconciler) reconcileDeployment(ctx context.Context, axonhub *axonhubv1alpha1.AxonHub) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      axonhub.Name,
			Namespace: axonhub.Namespace,
		},
	}

	if err := ctrl.SetControllerReference(axonhub, deployment, r.Scheme); err != nil {
		return nil, err
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		replicas := r.desiredReplicas(axonhub)
		deployment.Spec.Replicas = &replicas
		deployment.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: labelsForAxonHub(axonhub.Name),
		}
		// The strategy is only set on creation to avoid fighting with
		// Kubernetes defaults on subsequent updates.
		if deployment.CreationTimestamp.IsZero() {
			deployment.Spec.Strategy = appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			}
		}
		deployment.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labelsForAxonHub(axonhub.Name),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:            "axonhub",
					Image:           axonhub.Spec.Image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{{
						Name:          "http",
						ContainerPort: axonhubContainerPort,
						Protocol:      corev1.ProtocolTCP,
					}},
				}},
			},
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deployment, nil
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
	if deployment == nil {
		return nil
	}

	status := axonhub.DeepCopy().Status
	status.ObservedGeneration = axonhub.Generation
	status.ReadyReplicas = deployment.Status.ReadyReplicas

	ready := deployment.Status.ReadyReplicas >= r.desiredReplicas(axonhub)
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: axonhub.Generation,
		Reason:             "DeploymentNotReady",
		Message:            "AxonHub deployment is not yet ready",
	}
	if ready {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "DeploymentReady"
		condition.Message = "AxonHub deployment is ready"
	}
	meta.SetStatusCondition(&status.Conditions, condition)

	if !statusEqual(axonhub.Status, status) {
		axonhub.Status = status
		if err := r.Status().Update(ctx, axonhub); err != nil {
			return err
		}
	}
	return nil
}

// statusEqual reports whether two statuses are equivalent for update purposes.
func statusEqual(a, b axonhubv1alpha1.AxonHubStatus) bool {
	if a.ObservedGeneration != b.ObservedGeneration || a.ReadyReplicas != b.ReadyReplicas {
		return false
	}
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if a.Conditions[i] != b.Conditions[i] {
			return false
		}
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *AxonHubReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&axonhubv1alpha1.AxonHub{}).
		Owns(&appsv1.Deployment{}).
		Named("axonhub").
		Complete(r)
}
