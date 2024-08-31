package controller

import (
	"context"
	"encoding/base64"
	"fmt"

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

func (r *DatabaseReconciler) GetOrCreateAuthSecret(
	ctx context.Context,
	authSecretName types.NamespacedName,
	database *libsqlv1.Database,
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
			authSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      authSecretName.Name,
					Namespace: authSecretName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: database.APIVersion,
							Kind:       database.Kind,
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
		} else {
			return nil, err
		}
	}
	return authSecret, nil
}

func (r *DatabaseReconciler) DeleteDatabaseAuthSecret(ctx context.Context, database *libsqlv1.Database) error {
	authSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: database.Namespace, Name: fmt.Sprintf("%v-auth-key", database.Name)}, authSecret); err != nil {
		return err
	}
	if err := r.Delete(ctx, authSecret); err != nil {
		return err
	}
	return nil
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
