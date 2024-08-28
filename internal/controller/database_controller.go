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
	"encoding/hex"
	"errors"
	"fmt"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
	"github.com/ahti-database/operator/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const databaseFinalizer = "libsql.ahti.io/finalizer"

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
	log.Info("Reconciling...")

	log.Info("Finding existing Ahti Database resource...")
	// Get the Database object
	database := &libsqlv1.Database{}
	if err := r.Get(ctx, req.NamespacedName, database); err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("database resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get database")
		return ctrl.Result{}, err
	}

	// Let's just set the status as Unknown when no status is available
	if len(database.Status.Conditions) == 0 {
		meta.SetStatusCondition(&database.Status.Conditions, metav1.Condition{Type: typeAvailableDatabase, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err := r.Status().Update(ctx, database); err != nil {
			log.Error(err, "Failed to update database status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the database Custom Resource after updating the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raising the error "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, database); err != nil {
			log.Error(err, "Failed to re-fetch database")
			return ctrl.Result{}, err
		}
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occur before the custom resource is deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(database, databaseFinalizer) {
		log.Info("Adding Finalizer for Database")
		if ok := controllerutil.AddFinalizer(database, databaseFinalizer); !ok {
			log.Error(errors.New("failed to add finalizer"), "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err := r.Update(ctx, database); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the Database instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isDatabaseMarkedToBeDeleted := database.GetDeletionTimestamp() != nil
	if isDatabaseMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(database, databaseFinalizer) {
			log.Info("Performing Finalizer Operations for Database before delete CR")

			// Let's add here a status "Downgrade" to reflect that this resource began its process to be terminated.
			meta.SetStatusCondition(&database.Status.Conditions, metav1.Condition{Type: typeDegradedDatabase,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", database.Name)})

			if err := r.Status().Update(ctx, database); err != nil {
				log.Error(err, "Failed to update Database status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before removing the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			r.doFinalizerOperationsForDatabase(ctx, database)

			// If you add operations to the doFinalizerOperationsForDatabase method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			// Re-fetch the Database Custom Resource before updating the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raising the error "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, database); err != nil {
				log.Error(err, "Failed to re-fetch Database")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&database.Status.Conditions, metav1.Condition{Type: typeDegradedDatabase,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", database.Name)})

			if err := r.Status().Update(ctx, database); err != nil {
				log.Error(err, "Failed to update Database status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for Database after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(database, databaseFinalizer); !ok {
				log.Error(errors.New("failed to remove finalizer"), "Failed to remove finalizer for Database")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, database); err != nil {
				log.Error(err, "Failed to remove finalizer for Database")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	log.Info(
		"Listing all database spec fields",
		"Database.Image", fmt.Sprintf("%v", database.Spec.Image),
		"Database.ImagePullPolicy", fmt.Sprintf("%v", database.Spec.ImagePullPolicy),
		"Database.Replicas", fmt.Sprintf("%v", database.Spec.Replicas),
		"Database.Auth", fmt.Sprintf("%v", database.Spec.Auth),
		"Database.Storage", fmt.Sprintf("%v", database.Spec.Storage),
		"Database.Ingress", fmt.Sprintf("%v", database.Spec.Ingress),
		"Database.Resource", fmt.Sprintf("%v", database.Spec.Resource),
	)
	// https://github.com/operator-framework/operator-sdk/blob/latest/testdata/go/v4/database-operator/internal/controller/database_controller.go
	// create secret if not yet created
	databaseAuthSecret, err := r.getOrCreateAuthSecret(ctx, types.NamespacedName{Namespace: req.Namespace, Name: fmt.Sprintf("%v-auth-key", database.Name)})
	if err != nil {
		log.Error(err, "Failed to get/create database auth secret")
		return ctrl.Result{}, err
	}

	log.Info(databaseAuthSecret.Name)

	// get secret jwt key if created already
	// upsert all statefulsets with the secret jwt reference from above
	// upsert all services
	if database.Spec.Ingress != nil {
		// upsert ingress
		log.Info(
			"Listing all database spec ingress fields",
			"Database.Ingress.IngressClassName", fmt.Sprintf("%v", database.Spec.Ingress.IngressClassName),
			"Database.Ingress.Host", fmt.Sprintf("%v", database.Spec.Ingress.Host),
			"Database.Ingress.TLS", fmt.Sprintf("%v", database.Spec.Ingress.TLS),
		)
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&database.Status.Conditions, metav1.Condition{Type: typeAvailableDatabase,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", database.Name, database.Spec.Replicas)})

	if err := r.Status().Update(ctx, database); err != nil {
		log.Error(err, "Failed to update Database status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalizeDatabase will perform the required operations before delete the CR.
func (r *DatabaseReconciler) doFinalizerOperationsForDatabase(ctx context.Context, database *libsqlv1.Database) {
	// Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of deleting resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as dependent of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	log := log.FromContext(ctx)
	// The following implementation will raise an event
	r.Recorder.Event(database, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			database.Name,
			database.Namespace))

	databasePVCList := &corev1.PersistentVolumeClaimList{}
	pvcLabels := labels.NewSelector()
	appNameRequirement, err := labels.NewRequirement("app", selection.Equals, []string{database.Name})
	if err != nil {
		log.Error(err, "error trying to select app labels")
	}
	controlledByRequirement, err := labels.NewRequirement("ahti.database.io/managed-by", selection.Equals, []string{})
	if err != nil {
		log.Error(err, "error trying to select app labels")
	}
	pvcLabels.Add(
		*appNameRequirement,
		*controlledByRequirement,
	)
	if err := r.List(ctx, databasePVCList, &client.ListOptions{
		LabelSelector: pvcLabels,
	}); err != nil {
		log.Error(err, "pvc resources not found. Ignoring since object must be deleted")
		return
	}
	for _, databasePVC := range databasePVCList.Items {
		if err := r.Delete(ctx, &databasePVC); err != nil {
			log.Error(err, "pvc resources not found. Ignoring since object must be deleted")
		}
	}

	// TODO: delete secret
	authSecret := &corev1.Secret{}

}

func (r *DatabaseReconciler) getOrCreateAuthSecret(
	ctx context.Context,
	authSecretName types.NamespacedName,
) (*corev1.Secret, error) {
	log := log.FromContext(ctx)
	authSecret := &corev1.Secret{}
	if err := r.Get(ctx, authSecretName, authSecret); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating Auth Secret")
			publicKey, privateKey, err := utils.GenerateAsymmetricKeys()
			if err != nil {
				return nil, err
			}
			log.Info("logging keys", "PUBLIC KEY", hex.EncodeToString(publicKey), "PRIVATE KEY", hex.EncodeToString(privateKey))
			authSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      authSecretName.Name,
					Namespace: authSecretName.Namespace,
				},
				StringData: map[string]string{"SQLD_AUTH_JWT_KEY": string(publicKey), "PRIVATE_KEY": string(privateKey)},
			}
			if err := r.Create(ctx, authSecret); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return authSecret, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&libsqlv1.Database{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
