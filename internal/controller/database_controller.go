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
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=libsql.ahti.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=libsql.ahti.io,resources=databases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=libsql.ahti.io,resources=databases/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
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
		return ctrl.Result{}, client.IgnoreNotFound(err)
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
	// create secret if not yet created
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

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&libsqlv1.Database{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
