package controller

import (
	"context"
	"fmt"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
	"github.com/ahti-database/operator/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *DatabaseReconciler) ReconcileStatefulSets(ctx context.Context, database *libsqlv1.Database) (*appsv1.StatefulSet, error) {
	found := &appsv1.StatefulSet{}
	primaryStatefulSet := r.ConstructPrimaryStatefulSet(ctx, database)
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Name:      database.Name,
			Namespace: database.Namespace,
		},
		found,
	); err != nil {
		if apierrors.IsNotFound(err) {

			if err := r.Create(ctx, primaryStatefulSet); err != nil {
				return nil, err
			}
			r.Recorder.Event(database, utils.EventNormal, "SuccessfulCreate",
				fmt.Sprintf("create StatefulSet %s is being created in the Namespace %s success",
					database.Name,
					database.Namespace))
		} else {
			return nil, err
		}
	}
	// patch the statefulset
	if err := r.Update(ctx, primaryStatefulSet); err != nil {
		return nil, err
	}
	return primaryStatefulSet, nil
}

func (r *DatabaseReconciler) ConstructPrimaryStatefulSet(ctx context.Context, database *libsqlv1.Database) *appsv1.StatefulSet {
	log := log.FromContext(ctx)
	primaryStatefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      database.Name,
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
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					databaseLabel: database.Name,
					"node":        "primary",
				},
			},
			ServiceName: utils.GetDatabaseServiceName(database, true),
			Replicas:    ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						databaseLabel: database.Name,
						"node":        "primary",
					},
				},
				Spec: corev1.PodSpec{
					NodeSelector:                 database.Spec.NodeSelector,
					ServiceAccountName:           database.Spec.ServiceAccountName,
					AutomountServiceAccountToken: database.Spec.AutomountServiceAccountToken,
					ImagePullSecrets:             database.Spec.ImagePullSecrets,
					Affinity:                     database.Spec.Affinity,
					SchedulerName:                database.Spec.SchedulerName,
					Tolerations:                  database.Spec.Tolerations,
					Containers: []corev1.Container{
						{
							Image:           database.Spec.Image,
							ImagePullPolicy: corev1.PullPolicy(database.Spec.ImagePullPolicy),
							Name:            "libsql-server",
							Resources:       database.Spec.Resource,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
									Name:          "primary-http",
								},
								{
									ContainerPort: 5001,
									Protocol:      corev1.ProtocolTCP,
									Name:          "primary-grpc",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "SQLD_NODE",
									Value: "primary",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.IntOrString{
											IntVal: 8080,
										},
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.IntOrString{
											IntVal: 8080,
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      utils.GetDatabasePVCName(database),
									MountPath: "/var/lib/sqld",
								},
							},
							// TODO: Add nodeselector, ServiceAccountName, etc.
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: utils.GetDatabasePVCName(database),
						Labels: map[string]string{
							databaseLabel: database.Name,
							"node":        "primary",
						},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: database.Spec.Storage.Size,
							},
						},
					},
				},
			},
		},
	}
	if database.Spec.Auth {
		primaryStatefulSet.Spec.Template.Spec.Containers[0].Env = append(primaryStatefulSet.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{
			Name: "SQLD_AUTH_JWT_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: utils.GetAuthSecretName(database),
					},
					Key: "PUBLIC_KEY",
				},
			},
		})
	}
	for _, env := range database.Spec.Env {
		if !(env.Name == "SQLD_NODE" || env.Name == "SQLD_AUTH_JWT_KEY") {
			primaryStatefulSet.Spec.Template.Spec.Containers[0].Env = append(primaryStatefulSet.Spec.Template.Spec.Containers[0].Env, env)
		} else {
			log.Info(fmt.Sprintf("overwriting provided env %v with default generated values", env.Name))
		}
	}
	return primaryStatefulSet
}
