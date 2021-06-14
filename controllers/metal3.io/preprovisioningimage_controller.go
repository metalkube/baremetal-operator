/*


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
	"os"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3iov1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
)

// PreprovisioningImageReconciler reconciles a PreprovisioningImage object
type PreprovisioningImageReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=metal3.io,resources=preprovisioningimages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=metal3.io,resources=preprovisioningimages/status,verbs=get;update;patch

func (r *PreprovisioningImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("preprovisioningimage", req.NamespacedName)

	// your logic here

	return ctrl.Result{}, nil
}

func (r *PreprovisioningImageReconciler) CanStart() bool {
	deployKernelURL := os.Getenv("DEPLOY_KERNEL_URL")
	deployRamdiskURL := os.Getenv("DEPLOY_RAMDISK_URL")
	deployISOURL := os.Getenv("DEPLOY_ISO_URL")
	hasCfg := (deployISOURL != "" ||
		(deployKernelURL != "" && deployRamdiskURL != ""))
	if hasCfg {
		r.Log.Info("found deploy image data",
			"iso_url", deployISOURL,
			"ramdisk_url", deployRamdiskURL,
			"kernel_url", deployKernelURL)
	} else {
		r.Log.Info("not starting preprovisioning image controller; no image data found")
	}
	return hasCfg
}

func (r *PreprovisioningImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&metal3iov1alpha1.PreprovisioningImage{}).
		Complete(r)
}
