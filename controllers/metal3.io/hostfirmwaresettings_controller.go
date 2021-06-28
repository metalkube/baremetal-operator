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
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
)

// HostFirmwareSettingsReconciler reconciles a HostFirmwareSettings object
type HostFirmwareSettingsReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=metal3.io,resources=hostfirmwaresettings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=metal3.io,resources=hostfirmwaresettings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=firmwareschema,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=metal3.io,resources=firmwareschema/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.6.4/pkg/reconcile
func (r *HostFirmwareSettingsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {

	reqLogger := r.Log.WithValues("hostfirmwaresettings", req.NamespacedName)
	reqLogger.Info("start")

	// Fetch the HostFirmwareSettings
	hfs := &metal3v1alpha1.HostFirmwareSettings{}
	if err = r.Get(ctx, req.NamespacedName, hfs); err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after
			// reconcile request. Return and don't requeue
			reqLogger.Info("could not find host firmware settings")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, errors.Wrap(err, "could not load host firmware settings")
	}

	// Check if settings has been cleared during pivot, and if so, restore firmwareSchema from annotation
	if hfs.Status.FirmwareSchema == nil {
		if _, ok := hfs.Annotations[metal3v1alpha1.FirmwareSchemaNameAnnotation]; ok {
			hfs.Status.FirmwareSchema = &metal3v1alpha1.SchemaReference{
				Name:      hfs.Annotations[metal3v1alpha1.FirmwareSchemaNameAnnotation],
				Namespace: hfs.Annotations[metal3v1alpha1.FirmwareSchemaNamespaceAnnotation]}

			if r.Update(context.TODO(), hfs) != nil {
				reqLogger.Info("could not update hostfirmwaresettings", "hostfirmwaresettings", req.NamespacedName)
			}
		}
	}

	// The Settings field cleared during pivot will be updated by baremetalhost controller which accesses Ironic via provisioner
	if hfs.Status.Settings == nil {
		reqLogger.Info("hostfirmwaresettings settings field has been cleared", "hostfirmwaresettings", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// Get the referenced schema
	var schema *metal3v1alpha1.FirmwareSchema
	if hfs.Status.FirmwareSchema != nil {
		schema = &metal3v1alpha1.FirmwareSchema{}
		if err = r.Get(ctx, client.ObjectKey{
			Namespace: hfs.Status.FirmwareSchema.Namespace,
			Name:      hfs.Status.FirmwareSchema.Name}, schema); err != nil {
			if k8serrors.IsNotFound(err) {
				reqLogger.Info("could not find firmware schema for host", "hostfirmwaresettings", req.NamespacedName)
				return ctrl.Result{}, nil
			}

			// Error reading the object - requeue the request.
			return ctrl.Result{}, errors.Wrap(err, "could not load firmware schema")
		}
	} else {
		schema = nil
	}

	if err = ValidateHostFirmwareSettings(hfs, schema); err != nil {
		reqLogger.Info("hostFirmwareSetting validation failed", "error", fmt.Errorf("invalid spec settings detected %v", err))

	}

	// only run again on changes
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HostFirmwareSettingsReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&metal3v1alpha1.HostFirmwareSettings{}).
		Complete(r)
}

// Validate the HostFirmwareSetting Spec
func ValidateHostFirmwareSettings(hfs *metal3v1alpha1.HostFirmwareSettings, schema *metal3v1alpha1.FirmwareSchema) error {

	if hfs == nil {
		return fmt.Errorf("Missing parameter to ValidateHostFirmwareSettingsWithSchema")
	}

	for name, val := range hfs.Spec.Settings {
		// Prohibit any Spec settings with "Password"
		if strings.Contains(name, "Password") {
			return InvalidHostFirmwareNameError{name: name}
		}

		// The setting must be in the Status
		if _, ok := hfs.Status.Settings[name]; !ok {
			return InvalidHostFirmwareNameError{name: name}
		}

		// check validity of updated value
		if schema != nil && !schema.CheckSettingIsValid(name, val, schema.Spec.Schema) {
			return InvalidHostFirmwareValueError{name: name, value: val.String()}
		}
	}

	return nil
}

// Generate a name based on the schema keys which should be the same for similar hardware
func GetSchemaName(schema map[string]metal3v1alpha1.SettingSchema) string {

	// Schemas from the same vendor and model should be identical for both keys and values.
	// Hash the keys of the map to get a identifier unique to this schema.
	keys := make([]string, 0, len(schema))
	for k := range schema {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%v", keys)))
	hash := fmt.Sprintf("%x", h.Sum(nil))[:8]

	return "schema-" + hash
}
