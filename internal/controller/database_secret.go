package controller

import (
	"context"
	"encoding/base64"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
	"github.com/ahti-database/operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *DatabaseReconciler) ReconcileDatabaseSecrets(ctx context.Context, database *libsqlv1.Database) (*corev1.Secret, error) {
	log := log.FromContext(ctx)
	authSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      utils.GetAuthSecretName(database),
		Namespace: database.Namespace,
	}, authSecret); err != nil {
		if database.Spec.Auth && apierrors.IsNotFound(err) {
			log.Info("Creating Auth Secret")
			publicKey, privateKey, err := utils.GenerateAsymmetricKeys()
			if err != nil {
				return nil, err
			}
			authSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      utils.GetAuthSecretName(database),
					Namespace: database.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: databaseAPIVersion,
							Kind:       databaseKind,
							Name:       database.Name,
							UID:        database.UID,
						},
					},
				},
				StringData: map[string]string{
					"PUBLIC_KEY":  base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(publicKey),
					"PRIVATE_KEY": base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(privateKey),
				},
			}
			if err := r.Create(ctx, authSecret); err != nil {
				return nil, err
			}
		} else if !database.Spec.Auth && apierrors.IsNotFound(err) {
			return nil, nil
		} else {
			return nil, err
		}
	}
	if !database.Spec.Auth {
		// delete secret if database does not need auth
		if err := r.Delete(ctx, authSecret); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return authSecret, nil
}

func (r *DatabaseReconciler) MapAuthSecretsToReconcile(ctx context.Context, object client.Object) []reconcile.Request {
	authSecret := object.(*corev1.Secret)
	gvk, err := apiutil.GVKForObject(&libsqlv1.Database{}, r.Scheme)
	if err != nil {
		return nil
	}
	if len(authSecret.ObjectMeta.OwnerReferences) > 0 {
		for _, ownerReference := range authSecret.ObjectMeta.OwnerReferences {
			if ownerReference.APIVersion == gvk.GroupVersion().String() {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{Namespace: authSecret.Namespace, Name: ownerReference.Name},
					},
				}
			}
		}
	}
	return nil
}
