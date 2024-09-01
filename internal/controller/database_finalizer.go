package controller

import (
	"context"
	"errors"
	"fmt"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// finalizeDatabase will perform the required operations before delete the CR.
func (r *DatabaseReconciler) ReconcileFinalizer(ctx context.Context, database *libsqlv1.Database) (requeue bool, err error) {
	log := log.FromContext(ctx)
	// Let's add a finalizer. Then, we can define some operations which should
	// occur before the custom resource is deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(database, databaseFinalizer) {
		log.Info("Adding Finalizer for Database")
		if ok := controllerutil.AddFinalizer(database, databaseFinalizer); !ok {
			log.Error(errors.New("failed to add finalizer"), "Failed to add finalizer into the custom resource")
			return true, nil
		}
		if err := r.Update(ctx, database); err != nil {
			if apierrors.IsConflict(err) {
				return true, nil
			}
			log.Error(err, fmt.Sprintf("Failed to update custom resource to add finalizer %v", database.Finalizers))
			return false, err
		}
	}

	// Check if the Database instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isDatabaseMarkedToBeDeleted := database.GetDeletionTimestamp() != nil && !database.GetDeletionTimestamp().IsZero()
	if isDatabaseMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(database, databaseFinalizer) {
			log.Info("Performing Finalizer Operations for Database before delete CR")
			// Let's add here a status "Downgrade" to reflect that this resource began its process to be terminated.
			changed := meta.SetStatusCondition(&database.Status.Conditions, metav1.Condition{Type: typeDegradedDatabase,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", database.Name)})
			if changed {
				if err := r.Status().Update(ctx, database); err != nil {
					if apierrors.IsConflict(err) {
						return true, nil
					}
					log.Error(err, "Failed to update Database status")
					return false, err
				}
			}
			// Perform all operations required before removing the finalizer and allow
			// the Kubernetes API to remove the custom resource.

			r.DoFinalizerOperationsForDatabase(ctx, database)

			// If you add operations to the doFinalizerOperationsForDatabase method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.
			changed = meta.SetStatusCondition(&database.Status.Conditions, metav1.Condition{Type: typeDegradedDatabase,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", database.Name)})
			if changed {
				if err := r.Status().Update(ctx, database); err != nil {
					if apierrors.IsConflict(err) {
						return true, nil
					}
					log.Error(err, "Failed to update Database status")
					return false, err
				}
			}

			log.Info("Removing Finalizer for Database after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(database, databaseFinalizer); !ok {
				log.Error(errors.New("failed to remove finalizer"), "Failed to remove finalizer for Database")
				return true, nil
			}

			if err := r.Update(ctx, database); err != nil {
				if apierrors.IsConflict(err) {
					return true, nil
				}
				log.Error(err, "Failed to remove finalizer for Database")
				return false, err
			}
		}
		return false, nil
	}

	return false, nil
}

// finalizeDatabase will perform the required operations before delete the CR.
func (r *DatabaseReconciler) DoFinalizerOperationsForDatabase(ctx context.Context, database *libsqlv1.Database) {
	// Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of deleting resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as dependent of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	// The following implementation will raise an event
	log := log.FromContext(ctx)
	r.Recorder.Event(database, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			database.Name,
			database.Namespace))

	err := r.DeleteDatabasePVC(ctx, database)
	if err != nil {
		log.Error(err, "Failed to delete database PVC")
	}

}
