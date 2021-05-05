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

package v1alpha1

import (
	"fmt"
	"k8s.io/apimachinery/pkg/util/errors"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var baremetalhostlog = logf.Log.WithName("baremetalhost-resource")

func (r *BareMetalHost) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:verbs=create;update,path=/validate-metal3-io-v1alpha1-baremetalhost,mutating=false,failurePolicy=fail,sideEffects=none,admissionReviewVersions=v1;v1beta1,groups=metal3.io,resources=baremetalhosts,versions=v1alpha1,name=vbaremetalhost.kb.io

var _ webhook.Validator = &BareMetalHost{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *BareMetalHost) ValidateCreate() error {
	baremetalhostlog.Info("validate create", "name", r.Name)
	return errors.NewAggregate(r.validateCreate())
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *BareMetalHost) ValidateUpdate(old runtime.Object) error {
	baremetalhostlog.Info("validate update", "name", r.Name)
	bmh, casted := old.(*BareMetalHost)
	if !casted {
		baremetalhostlog.Error(fmt.Errorf("old object conversion error"), "validate update error")
		return nil
	}
	return errors.NewAggregate(r.validateUpdate(bmh))
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *BareMetalHost) ValidateDelete() error {
	return nil
}
