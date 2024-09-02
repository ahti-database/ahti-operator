package controller

import (
	"context"
	"fmt"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
	"github.com/ahti-database/operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (r *DatabaseReconciler) ReconcileService(ctx context.Context, database *libsqlv1.Database) (reconciledHeadlessService *corev1.Service, reconciledService *corev1.Service, reconcileErr error) {
	headlessService, err := r.reconcileService(ctx, database, true)
	if err != nil {
		return nil, nil, err
	}
	service, err := r.reconcileService(ctx, database, false)
	if err != nil {
		return headlessService, nil, err
	}
	return headlessService, service, nil
}

func (r *DatabaseReconciler) reconcileService(ctx context.Context, database *libsqlv1.Database, headless bool) (*corev1.Service, error) {
	found := &corev1.Service{}
	service := r.ConstructService(ctx, database, headless)
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Name:      utils.GetDatabaseServiceName(database, headless),
			Namespace: database.Namespace,
		},
		found,
	); err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, service); err != nil {
				return nil, err
			}
			r.Recorder.Event(database, utils.EventNormal, "SuccessfulCreate",
				fmt.Sprintf("create Service %s is being created in the Namespace %s success",
					utils.GetDatabaseServiceName(database, headless),
					database.Namespace))
		} else {
			return nil, err
		}
	}
	// patch the service
	if err := r.Update(ctx, service); err != nil {
		return nil, err
	}
	return service, nil
}

func (r *DatabaseReconciler) ConstructService(ctx context.Context, database *libsqlv1.Database, headless bool) *corev1.Service {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GetDatabaseServiceName(database, headless),
			Namespace: database.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: database.APIVersion,
					Kind:       database.Kind,
					Name:       database.Name,
					UID:        database.UID,
				},
			},
			Labels: map[string]string{
				databaseLabel: database.Name,
				"node":        "primary",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       int32(8080),
					TargetPort: intstr.FromInt32(int32(8080)),
					Protocol:   corev1.ProtocolTCP,
					Name:       "primary-http",
				},
				{
					Port:       int32(5001),
					TargetPort: intstr.FromInt32(int32(5001)),
					Protocol:   corev1.ProtocolTCP,
					Name:       "primary-grpc",
				},
			},
			Selector: map[string]string{
				databaseLabel: database.Name,
				"node":        "primary",
			},
		},
	}
	if headless {
		service.Spec.ClusterIP = "None"
	}
	return service
}
