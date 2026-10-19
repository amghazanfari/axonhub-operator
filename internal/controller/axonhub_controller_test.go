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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	axonhubv1alpha1 "github.com/amghazanfari/axonhub-operator/api/v1alpha1"
)

// deploymentAvailableReason is the reason the deployment controller uses
// for a True Available condition once minimum availability is reached.
const deploymentAvailableReason = "MinimumReplicasAvailable"

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

		// getAxonHubAt fetches the latest state of the named AxonHub resource.
		getAxonHubAt := func(name types.NamespacedName) *axonhubv1alpha1.AxonHub {
			resource := &axonhubv1alpha1.AxonHub{}
			Expect(k8sClient.Get(ctx, name, resource)).To(Succeed())
			return resource
		}

		// getAxonHub fetches the latest state of the resource under test.
		getAxonHub := func() *axonhubv1alpha1.AxonHub {
			return getAxonHubAt(typeNamespacedName)
		}

		// setDeploymentReadyReplicas simulates the deployment controller
		// having completed the rollout: the generation is observed, the
		// given number of replicas is ready and updated to the current
		// template, and the Available condition is True.
		setDeploymentReadyReplicas := func(ready int32) {
			deployment := getDeployment()
			deployment.Status.ObservedGeneration = deployment.Generation
			deployment.Status.ReadyReplicas = ready
			deployment.Status.UpdatedReplicas = ready
			deployment.Status.Replicas = ready
			deployment.Status.Conditions = []appsv1.DeploymentCondition{{
				Type:               appsv1.DeploymentAvailable,
				Status:             corev1.ConditionTrue,
				Reason:             deploymentAvailableReason,
				LastTransitionTime: metav1.Now(),
			}}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
		}

		// setDeploymentStatus mutates the Deployment's status subresource
		// with the given callback, simulating arbitrary deployment
		// controller observations.
		setDeploymentStatus := func(mutate func(*appsv1.DeploymentStatus)) {
			deployment := getDeployment()
			mutate(&deployment.Status)
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
			Expect(deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy).To(Equal(corev1.PullAlways))
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].Kind).To(Equal("AxonHub"))
			Expect(deployment.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "axonhub"))
			Expect(deployment.Labels).To(HaveKeyWithValue("app.kubernetes.io/instance", resourceName))

			By("Creating an owned Service exposing the AxonHub HTTP port")
			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, service)).To(Succeed())
			Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(service.Spec.Selector).To(HaveKeyWithValue("app.kubernetes.io/name", "axonhub"))
			Expect(service.Spec.Selector).To(HaveKeyWithValue("app.kubernetes.io/instance", resourceName))
			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Name).To(Equal("http"))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(8090)))
			Expect(service.Spec.Ports[0].TargetPort.String()).To(Equal("http"))
			Expect(service.Spec.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].Kind).To(Equal("AxonHub"))
			Expect(service.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "axonhub-operator"))

			By("Setting a security context compatible with the restricted PSS")
			podSC := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSC).NotTo(BeNil())
			Expect(podSC.RunAsNonRoot).To(HaveValue(BeTrue()))
			Expect(podSC.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
			containerSC := deployment.Spec.Template.Spec.Containers[0].SecurityContext
			Expect(containerSC).NotTo(BeNil())
			Expect(containerSC.AllowPrivilegeEscalation).To(HaveValue(BeFalse()))
			Expect(containerSC.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKey(corev1.ResourceCPU))

			By("Reporting status with an observed generation and Ready condition")
			Expect(k8sClient.Get(ctx, typeNamespacedName, axonhub)).To(Succeed())
			Expect(axonhub.Status.ObservedGeneration).To(Equal(axonhub.Generation))
			ready := meta.FindStatusCondition(axonhub.Status.Conditions, "Ready")
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(ReasonDeploymentStatusNotObservedYet))

			By("Propagating spec changes to the deployment")
			axonhub.Spec.Replicas = ptr.To(int32(3))
			axonhub.Spec.Image = "looplj/axonhub:v1.0.0"
			Expect(k8sClient.Update(ctx, axonhub)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			Expect(deployment.Spec.Replicas).To(HaveValue(BeNumerically("==", 3)))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("looplj/axonhub:v1.0.0"))
			Expect(deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy).To(Equal(corev1.PullIfNotPresent))

			By("Being a no-op when the spec has not changed")
			before := deployment.ResourceVersion
			serviceBefore := service.ResourceVersion
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			Expect(deployment.ResourceVersion).To(Equal(before))
			Expect(k8sClient.Get(ctx, typeNamespacedName, service)).To(Succeed())
			Expect(service.ResourceVersion).To(Equal(serviceBefore))
		})
		It("should fail loudly when a pre-existing Service has a conflicting selector", func() {
			By("Creating a Service with an unrelated immutable selector")
			conflictName := types.NamespacedName{Name: "conflict-resource", Namespace: testNamespace}
			conflictingService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      conflictName.Name,
					Namespace: conflictName.Namespace,
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"workload": "something-else"},
					Ports: []corev1.ServicePort{{
						Port: 80,
					}},
				},
			}
			Expect(k8sClient.Create(ctx, conflictingService)).To(Succeed())

			By("Creating an AxonHub resource with the same name")
			resource := &axonhubv1alpha1.AxonHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      conflictName.Name,
					Namespace: conflictName.Namespace,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			defer func() {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
				Expect(k8sClient.Delete(ctx, conflictingService)).To(Succeed())
			}()

			By("Reconciling and expecting a descriptive error, not silent misrouting")
			controllerReconciler := &AxonHubReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: conflictName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("conflicting immutable selector"))
		})

		// reconcileWithRecorder runs a single reconcile with a fake event
		// recorder attached, so emitted Events can be asserted.
		reconcileWithRecorder := func(recorder *record.FakeRecorder, name types.NamespacedName) (ctrl.Result, error) {
			reconciler := &AxonHubReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Record: recorder,
			}
			return reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		}

		// foreignOwnerRef returns a controller owner reference pointing at
		// an unrelated owner, simulating an object managed by someone else.
		foreignOwnerRef := func(name string) metav1.OwnerReference {
			return metav1.OwnerReference{
				APIVersion: "axonhub.onrender.com/v1alpha1",
				Kind:       "AxonHub",
				Name:       name,
				UID:        "11111111-2222-3333-4444-555555555555",
				Controller: ptr.To(true),
			}
		}

		It("should surface an AlreadyOwned Deployment conflict as a condition and event without erroring", func() {
			const conflictName = "owned-deployment-resource"
			namespacedName := types.NamespacedName{Name: conflictName, Namespace: testNamespace}

			By("Creating a Deployment already controlled by another owner")
			conflictingDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      conflictName,
					Namespace: testNamespace,
					OwnerReferences: []metav1.OwnerReference{
						foreignOwnerRef("some-other-owner"),
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "foreign"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "foreign"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  axonhubContainerName,
								Image: "nginx:1.27",
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, conflictingDeployment)).To(Succeed())

			By("Creating an AxonHub resource with the same name")
			resource := &axonhubv1alpha1.AxonHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      conflictName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			defer func() {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
				Expect(k8sClient.Delete(ctx, conflictingDeployment)).To(Succeed())
			}()

			By("Reconciling and expecting a backed-off requeue instead of an error")
			recorder := record.NewFakeRecorder(10)
			result, err := reconcileWithRecorder(recorder, namespacedName)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(alreadyOwnedRequeueInterval))

			By("Surfacing the conflict as an actionable Ready condition")
			Expect(k8sClient.Get(ctx, namespacedName, resource)).To(Succeed())
			ready := meta.FindStatusCondition(resource.Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonChildObjectAlreadyOwned))
			Expect(ready.Message).To(ContainSubstring("Deployment"))
			Expect(ready.Message).To(ContainSubstring("some-other-owner"))
			Expect(ready.Message).To(ContainSubstring("delete the conflicting object or rename the AxonHub resource"))

			By("Emitting a warning Event on the AxonHub resource")
			Eventually(recorder.Events).Should(Receive(ContainSubstring(ReasonChildObjectAlreadyOwned)))

			By("Not re-emitting the Event while the conflict persists")
			_, err = reconcileWithRecorder(recorder, namespacedName)
			Expect(err).NotTo(HaveOccurred())
			Expect(recorder.Events).ToNot(Receive())

			By("Not re-emitting the Event on an incidental status change (spec edit)")
			resource.Spec.Replicas = ptr.To(int32(3))
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())
			_, err = reconcileWithRecorder(recorder, namespacedName)
			Expect(err).NotTo(HaveOccurred())
			Expect(recorder.Events).ToNot(Receive())
			ready = meta.FindStatusCondition(getAxonHubAt(namespacedName).Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(ReasonChildObjectAlreadyOwned))
			Expect(ready.ObservedGeneration).To(Equal(getAxonHubAt(namespacedName).Generation))

			By("Coping with a nil event recorder (no panic, condition still reported)")
			nilRecorder := &AxonHubReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			result, err = nilRecorder.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(alreadyOwnedRequeueInterval))
			ready = meta.FindStatusCondition(getAxonHubAt(namespacedName).Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(ReasonChildObjectAlreadyOwned))

			By("Not having mutated the conflicting Deployment's owner references")
			Expect(k8sClient.Get(ctx, namespacedName, conflictingDeployment)).To(Succeed())
			Expect(conflictingDeployment.OwnerReferences).To(HaveLen(1))
			Expect(conflictingDeployment.OwnerReferences[0].Name).To(Equal("some-other-owner"))
		})

		It("should surface an AlreadyOwned Service conflict and recover once the object is removed", func() {
			const conflictName = "owned-service-resource"
			namespacedName := types.NamespacedName{Name: conflictName, Namespace: testNamespace}

			By("Creating a Service already controlled by another owner")
			conflictingService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      conflictName,
					Namespace: testNamespace,
					OwnerReferences: []metav1.OwnerReference{
						foreignOwnerRef("some-other-owner"),
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: labelsForAxonHub(conflictName),
					Ports: []corev1.ServicePort{{
						Port: 80,
					}},
				},
			}
			Expect(k8sClient.Create(ctx, conflictingService)).To(Succeed())

			By("Creating an AxonHub resource with the same name")
			resource := &axonhubv1alpha1.AxonHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      conflictName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			defer func() {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
				Expect(k8sClient.Delete(ctx, &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: conflictName, Namespace: testNamespace},
				})).To(SatisfyAny(Succeed(), MatchError(ContainSubstring("not found"))))
				service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: conflictName, Namespace: testNamespace}}
				Expect(k8sClient.Delete(ctx, service)).To(SatisfyAny(Succeed(), MatchError(ContainSubstring("not found"))))
			}()

			By("Reconciling: the Deployment is created, the Service conflicts")
			recorder := record.NewFakeRecorder(10)
			result, err := reconcileWithRecorder(recorder, namespacedName)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(alreadyOwnedRequeueInterval))

			ready := meta.FindStatusCondition(getAxonHubAt(namespacedName).Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(ReasonChildObjectAlreadyOwned))
			Expect(ready.Message).To(ContainSubstring("Service"))
			Eventually(recorder.Events).Should(Receive(ContainSubstring(ReasonChildObjectAlreadyOwned)))

			By("Deleting the conflicting Service and reconciling again recovers")
			Expect(k8sClient.Delete(ctx, conflictingService)).To(Succeed())
			result, err = reconcileWithRecorder(recorder, namespacedName)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(recorder.Events).ToNot(Receive())

			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, namespacedName, service)).To(Succeed())
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].Kind).To(Equal("AxonHub"))
			Expect(service.OwnerReferences[0].Name).To(Equal(conflictName))

			ready = meta.FindStatusCondition(getAxonHubAt(namespacedName).Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(ReasonDeploymentStatusNotObservedYet))
		})
		It("should configure the AxonHub database from spec.db", func() {
			By("Reconciling the created resource without a db configured")
			controllerReconciler := &AxonHubReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Leaving the database env unset so AxonHub defaults apply")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(BeEmpty())

			By("Configuring a postgres database")
			Expect(k8sClient.Get(ctx, typeNamespacedName, axonhub)).To(Succeed())
			axonhub.Spec.DB = &axonhubv1alpha1.DatabaseSpec{
				Type:    "postgres",
				Address: "postgres://user:pass@postgres:5432/axonhub?sslmode=disable",
			}
			Expect(k8sClient.Update(ctx, axonhub)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Injecting the AXONHUB_DB_* environment variables")
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			env := deployment.Spec.Template.Spec.Containers[0].Env
			Expect(env).To(HaveLen(2))
			Expect(env).To(ContainElement(corev1.EnvVar{Name: "AXONHUB_DB_DIALECT", Value: "postgres"}))
			Expect(env).To(ContainElement(corev1.EnvVar{Name: "AXONHUB_DB_DSN", Value: "postgres://user:pass@postgres:5432/axonhub?sslmode=disable"}))

			By("Switching to mysql updates the injected variables")
			Expect(k8sClient.Get(ctx, typeNamespacedName, axonhub)).To(Succeed())
			axonhub.Spec.DB = &axonhubv1alpha1.DatabaseSpec{
				Type:    "mysql",
				Address: "mysql://user:pass@mysql:3306/axonhub",
			}
			Expect(k8sClient.Update(ctx, axonhub)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			env = deployment.Spec.Template.Spec.Containers[0].Env
			Expect(env).To(HaveLen(2))
			Expect(env).To(ContainElement(corev1.EnvVar{Name: "AXONHUB_DB_DIALECT", Value: "mysql"}))
			Expect(env).To(ContainElement(corev1.EnvVar{Name: "AXONHUB_DB_DSN", Value: "mysql://user:pass@mysql:3306/axonhub"}))
		})

		It("should reject unsupported database types", func() {
			By("Creating an AxonHub resource with an invalid db type")
			resource := &axonhubv1alpha1.AxonHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-db-type",
					Namespace: testNamespace,
				},
				Spec: axonhubv1alpha1.AxonHubSpec{
					DB: &axonhubv1alpha1.DatabaseSpec{
						Type:    "oracle",
						Address: "oracle://localhost:1521/xe",
					},
				},
			}
			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
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
			Expect(ready.Reason).To(Equal(ReasonDeploymentReplicasNotReady))
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

		It("should not report ready while the rollout is still in progress", func() {
			By("Reconciling the resource with the default replica count")
			doReconcile()

			By("Not reporting ready when the Deployment status is unobserved")
			ready := meta.FindStatusCondition(getAxonHub().Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonDeploymentStatusNotObservedYet))

			By("Not reporting ready mid rolling update (old pods ready, not all updated)")
			// The real mid-rollout state: the generation is observed,
			// Available stays True to preserve minimum availability, and
			// enough pods are ready — but only some run the new template.
			setDeploymentStatus(func(status *appsv1.DeploymentStatus) {
				status.ObservedGeneration = getDeployment().Generation
				status.Replicas = 1
				status.ReadyReplicas = 1
				status.UpdatedReplicas = 0
				status.Conditions = []appsv1.DeploymentCondition{{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
					Reason: deploymentAvailableReason,
				}}
			})
			doReconcile()
			ready = meta.FindStatusCondition(getAxonHub().Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonDeploymentRolloutInProgress))

			By("Not reporting ready during a surge (ready replicas reached, Available condition absent)")
			setDeploymentStatus(func(status *appsv1.DeploymentStatus) {
				status.ObservedGeneration = getDeployment().Generation
				status.Replicas = 1
				status.ReadyReplicas = 1
				status.UpdatedReplicas = 1
				// No Available condition published yet.
				status.Conditions = nil
			})
			doReconcile()
			ready = meta.FindStatusCondition(getAxonHub().Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonDeploymentAvailableUnknown))

			By("Not reporting ready when the Available condition is Unknown")
			setDeploymentStatus(func(status *appsv1.DeploymentStatus) {
				status.Conditions = []appsv1.DeploymentCondition{{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionUnknown,
					Reason: "DeploymentConditionUnknown",
				}}
			})
			doReconcile()
			ready = meta.FindStatusCondition(getAxonHub().Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonDeploymentAvailableUnknown))

			By("Not reporting ready when the Available condition is False")
			setDeploymentStatus(func(status *appsv1.DeploymentStatus) {
				status.Conditions = []appsv1.DeploymentCondition{{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionFalse,
					Reason: "MinimumReplicasUnavailable",
				}}
			})
			doReconcile()
			ready = meta.FindStatusCondition(getAxonHub().Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonDeploymentUnavailable))

			By("Not reporting ready when the Deployment generation is stale")
			// Bump the Deployment's generation by mutating its spec so the
			// stale-generation branch is distinct from the unobserved one.
			deployment := getDeployment()
			deployment.Spec.Template.Labels["test.axonhub.io/generation-bump"] = "true"
			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())
			setDeploymentStatus(func(status *appsv1.DeploymentStatus) {
				status.ObservedGeneration = getDeployment().Generation - 1
				status.Replicas = 1
				status.ReadyReplicas = 1
				status.Conditions = []appsv1.DeploymentCondition{{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
					Reason: deploymentAvailableReason,
				}}
			})
			doReconcile()
			ready = meta.FindStatusCondition(getAxonHub().Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonDeploymentGenerationStale))

			By("Reporting ready only once the rollout has completed")
			setDeploymentReadyReplicas(1)
			doReconcile()
			ready = meta.FindStatusCondition(getAxonHub().Status.Conditions, AxonHubConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(ready.Reason).To(Equal(ReasonDeploymentReady))
		})

		It("should error when the deployment has no axonhub container", func() {
			By("Reconciling the created resource")
			controllerReconciler := &AxonHubReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Replacing the deployment with one that has a foreign container")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			deployment.Spec.Template.Spec.Containers = []corev1.Container{{
				Name:  "other",
				Image: "nginx:1.27",
			}}
			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())
			drifted := deployment.ResourceVersion

			By("Failing to reconcile the drifted deployment")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no \"axonhub\" container"))

			By("Surfacing the failure on the Ready condition instead of freezing status")
			Expect(k8sClient.Get(ctx, typeNamespacedName, axonhub)).To(Succeed())
			ready := meta.FindStatusCondition(axonhub.Status.Conditions, "Ready")
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonDeploymentReconcileFailed))
			Expect(ready.Message).To(ContainSubstring("no \"axonhub\" container"))

			By("Not having mutated the drifted deployment")
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("other"))
			Expect(deployment.ResourceVersion).To(Equal(drifted))
		})
	})
})
