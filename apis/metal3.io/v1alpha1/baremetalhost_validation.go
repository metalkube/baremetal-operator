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
	var errs []error
	if old.Spec.BMC.Address != "" && new.Spec.BMC.Address != old.Spec.BMC.Address {
		errs = append(errs, fmt.Errorf("BMC address can not be changed once it is set"))
	}

	if old.Spec.BootMACAddress != "" && new.Spec.BootMACAddress != old.Spec.BootMACAddress {
		errs = append(errs, fmt.Errorf("bootMACAddress can not be changed once it is set"))
	}

	return errs
}
