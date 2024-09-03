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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
)

var _ = Describe("Database Controller", func() {
	Context("When reconciling a resource", func() {
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
			Eventually(func() error {
				found := &libsqlv1.Database{}
				return k8sClient.Get(ctx, typeNamespacedName, found)
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

			By("Checking if Deployment was successfully created in the reconciliation")
			Eventually(func() error {
				found := &appsv1.StatefulSet{}
				return k8sClient.Get(ctx, typeNamespacedName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking the latest Status Condition added to the Memcached instance")
			Eventually(func() error {
				if len(database.Status.Conditions) != 0 {
					latestStatusCondition := database.Status.Conditions[len(database.Status.Conditions)-1]
					expectedLatestStatusCondition := metav1.Condition{
						Type:   typeAvailableDatabase,
						Status: metav1.ConditionTrue,
						Reason: "Reconciling",
						Message: fmt.Sprintf(
							"Deployment for custom resource (%s) created successfully", database.Name),
					}
					if latestStatusCondition != expectedLatestStatusCondition {
						return fmt.Errorf("The latest status condition added to the Database instance is not as expected")
					}
				}
				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})
