package v1alpha1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/errors"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// log is for logging in this package.
var log = logf.Log.WithName("baremetalhost-validation")

func (host *BareMetalHost) Validate(old *BareMetalHost) error {
	log.Info("validate", "name", host.Name)
	var errs []error

	if err := validateRAID(host.Spec.RAID); err != nil {
		errs = append(errs, err)
	}

	if old != nil {
		if updateErrs := validateUpdate(old, host); updateErrs != nil {
			errs = append(errs, updateErrs...)
		}
	}

	return errors.NewAggregate(errs)
}

func validateRAID(r *RAIDConfig) error {
	if r == nil {
		return nil
	}

	if len(r.HardwareRAIDVolumes) > 0 && len(r.SoftwareRAIDVolumes) > 0 {
		return fmt.Errorf("hardwareRAIDVolumes and softwareRAIDVolumes can not be set at the same time")
	}

	return nil
}

func validateUpdate(old, new *BareMetalHost) []error {
	return nil
}
