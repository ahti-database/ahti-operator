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

	libsqlv1 "github.com/ahti-database/operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	databaseAPIVersion string = "libsql.ahti.io/v1"
	databaseKind       string = "Database"
	databaseFinalizer  string = "libsql.ahti.io/finalizer"
	databaseLabel      string = "ahti.database.io/managed-by"
	databaseAppName    string = "ahti-database"
)

// Definitions to manage status conditions
const (
	// typeAvailableDatabase represents the status of the Deployment reconciliation
	typeAvailableDatabase = "Available"
	// typeDegradedDatabase represents the status used when the custom resource is deleted and the finalizer operations are yet to occur.
	typeDegradedDatabase = "Degraded"
)

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=libsql.ahti.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=libsql.ahti.io,resources=databases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=libsql.ahti.io,resources=databases/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// Modify the Reconcile function to compare the state specified by
// the Database object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get the Database object
	database := &libsqlv1.Database{}
	if err := r.Get(ctx, req.NamespacedName, database); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Let's just set the status as Unknown when no status is available
	if len(database.Status.Conditions) == 0 || database.Status.Conditions == nil {
		changed := meta.SetStatusCondition(&database.Status.Conditions, metav1.Condition{Type: typeAvailableDatabase, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if changed {
			if err := r.Status().Update(ctx, database); err != nil {
				// requeue for case of stale data without raising errors
				// https://github.com/kubernetes-sigs/controller-runtime/issues/1464
				if apierrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "Failed to update Database status")
				return ctrl.Result{}, err
			}
		}
	}

	requeue, err := r.ReconcileDatabaseFinalizer(ctx, database)
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeue {
		return ctrl.Result{Requeue: true}, nil
	}

	_, err = r.ReconcileDatabaseSecrets(ctx, database)
	if err != nil {
		log.Error(err, "Failed to reconcile database auth secret")
		return ctrl.Result{}, err
	}
	_, err = r.ReconcileDatabaseStatefulSets(ctx, database)
	if err != nil {
		log.Error(err, "Failed to reconcile statefulset")
		return ctrl.Result{}, err
	}
	_, _, err = r.ReconcileDatabaseService(ctx, database)
	if err != nil {
		log.Error(err, "Failed to reconcile service")
		return ctrl.Result{}, err
	}
	_, err = r.ReconcileDatabaseIngress(ctx, database)
	if err != nil {
		log.Error(err, "Failed to reconcile ingress")
		return ctrl.Result{}, err
	}

	// The following implementation will update the status
	changed := meta.SetStatusCondition(&database.Status.Conditions, metav1.Condition{Type: typeAvailableDatabase,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) created successfully", database.Name)})
	if changed {
		if err := r.Status().Update(ctx, database); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "Failed to update Database status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&libsqlv1.Database{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Secret{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.MapAuthSecretsToReconcile),
		).
		Watches(
			&appsv1.StatefulSet{},
			handler.EnqueueRequestsFromMapFunc(r.MapDatabaseStatefulSetsToReconcile),
		).
		Watches(
			&networkingv1.Ingress{},
			handler.EnqueueRequestsFromMapFunc(r.MapDatabaseIngressToReconcile),
		).
		Complete(r)
}
