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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	axonhubv1alpha1 "github.com/amghazanfari/axonhub-operator/api/v1alpha1"
)

var _ = Describe("AxonHub Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const testNamespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}
		axonhub := &axonhubv1alpha1.AxonHub{}

		// newReconciler returns a reconciler wired to the test client.
		newReconciler := func() *AxonHubReconciler {
			return &AxonHubReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
		}

		// reconcile runs a single reconcile for the resource under test.
		doReconcile := func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		}

		// getDeployment fetches the Deployment owned by the resource.
		getDeployment := func() *appsv1.Deployment {
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			return deployment
		}

		// getAxonHub fetches the latest state of the AxonHub resource.
		getAxonHub := func() *axonhubv1alpha1.AxonHub {
			resource := &axonhubv1alpha1.AxonHub{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			return resource
		}

		// setDeploymentReadyReplicas simulates the deployment controller
		// reporting the given number of ready replicas.
		setDeploymentReadyReplicas := func(ready int32) {
			deployment := getDeployment()
			deployment.Status.ReadyReplicas = ready
			deployment.Status.Replicas = ready
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind AxonHub")
			err := k8sClient.Get(ctx, typeNamespacedName, axonhub)
			if err != nil && errors.IsNotFound(err) {
				resource := &axonhubv1alpha1.AxonHub{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespace,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &axonhubv1alpha1.AxonHub{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance AxonHub")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			// envtest runs no garbage collector, so the owned Deployment
			// must be removed explicitly to keep tests hermetic.
			By("Cleanup the owned Deployment left behind by the resource")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
			}
			err = k8sClient.Delete(ctx, deployment)
			if err != nil && errors.IsNotFound(err) {
				return
			}
			Expect(err).NotTo(HaveOccurred())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &AxonHubReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Creating an owned Deployment matching the spec")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			Expect(deployment.Spec.Replicas).To(HaveValue(BeNumerically("==", 1)))
			Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("axonhub"))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("looplj/axonhub:latest"))
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].Kind).To(Equal("AxonHub"))
			Expect(deployment.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "axonhub"))
			Expect(deployment.Labels).To(HaveKeyWithValue("app.kubernetes.io/instance", resourceName))

			By("Reporting status with an observed generation and Ready condition")
			Expect(k8sClient.Get(ctx, typeNamespacedName, axonhub)).To(Succeed())
			Expect(axonhub.Status.ObservedGeneration).To(Equal(axonhub.Generation))
			ready := meta.FindStatusCondition(axonhub.Status.Conditions, "Ready")
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(ReasonDeploymentNotReady))

			By("Reconciling again to update the spec")
			axonhub.Spec.Replicas = ptr.To(int32(3))
			Expect(k8sClient.Update(ctx, axonhub)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			Expect(deployment.Spec.Replicas).To(HaveValue(BeNumerically("==", 3)))

			By("Being a no-op when the spec has not changed")
			before := deployment.ResourceVersion
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			Expect(deployment.ResourceVersion).To(Equal(before))
		})

		It("should report Ready=True when the deployment has enough ready replicas", func() {
			By("Creating a resource with 2 replicas and reconciling")
			current := getAxonHub()
			current.Spec.Replicas = ptr.To(int32(2))
			Expect(k8sClient.Update(ctx, current)).To(Succeed())
			doReconcile()

			By("Simulating the deployment controller reporting 2 ready replicas")
			setDeploymentReadyReplicas(2)
			doReconcile()

			By("Reporting Ready=True on the AxonHub status")
			updated := getAxonHub()
			Expect(updated.Status.ReadyReplicas).To(Equal(int32(2)))
			Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
			ready := meta.FindStatusCondition(updated.Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(ready.Reason).To(Equal(ReasonDeploymentReady))
			Expect(ready.ObservedGeneration).To(Equal(updated.Generation))

			By("Dropping back below the desired count reports Ready=False")
			setDeploymentReadyReplicas(1)
			doReconcile()
			updated = getAxonHub()
			Expect(updated.Status.ReadyReplicas).To(Equal(int32(1)))
			ready = meta.FindStatusCondition(updated.Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonDeploymentNotReady))
		})

		It("should scale the deployment down when replicas are lowered", func() {
			By("Starting at 3 replicas")
			current := getAxonHub()
			current.Spec.Replicas = ptr.To(int32(3))
			Expect(k8sClient.Update(ctx, current)).To(Succeed())
			doReconcile()
			Expect(getDeployment().Spec.Replicas).To(HaveValue(BeNumerically("==", 3)))

			By("Scaling down to 1 replica and reconciling")
			current = getAxonHub()
			current.Spec.Replicas = ptr.To(int32(1))
			Expect(k8sClient.Update(ctx, current)).To(Succeed())
			doReconcile()

			deployment := getDeployment()
			Expect(deployment.Spec.Replicas).To(HaveValue(BeNumerically("==", 1)))

			By("Keeping the rest of the pod template intact")
			Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("axonhub"))
			Expect(deployment.OwnerReferences).To(HaveLen(1))
		})

		It("should update the pod template when the image changes", func() {
			By("Starting with the default image")
			doReconcile()
			Expect(getDeployment().Spec.Template.Spec.Containers[0].Image).To(Equal("looplj/axonhub:latest"))

			By("Changing the image and reconciling")
			current := getAxonHub()
			current.Spec.Image = "looplj/axonhub:v1.2.3"
			Expect(k8sClient.Update(ctx, current)).To(Succeed())
			doReconcile()

			By("Rolling the deployment pod template to the new image")
			deployment := getDeployment()
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("looplj/axonhub:v1.2.3"))
			Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("axonhub"))
		})

		It("should not issue a redundant status update on a no-op reconcile", func() {
			By("Reaching a steady ready state")
			doReconcile()
			setDeploymentReadyReplicas(1)
			doReconcile()

			By("Reconciling again without any change")
			before := getAxonHub()
			beforeResourceVersion := before.ResourceVersion
			doReconcile()

			By("Leaving the status (and resource version) untouched")
			after := getAxonHub()
			Expect(after.ResourceVersion).To(Equal(beforeResourceVersion))
			Expect(after.Status).To(Equal(before.Status))
		})
	})
})
