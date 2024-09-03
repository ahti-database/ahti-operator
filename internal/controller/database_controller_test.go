/*
Copyright 2024.

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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
	"github.com/ahti-database/operator/internal/utils"
)

var _ = Describe("Database Controller", func() {
	Context("When creating a database", func() {
		const databaseName = "test-sample-database"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      databaseName,
			Namespace: "default",
		}
		database := &libsqlv1.Database{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Database")
			err := k8sClient.Get(ctx, typeNamespacedName, database)
			if err != nil && errors.IsNotFound(err) {
				database = &libsqlv1.Database{
					ObjectMeta: metav1.ObjectMeta{
						Name:      databaseName,
						Namespace: "default",
					},
					TypeMeta: metav1.TypeMeta{
						APIVersion: databaseAPIVersion,
						Kind:       databaseKind,
					},
					Spec: libsqlv1.DatabaseSpec{
						Image:           "ghcr.io/tursodatabase/libsql-server:v0.24.21",
						ImagePullPolicy: "Always",
						Auth:            true,
						Storage:         libsqlv1.DatabaseStorage{Size: *resource.NewMilliQuantity(int64(1000), resource.BinarySI)},
						Ingress: &libsqlv1.AhtiDatabaseIngressSpec{
							IngressClassName: ptr.To("nginx"),
							Host:             "database.ahti.io",
						},
					},
				}
				Expect(k8sClient.Create(ctx, database)).To(Succeed())
			}
		})

		AfterEach(func() {
			database := &libsqlv1.Database{}
			err := k8sClient.Get(ctx, typeNamespacedName, database)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Database")
			Expect(k8sClient.Delete(ctx, database)).To(Succeed())
		})

		It("should successfully reconcile the Database resource", func() {
			By("Checking if the custom resource was successfully created")
			database = &libsqlv1.Database{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, database)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &DatabaseReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: MockEventRecorder{},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, database)
			}, time.Minute, time.Second).Should(Succeed())
			Expect(controllerutil.ContainsFinalizer(database, databaseFinalizer)).Should(BeTrue())

			By("Checking if StatefulSet was successfully created in the reconciliation")
			databaseStatefulSet := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, databaseStatefulSet)
			}, time.Minute, time.Second).Should(Succeed())
			Expect(databaseStatefulSet.Spec.Template.Spec.Containers[0].Image).Should(Equal(database.Spec.Image))
			Expect(databaseStatefulSet.ObjectMeta.OwnerReferences[0].Name).Should(Equal(database.Name))

			By("Checking the latest Status Condition added to the Database instance")
			Eventually(func() error {
				if len(database.Status.Conditions) != 0 {
					latestStatusCondition := database.Status.Conditions[len(database.Status.Conditions)-1]
					expectedLatestStatusCondition := metav1.Condition{
						Type:               typeAvailableDatabase,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: latestStatusCondition.LastTransitionTime,
						Reason:             "Reconciling",
						Message: fmt.Sprintf(
							"Deployment for custom resource (%s) created successfully", database.Name),
					}
					if latestStatusCondition != expectedLatestStatusCondition {
						return fmt.Errorf("The latest status condition added to the Database instance is not as expected\n%v\n%v", latestStatusCondition, expectedLatestStatusCondition)
					}
				}
				return nil
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking if Auth Secret was successfully created in the reconciliation")
			secret := &corev1.Secret{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: utils.GetAuthSecretName(database), Namespace: database.Namespace}, secret)
			}, time.Minute, time.Second).Should(Succeed())
			Expect(secret.ObjectMeta.OwnerReferences[0].Name).Should(Equal(database.Name))

			By("Checking if Headless Service was successfully created in the reconciliation")
			headlessService := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: utils.GetDatabaseServiceName(database, true), Namespace: database.Namespace}, headlessService)
			}, time.Minute, time.Second).Should(Succeed())
			Expect(headlessService.ObjectMeta.OwnerReferences[0].Name).Should(Equal(database.Name))

			By("Checking if ClusterIP Service was successfully created in the reconciliation")
			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: utils.GetDatabaseServiceName(database, false), Namespace: database.Namespace}, service)
			}, time.Minute, time.Second).Should(Succeed())
			Expect(service.ObjectMeta.OwnerReferences[0].Name).Should(Equal(database.Name))

			By("Checking if Ingress was successfully created in the reconciliation")
			ingress := &networkingv1.Ingress{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: utils.GetDatabaseIngressName(database), Namespace: database.Namespace}, ingress)
			}, time.Minute, time.Second).Should(Succeed())
			Expect(ingress.ObjectMeta.OwnerReferences[0].Name).Should(Equal(database.Name))

			By("Checking if secret is removed after updating database auth to false")
			database.Spec.Auth = false
			Eventually(func() error {
				return k8sClient.Update(ctx, database)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the created resource")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if Auth Secret was successfully deleted in the reconciliation")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: utils.GetAuthSecretName(database), Namespace: database.Namespace}, secret)
			}, time.Minute, time.Second).ShouldNot(Succeed())

			By("Checking if ingress is removed after updating database ingress to nil")
			database.Spec.Ingress = nil
			Eventually(func() error {
				return k8sClient.Update(ctx, database)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the created resource")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if Ingress was successfully deleted in the reconciliation")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: utils.GetDatabaseIngressName(database), Namespace: database.Namespace}, ingress)
			}, time.Minute, time.Second).ShouldNot(Succeed())
		})

	})
})
