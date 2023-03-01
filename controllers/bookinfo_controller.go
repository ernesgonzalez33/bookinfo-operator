/*
Copyright 2023.

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

package controllers

import (
	"context"
	"fmt"
	"time"

	networkingv1beta1 "istio.io/api/networking/v1beta1"
	istiov1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	maistrav1 "maistra.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	deployv1alpha1 "github.com/ernesgonzalez33/bookinfo-operator/api/v1alpha1"
)

// typeAvailableBookinfo represents the status of the Deployment reconciliation
const typeAvailableBookinfo = "Available"

const (
	detailsName     = "details"
	ratingsName     = "ratings"
	reviewsName     = "reviews"
	productpageName = "productpage"

	detailsVersion     = "v1"
	ratingsVersion     = "v1"
	reviewsVersion     = "v1"
	productpageVersion = "v1"

	detailsImage     = "docker.io/maistra/examples-bookinfo-details-v1:0.12.0"
	ratingsImage     = "docker.io/maistra/examples-bookinfo-ratings-v1:0.12.0"
	reviewsImage     = "docker.io/maistra/examples-bookinfo-reviews-v1:0.12.0"
	productpageImage = "docker.io/maistra/examples-bookinfo-productpage-v1:0.12.0"

	gatewayName = "bookinfo-gateway"

	microsPort = 9080
)

// BookinfoReconciler reconciles a Bookinfo object
type BookinfoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=deploy.kubernesto.io,resources=bookinfoes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deploy.kubernesto.io,resources=bookinfoes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deploy.kubernesto.io,resources=bookinfoes/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=maistra.io,resources=servicemeshmembers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="networking.istio.io",resources=destinationrules;virtualservices;gateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Bookinfo object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *BookinfoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	bookinfo := &deployv1alpha1.Bookinfo{}
	err := r.Get(ctx, req.NamespacedName, bookinfo)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found
			logger.Info("bookinfo resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object
		logger.Error(err, "Failed to get bookinfo")
		return ctrl.Result{}, err
	}

	if bookinfo.Status.Conditions == nil || len(bookinfo.Status.Conditions) == 0 {
		meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, bookinfo); err != nil {
			logger.Error(err, "Failed to update bookinfo status")
			return ctrl.Result{}, err
		}

		// Re-fetch bookinfo because of the change of status
		if err := r.Get(ctx, req.NamespacedName, bookinfo); err != nil {
			logger.Error(err, "Failed to re-fetch bookinfo after updated status")
			return ctrl.Result{}, err
		}
	}

	// Checking service mesh resources
	logger.Info("Checking if compatibility with Service Mesh is enabled")
	if bookinfo.Spec.MeshEnabled {

		// Checking SMM
		logger.Info("Checking if the SMM is already created")
		found := &maistrav1.ServiceMeshMember{}
		err = r.Get(ctx, types.NamespacedName{Name: "default", Namespace: req.Namespace}, found)
		if err != nil && apierrors.IsNotFound(err) {
			smm, err := r.getServiceMeshMemberDetails(bookinfo)
			if err != nil {
				logger.Error(err, "Failed to define Service Mesh Member")

				// The following implementation will update the status
				meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("Failed to create Service Mesh Member for the custom resource (%s): (%s)", bookinfo.Name, err)})

				if err := r.Status().Update(ctx, bookinfo); err != nil {
					logger.Error(err, "Failed to update Bookinfo status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, err
			}

			logger.Info("Creating Service Mesh Member")
			if err = r.Create(ctx, smm); err != nil {
				logger.Error(err, "Failed to create Service Mesh Member")
				return ctrl.Result{}, err
			}

			// Service Mesh Member created successfully
			return ctrl.Result{RequeueAfter: time.Second}, nil

		} else if err != nil {
			logger.Error(err, "Failed to get Service Mesh Member")
			return ctrl.Result{}, err
		}

		// Checking Gateway
		logger.Info("Checking if the Gateway is already created")
		gwFound := &istiov1beta1.Gateway{}
		err = r.Get(ctx, types.NamespacedName{Name: gatewayName, Namespace: req.Namespace}, gwFound)
		if err != nil && apierrors.IsNotFound(err) {
			gw, err := r.getGWDetails(bookinfo)
			if err != nil {
				logger.Error(err, "Failed to define", "gateway", gatewayName)

				// The following implementation will update the status
				meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("Failed to create Gateway for the custom resource (%s): (%s)", bookinfo.Name, err)})

				if err := r.Status().Update(ctx, bookinfo); err != nil {
					logger.Error(err, "Failed to update Bookinfo status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, err
			}

			logger.Info("Creating", "gateway", gatewayName)
			if err = r.Create(ctx, gw); err != nil {
				logger.Error(err, "Failed to create", "gateway", gatewayName)
				return ctrl.Result{}, err
			}

			// Gateway created successfully
			return ctrl.Result{RequeueAfter: time.Second}, nil

		} else if err != nil {
			logger.Error(err, "Failed to get", "gateway", gatewayName)
			return ctrl.Result{}, err
		}

		// Checking Virtual Service
		logger.Info("Checking if the Virtual service is already created")
		vsFound := &istiov1beta1.VirtualService{}
		err = r.Get(ctx, types.NamespacedName{Name: bookinfo.Name, Namespace: req.Namespace}, vsFound)
		if err != nil && apierrors.IsNotFound(err) {
			gw, err := r.getVSDetails(bookinfo)
			if err != nil {
				logger.Error(err, "Failed to define", "virtual service", req.Name)

				// The following implementation will update the status
				meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("Failed to create Virtual Service %s for the custom resource (%s): (%s)", bookinfo.Name, bookinfo.Name, err)})

				if err := r.Status().Update(ctx, bookinfo); err != nil {
					logger.Error(err, "Failed to update Bookinfo status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, err
			}

			logger.Info("Creating", "virtual service", bookinfo.Name)
			if err = r.Create(ctx, gw); err != nil {
				logger.Error(err, "Failed to create", "virtual service", bookinfo.Name)
				return ctrl.Result{}, err
			}

			// Virtual Service created successfully
			return ctrl.Result{RequeueAfter: time.Second}, nil

		} else if err != nil {
			logger.Error(err, "Failed to get", "virtual service", bookinfo.Name)
			return ctrl.Result{}, err
		}

		// Checking Details Destination Rule
		logger.Info("Checking if the resource is already created", "destination rule", detailsName)
		detailsDRFound := &istiov1beta1.DestinationRule{}
		err = r.Get(ctx, types.NamespacedName{Name: detailsName, Namespace: req.Namespace}, detailsDRFound)
		if err != nil && apierrors.IsNotFound(err) {
			gw, err := r.getDRDetails(detailsName, detailsVersion, bookinfo)
			if err != nil {
				logger.Error(err, "Failed to define", "destination rule", detailsName)

				// The following implementation will update the status
				meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("Failed to create Destination Rule %s for the custom resource (%s): (%s)", detailsName, bookinfo.Name, err)})

				if err := r.Status().Update(ctx, bookinfo); err != nil {
					logger.Error(err, "Failed to update Bookinfo status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, err
			}

			logger.Info("Creating", "destination rule", detailsName)
			if err = r.Create(ctx, gw); err != nil {
				logger.Error(err, "Failed to create", "destination rule", detailsName)
				return ctrl.Result{}, err
			}

			// Destination Rule created successfully
			return ctrl.Result{RequeueAfter: time.Second}, nil

		} else if err != nil {
			logger.Error(err, "Failed to get", "destination rule", detailsName)
			return ctrl.Result{}, err
		}

		// Checking Reviews Destination Rule
		logger.Info("Checking if the resource is already created", "destination rule", reviewsName)
		reviewsDRFound := &istiov1beta1.DestinationRule{}
		err = r.Get(ctx, types.NamespacedName{Name: reviewsName, Namespace: req.Namespace}, reviewsDRFound)
		if err != nil && apierrors.IsNotFound(err) {
			gw, err := r.getDRDetails(reviewsName, reviewsVersion, bookinfo)
			if err != nil {
				logger.Error(err, "Failed to define", "destination rule", reviewsName)

				// The following implementation will update the status
				meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("Failed to create Destination Rule %s for the custom resource (%s): (%s)", reviewsName, bookinfo.Name, err)})

				if err := r.Status().Update(ctx, bookinfo); err != nil {
					logger.Error(err, "Failed to update Bookinfo status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, err
			}

			logger.Info("Creating", "destination rule", reviewsName)
			if err = r.Create(ctx, gw); err != nil {
				logger.Error(err, "Failed to create", "destination rule", reviewsName)
				return ctrl.Result{}, err
			}

			// Destination Rule created successfully
			return ctrl.Result{RequeueAfter: time.Second}, nil

		} else if err != nil {
			logger.Error(err, "Failed to get", "destination rule", reviewsName)
			return ctrl.Result{}, err
		}

		// Checking Ratings Destination Rule
		logger.Info("Checking if the resource is already created", "destination rule", ratingsName)
		ratingsDRFound := &istiov1beta1.DestinationRule{}
		err = r.Get(ctx, types.NamespacedName{Name: ratingsName, Namespace: req.Namespace}, ratingsDRFound)
		if err != nil && apierrors.IsNotFound(err) {
			gw, err := r.getDRDetails(ratingsName, ratingsVersion, bookinfo)
			if err != nil {
				logger.Error(err, "Failed to define", "destination rule", ratingsName)

				// The following implementation will update the status
				meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("Failed to create Destination Rule %s for the custom resource (%s): (%s)", ratingsName, bookinfo.Name, err)})

				if err := r.Status().Update(ctx, bookinfo); err != nil {
					logger.Error(err, "Failed to update Bookinfo status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, err
			}

			logger.Info("Creating", "destination rule", ratingsName)
			if err = r.Create(ctx, gw); err != nil {
				logger.Error(err, "Failed to create", "destination rule", ratingsName)
				return ctrl.Result{}, err
			}

			// Destination Rule created successfully
			return ctrl.Result{RequeueAfter: time.Second}, nil

		} else if err != nil {
			logger.Error(err, "Failed to get", "destination rule", ratingsName)
			return ctrl.Result{}, err
		}

		// Checking Ratings Destination Rule
		logger.Info("Checking if the resource is already created", "destination rule", productpageName)
		productpageDRFound := &istiov1beta1.DestinationRule{}
		err = r.Get(ctx, types.NamespacedName{Name: productpageName, Namespace: req.Namespace}, productpageDRFound)
		if err != nil && apierrors.IsNotFound(err) {
			gw, err := r.getDRDetails(productpageName, productpageVersion, bookinfo)
			if err != nil {
				logger.Error(err, "Failed to define", "destination rule", productpageName)

				// The following implementation will update the status
				meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("Failed to create Destination Rule %s for the custom resource (%s): (%s)", productpageName, bookinfo.Name, err)})

				if err := r.Status().Update(ctx, bookinfo); err != nil {
					logger.Error(err, "Failed to update Bookinfo status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, err
			}

			logger.Info("Creating", "destination rule", productpageName)
			if err = r.Create(ctx, gw); err != nil {
				logger.Error(err, "Failed to create", "destination rule", productpageName)
				return ctrl.Result{}, err
			}

			// Destination Rule created successfully
			return ctrl.Result{RequeueAfter: time.Second}, nil

		} else if err != nil {
			logger.Error(err, "Failed to get", "destination rule", productpageName)
			return ctrl.Result{}, err
		}
	}

	// Start checking Bookinfo resources
	logger.Info("Checking bookinfo resources")
	logger.Info("Checking bookinfo services")

	// Check Details service
	logger.Info("Checking", "service", detailsName)
	detailsSvc := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: detailsName, Namespace: req.Namespace}, detailsSvc)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details service
		svc, err := r.getServiceDetails(detailsName, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "service", detailsName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service %s for the custom resource (%s): (%s)", detailsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "service", detailsName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "service", detailsName)
			return ctrl.Result{}, err
		}

		// Service created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "service", detailsName)
		return ctrl.Result{}, err
	}

	// Check Ratings service
	logger.Info("Checking", "service", ratingsName)
	ratingsSvc := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: ratingsName, Namespace: req.Namespace}, ratingsSvc)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details service
		svc, err := r.getServiceDetails(ratingsName, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "service", ratingsName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service %s for the custom resource (%s): (%s)", ratingsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "service", ratingsName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "service", ratingsName)
			return ctrl.Result{}, err
		}

		// Service created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "service", ratingsName)
		return ctrl.Result{}, err
	}

	// Check Reviews service
	logger.Info("Checking", "service", reviewsName)
	reviewsSvc := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: reviewsName, Namespace: req.Namespace}, reviewsSvc)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details service
		svc, err := r.getServiceDetails(reviewsName, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "service", reviewsName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service %s for the custom resource (%s): (%s)", reviewsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "service", reviewsName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "service", reviewsName)
			return ctrl.Result{}, err
		}

		// Service created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "service", reviewsName)
		return ctrl.Result{}, err
	}

	// Check Productpage service
	logger.Info("Checking", "service", productpageName)
	productpageSvc := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: productpageName, Namespace: req.Namespace}, productpageSvc)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details service
		svc, err := r.getServiceDetails(productpageName, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "service", productpageName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service %s for the custom resource (%s): (%s)", productpageName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "service", productpageName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "service", productpageName)
			return ctrl.Result{}, err
		}

		// Service created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "service", productpageName)
		return ctrl.Result{}, err
	}

	logger.Info("Checking bookinfo service accounts")

	// Check Details service account
	logger.Info("Checking", "service account", detailsName)
	detailsSa := &corev1.ServiceAccount{}
	err = r.Get(ctx, types.NamespacedName{Name: detailsName, Namespace: req.Namespace}, detailsSa)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details service account
		svc, err := r.getServiceAccountDetails(detailsName, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "service account", detailsName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service Account %s for the custom resource (%s): (%s)", detailsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "service account", detailsName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "service account", detailsName)
			return ctrl.Result{}, err
		}

		// Service account created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "service account", detailsName)
		return ctrl.Result{}, err
	}

	// Check Ratings service account
	logger.Info("Checking", "service account", ratingsName)
	ratingsSa := &corev1.ServiceAccount{}
	err = r.Get(ctx, types.NamespacedName{Name: ratingsName, Namespace: req.Namespace}, ratingsSa)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details service account
		svc, err := r.getServiceAccountDetails(ratingsName, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "service account", ratingsName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service Account %s for the custom resource (%s): (%s)", ratingsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "service account", ratingsName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "service account", ratingsName)
			return ctrl.Result{}, err
		}

		// Service account created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "service account", ratingsName)
		return ctrl.Result{}, err
	}

	// Check Reviews service account
	logger.Info("Checking", "service account", reviewsName)
	reviewsSa := &corev1.ServiceAccount{}
	err = r.Get(ctx, types.NamespacedName{Name: reviewsName, Namespace: req.Namespace}, reviewsSa)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details service account
		svc, err := r.getServiceAccountDetails(reviewsName, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "service account", reviewsName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service Account %s for the custom resource (%s): (%s)", reviewsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "service account", reviewsName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "service account", reviewsName)
			return ctrl.Result{}, err
		}

		// Service account created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "service account", reviewsName)
		return ctrl.Result{}, err
	}

	// Check Productpage service account
	logger.Info("Checking", "service account", productpageName)
	productpageSa := &corev1.ServiceAccount{}
	err = r.Get(ctx, types.NamespacedName{Name: productpageName, Namespace: req.Namespace}, productpageSa)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details service account
		svc, err := r.getServiceAccountDetails(productpageName, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "service account", productpageName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service Account %s for the custom resource (%s): (%s)", productpageName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "service account", productpageName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "service account", productpageName)
			return ctrl.Result{}, err
		}

		// Service account created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "service account", productpageName)
		return ctrl.Result{}, err
	}

	logger.Info("Checking bookinfo deployments")

	// Check Details deployment
	logger.Info("Checking", "deployment", detailsName)
	detailsDep := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: detailsName + "-" + detailsVersion, Namespace: req.Namespace}, detailsDep)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details deployment
		svc, err := r.getDeploymentDetails(detailsName, detailsVersion, detailsImage, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "deployment", detailsName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create deployment %s for the custom resource (%s): (%s)", detailsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "deployment", detailsName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "deployment", detailsName)
			return ctrl.Result{}, err
		}

		// deployment created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "deployment", detailsName)
		return ctrl.Result{}, err
	}

	// Checking if the replicas are ok in details deploment
	replicas := bookinfo.Spec.Replicas
	if *detailsDep.Spec.Replicas != replicas {
		detailsDep.Spec.Replicas = &replicas
		if err = r.Update(ctx, detailsDep); err != nil {
			logger.Error(err, "Failed to update deployment", "deployment", detailsName)

			if err := r.Get(ctx, req.NamespacedName, bookinfo); err != nil {
				logger.Error(err, "Failed to re-fetch bookinfo")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Resizing",
				Message: fmt.Sprintf("Failed to update deployment %s for the custom resource (%s): (%s)", detailsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", detailsName, replicas)})

	if err := r.Status().Update(ctx, bookinfo); err != nil {
		logger.Error(err, "Failed to update Bookinfo status")
		return ctrl.Result{}, err
	}

	// Check Ratings deployment
	logger.Info("Checking", "deployment", ratingsName)
	ratingsDep := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: ratingsName + "-" + ratingsVersion, Namespace: req.Namespace}, ratingsDep)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details deployment
		svc, err := r.getDeploymentDetails(ratingsName, ratingsVersion, ratingsImage, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "deployment", ratingsName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create deployment %s for the custom resource (%s): (%s)", ratingsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "deployment", ratingsName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "deployment", ratingsName)
			return ctrl.Result{}, err
		}

		// deployment created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "deployment", ratingsName)
		return ctrl.Result{}, err
	}

	// Checking if the replicas are ok in ratings deploment
	replicas = bookinfo.Spec.Replicas
	if *ratingsDep.Spec.Replicas != replicas {
		ratingsDep.Spec.Replicas = &replicas
		if err = r.Update(ctx, ratingsDep); err != nil {
			logger.Error(err, "Failed to update deployment", "deployment", ratingsName)

			if err := r.Get(ctx, req.NamespacedName, bookinfo); err != nil {
				logger.Error(err, "Failed to re-fetch bookinfo")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Resizing",
				Message: fmt.Sprintf("Failed to update deployment %s for the custom resource (%s): (%s)", ratingsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", ratingsName, replicas)})

	if err := r.Status().Update(ctx, bookinfo); err != nil {
		logger.Error(err, "Failed to update Bookinfo status")
		return ctrl.Result{}, err
	}

	// Check Reviews deployment
	logger.Info("Checking", "deployment", reviewsName)
	reviewsDep := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: reviewsName + "-" + reviewsVersion, Namespace: req.Namespace}, reviewsDep)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details deployment
		svc, err := r.getDeploymentDetails(reviewsName, reviewsVersion, reviewsImage, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "deployment", reviewsName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create deployment %s for the custom resource (%s): (%s)", reviewsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "deployment", reviewsName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "deployment", reviewsName)
			return ctrl.Result{}, err
		}

		// deployment created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "deployment", reviewsName)
		return ctrl.Result{}, err
	}

	// Checking if the replicas are ok in reviews deploment
	replicas = bookinfo.Spec.Replicas
	if *reviewsDep.Spec.Replicas != replicas {
		reviewsDep.Spec.Replicas = &replicas
		if err = r.Update(ctx, reviewsDep); err != nil {
			logger.Error(err, "Failed to update deployment", "deployment", reviewsName)

			if err := r.Get(ctx, req.NamespacedName, bookinfo); err != nil {
				logger.Error(err, "Failed to re-fetch bookinfo")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Resizing",
				Message: fmt.Sprintf("Failed to update deployment %s for the custom resource (%s): (%s)", reviewsName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", reviewsName, replicas)})

	if err := r.Status().Update(ctx, bookinfo); err != nil {
		logger.Error(err, "Failed to update Bookinfo status")
		return ctrl.Result{}, err
	}

	// Check Productpage deployment
	logger.Info("Checking", "deployment", productpageName)
	productpageDep := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: productpageName + "-" + productpageVersion, Namespace: req.Namespace}, productpageDep)
	if err != nil && apierrors.IsNotFound(err) {
		// Define details deployment
		svc, err := r.getDeploymentDetails(productpageName, productpageVersion, productpageImage, bookinfo)
		if err != nil {
			logger.Error(err, "Failed to define", "deployment", productpageName)

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create deployment %s for the custom resource (%s): (%s)", productpageName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		logger.Info("Creating", "deployment", productpageName)
		if err = r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create", "deployment", productpageName)
			return ctrl.Result{}, err
		}

		// deployment created successfully
		return ctrl.Result{RequeueAfter: time.Second}, nil

	} else if err != nil {
		logger.Error(err, "Failed to get", "deployment", productpageName)
		return ctrl.Result{}, err
	}

	// Checking if the replicas are ok in productpage deploment
	replicas = bookinfo.Spec.Replicas
	if *productpageDep.Spec.Replicas != replicas {
		productpageDep.Spec.Replicas = &replicas
		if err = r.Update(ctx, productpageDep); err != nil {
			logger.Error(err, "Failed to update deployment", "deployment", productpageName)

			if err := r.Get(ctx, req.NamespacedName, bookinfo); err != nil {
				logger.Error(err, "Failed to re-fetch bookinfo")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
				Status: metav1.ConditionFalse, Reason: "Resizing",
				Message: fmt.Sprintf("Failed to update deployment %s for the custom resource (%s): (%s)", productpageName, bookinfo.Name, err)})

			if err := r.Status().Update(ctx, bookinfo); err != nil {
				logger.Error(err, "Failed to update Bookinfo status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&bookinfo.Status.Conditions, metav1.Condition{Type: typeAvailableBookinfo,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", productpageName, replicas)})

	if err := r.Status().Update(ctx, bookinfo); err != nil {
		logger.Error(err, "Failed to update Bookinfo status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BookinfoReconciler) getServiceDetails(serviceName string, bookinfo *deployv1alpha1.Bookinfo) (client.Object, error) {

	ls := labelsForBookinfo(bookinfo.Name)
	ls["name"] = serviceName
	ls["service"] = serviceName

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: bookinfo.Namespace,
			Labels:    ls,
		},

		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Name: "http", Port: microsPort}},
			Selector: map[string]string{
				"app": serviceName,
			},
		},
	}

	// Set the ownerRef for the Service
	if err := ctrl.SetControllerReference(bookinfo, svc, r.Scheme); err != nil {
		return nil, err
	}

	return svc, nil

}

func (r *BookinfoReconciler) getServiceAccountDetails(serviceAccountName string, bookinfo *deployv1alpha1.Bookinfo) (client.Object, error) {

	ls := labelsForBookinfo(bookinfo.Name)
	ls["account"] = serviceAccountName

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: bookinfo.Namespace,
			Labels:    ls,
		},
	}

	// Set the ownerRef for the Service
	if err := ctrl.SetControllerReference(bookinfo, sa, r.Scheme); err != nil {
		return nil, err
	}

	return sa, nil

}

func (r *BookinfoReconciler) getDeploymentDetails(name string, version string, image string, bookinfo *deployv1alpha1.Bookinfo) (client.Object, error) {

	ls := labelsForBookinfo(bookinfo.Name)
	ls["app"] = name
	ls["version"] = version

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-" + version,
			Namespace: bookinfo.Namespace,
			Labels:    ls,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &bookinfo.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"sidecar.istio.io/inject": "true",
					},
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: name,
					Containers: []corev1.Container{{
						Name:            name,
						Image:           image,
						ImagePullPolicy: "IfNotPresent",
						Ports: []corev1.ContainerPort{{
							ContainerPort: microsPort,
						}},
					}},
				},
			},
		},
	}

	// Set the ownerRef for the Service
	if err := ctrl.SetControllerReference(bookinfo, dep, r.Scheme); err != nil {
		return nil, err
	}

	return dep, nil

}

func (r *BookinfoReconciler) getServiceMeshMemberDetails(bookinfo *deployv1alpha1.Bookinfo) (client.Object, error) {

	smm := &maistrav1.ServiceMeshMember{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: bookinfo.Namespace,
		},
		Spec: maistrav1.ServiceMeshMemberSpec{
			ControlPlaneRef: maistrav1.ServiceMeshControlPlaneRef{
				Name:      bookinfo.Spec.MeshControlPlaneName,
				Namespace: bookinfo.Spec.MeshControlPlaneNamespace,
			},
		},
	}

	// Set the ownerRef for the Service Mesh Member
	if err := ctrl.SetControllerReference(bookinfo, smm, r.Scheme); err != nil {
		return nil, err
	}

	return smm, nil

}

func (r *BookinfoReconciler) getGWDetails(bookinfo *deployv1alpha1.Bookinfo) (client.Object, error) {

	gw := &istiov1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: bookinfo.Namespace,
		},
		Spec: networkingv1beta1.Gateway{
			Selector: map[string]string{
				"istio": "ingressgateway",
			},
			Servers: []*networkingv1beta1.Server{{
				Port: &networkingv1beta1.Port{
					Name:     "http",
					Number:   80,
					Protocol: "http",
				},
				Hosts: []string{
					"*",
				},
			}},
		},
	}

	// Set the ownerRef for the Service Mesh Member
	if err := ctrl.SetControllerReference(bookinfo, gw, r.Scheme); err != nil {
		return nil, err
	}

	return gw, nil

}

func (r *BookinfoReconciler) getVSDetails(bookinfo *deployv1alpha1.Bookinfo) (client.Object, error) {

	vs := &istiov1beta1.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bookinfo.Name,
			Namespace: bookinfo.Namespace,
		},
		Spec: networkingv1beta1.VirtualService{
			Hosts: []string{
				"*",
			},
			Gateways: []string{
				gatewayName,
			},
			Http: []*networkingv1beta1.HTTPRoute{{
				Match: []*networkingv1beta1.HTTPMatchRequest{{
					Uri: &networkingv1beta1.StringMatch{
						MatchType: &networkingv1beta1.StringMatch_Exact{
							Exact: "/productpage",
						},
					},
				}, {
					Uri: &networkingv1beta1.StringMatch{
						MatchType: &networkingv1beta1.StringMatch_Prefix{
							Prefix: "/static",
						},
					},
				}, {
					Uri: &networkingv1beta1.StringMatch{
						MatchType: &networkingv1beta1.StringMatch_Exact{
							Exact: "/login",
						},
					},
				}, {
					Uri: &networkingv1beta1.StringMatch{
						MatchType: &networkingv1beta1.StringMatch_Exact{
							Exact: "/logout",
						},
					},
				}, {
					Uri: &networkingv1beta1.StringMatch{
						MatchType: &networkingv1beta1.StringMatch_Prefix{
							Prefix: "/api/v1/products",
						},
					},
				}},
				Route: []*networkingv1beta1.HTTPRouteDestination{{
					Destination: &networkingv1beta1.Destination{
						Host: productpageName,
						Port: &networkingv1beta1.PortSelector{
							Number: microsPort,
						},
					},
				}},
			},
			},
		},
	}

	// Set the ownerRef for the Service Mesh Member
	if err := ctrl.SetControllerReference(bookinfo, vs, r.Scheme); err != nil {
		return nil, err
	}

	return vs, nil

}

func (r *BookinfoReconciler) getDRDetails(name string, version string, bookinfo *deployv1alpha1.Bookinfo) (client.Object, error) {

	dr := &istiov1beta1.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: bookinfo.Namespace,
		},
		Spec: networkingv1beta1.DestinationRule{
			Host: name,
			TrafficPolicy: &networkingv1beta1.TrafficPolicy{
				Tls: &networkingv1beta1.ClientTLSSettings{
					Mode: networkingv1beta1.ClientTLSSettings_ISTIO_MUTUAL,
				},
			},
			Subsets: []*networkingv1beta1.Subset{{
				Name: version,
				Labels: map[string]string{
					"version": version,
				},
			}},
		},
	}

	// Set the ownerRef for the Service Mesh Member
	if err := ctrl.SetControllerReference(bookinfo, dr, r.Scheme); err != nil {
		return nil, err
	}

	return dr, nil

}

func labelsForBookinfo(name string) map[string]string {

	return map[string]string{"app.kubernetes.io/name": "Bookinfo",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/part-of":    "bookinfo-operator",
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *BookinfoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deployv1alpha1.Bookinfo{}).
		Complete(r)
}
