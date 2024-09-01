package controller

import (
	"context"
	"fmt"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *DatabaseReconciler) DeleteDatabasePVC(ctx context.Context, database *libsqlv1.Database) error {
	log := log.FromContext(ctx)
	databasePVCList := &corev1.PersistentVolumeClaimList{}
	pvcLabels := labels.NewSelector()
	controlledByRequirement, err := labels.NewRequirement(databaseLabel, selection.Equals, []string{database.Name})
	if err != nil {
		log.Error(err, "error trying to select app labels")
		return err
	}
	pvcLabels.Add(
		*controlledByRequirement,
	)
	if err := r.List(ctx, databasePVCList, &client.ListOptions{
		LabelSelector: pvcLabels,
	}); err != nil {
		log.Error(err, "pvc resources not found. Ignoring since object must be deleted")
		return err
	}
	log.Info(fmt.Sprintf("%v", len(databasePVCList.Items)))
	for _, databasePVC := range databasePVCList.Items {
		log.Info("TEST")
		if err := r.Delete(ctx, &databasePVC); err != nil {
			log.Error(err, "pvc resources not found. Ignoring since object must be deleted")
		}
	}

	return nil
}
