package controller

import (
	"context"
	"fmt"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
	"github.com/ahti-database/operator/internal/utils"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *DatabaseReconciler) ReconcileDatabaseIngress(ctx context.Context, database *libsqlv1.Database) (*networkingv1.Ingress, error) {
	found := &networkingv1.Ingress{}
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Name:      utils.GetDatabaseIngressName(database),
			Namespace: database.Namespace,
		},
		found,
	); err != nil {
		if apierrors.IsNotFound(err) && database.Spec.Ingress != nil {
			ingress := r.ConstructDatabaseIngress(ctx, database)
			if err := r.Create(ctx, ingress); err != nil {
				return nil, err
			}
			r.Recorder.Event(database, utils.EventNormal, "SuccessfulCreate",
				fmt.Sprintf("create Ingress %s is being created in the Namespace %s success",
					utils.GetDatabaseIngressName(database),
					database.Namespace))
		} else if apierrors.IsNotFound(err) && database.Spec.Ingress == nil {
			return nil, nil
		} else {
			return nil, err
		}
	}
	if database.Spec.Ingress == nil {
		// delete ingress if database does not need it
		if err := r.Delete(ctx, found); err != nil {
			return nil, err
		}
		return nil, nil
	} else {
		// patch the statefulset
		ingress := r.ConstructDatabaseIngress(ctx, database)
		if err := r.Update(ctx, ingress); err != nil {
			if apierrors.IsNotFound(err) {
				return ingress, nil
			}
			return nil, err
		}
		return ingress, nil
	}
}

func (r *DatabaseReconciler) ConstructDatabaseIngress(ctx context.Context, database *libsqlv1.Database) *networkingv1.Ingress {
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GetDatabaseIngressName(database),
			Namespace: database.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: databaseAPIVersion,
					Kind:       databaseKind,
					Name:       database.Name,
					UID:        database.UID,
				},
			},
			Labels: map[string]string{
				databaseLabel: database.Name,
				"node":        "primary",
			}},
		Spec: networkingv1.IngressSpec{
			IngressClassName: database.Spec.Ingress.IngressClassName,
			TLS:              database.Spec.Ingress.TLS,
			Rules: []networkingv1.IngressRule{
				{
					Host: database.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: ptr.To(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: utils.GetDatabaseServiceName(database, false),
											Port: networkingv1.ServiceBackendPort{
												Number: int32(8080),
											},
										},
									},
								},
							}},
					},
				},
			},
		},
	}
	return ingress
}

func (r *DatabaseReconciler) MapDatabaseIngressToReconcile(ctx context.Context, object client.Object) []reconcile.Request {
	ingress := object.(*networkingv1.Ingress)
	gvk, err := apiutil.GVKForObject(&libsqlv1.Database{}, r.Scheme)
	if err != nil {
		return nil
	}
	if len(ingress.ObjectMeta.OwnerReferences) > 0 {
		for _, ownerReference := range ingress.ObjectMeta.OwnerReferences {
			if ownerReference.APIVersion == gvk.GroupVersion().String() {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{Namespace: ingress.Namespace, Name: ownerReference.Name},
					},
				}
			}
		}
	}
	return nil
}
