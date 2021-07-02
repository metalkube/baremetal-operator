package controllers

import (
	"fmt"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// hostStateMachine is a finite state machine that manages transitions between
// the states of a BareMetalHost.
type hostStateMachine struct {
	Host        *metal3v1alpha1.BareMetalHost
	NextState   metal3v1alpha1.ProvisioningState
	Reconciler  *BareMetalHostReconciler
	Provisioner provisioner.Provisioner
	haveCreds   bool
}

func newHostStateMachine(host *metal3v1alpha1.BareMetalHost,
	reconciler *BareMetalHostReconciler,
	provisioner provisioner.Provisioner,
	haveCreds bool) *hostStateMachine {
	currentState := host.Status.Provisioning.State
	r := hostStateMachine{
		Host:        host,
		NextState:   currentState, // Remain in current state by default
		Reconciler:  reconciler,
		Provisioner: provisioner,
		haveCreds:   haveCreds,
	}
	return &r
}

type stateHandler func(*reconcileInfo) actionResult

func (hsm *hostStateMachine) handlers() map[metal3v1alpha1.ProvisioningState]stateHandler {
	return map[metal3v1alpha1.ProvisioningState]stateHandler{
		metal3v1alpha1.StateNone:                  hsm.handleNone,
		metal3v1alpha1.StateUnmanaged:             hsm.handleUnmanaged,
		metal3v1alpha1.StateRegistering:           hsm.handleRegistering,
		metal3v1alpha1.StateInspecting:            hsm.handleInspecting,
		metal3v1alpha1.StateExternallyProvisioned: hsm.handleExternallyProvisioned,
		metal3v1alpha1.StateMatchProfile:          hsm.handleMatchProfile,
		metal3v1alpha1.StateAvailable:             hsm.handleReady,
		metal3v1alpha1.StatePreparing:             hsm.handlePreparing,
		metal3v1alpha1.StateReady:                 hsm.handleReady,
		metal3v1alpha1.StateProvisioning:          hsm.handleProvisioning,
		metal3v1alpha1.StateProvisioned:           hsm.handleProvisioned,
		metal3v1alpha1.StateDeprovisioning:        hsm.handleDeprovisioning,
		metal3v1alpha1.StateDeleting:              hsm.handleDeleting,
	}
}

func recordStateBegin(host *metal3v1alpha1.BareMetalHost, state metal3v1alpha1.ProvisioningState, time metav1.Time) {
	if nextMetric := host.OperationMetricForState(state); nextMetric != nil {
		if nextMetric.Start.IsZero() || !nextMetric.End.IsZero() {
			*nextMetric = metal3v1alpha1.OperationMetric{
				Start: time,
			}
		}
	}
}

func recordStateEnd(info *reconcileInfo, host *metal3v1alpha1.BareMetalHost, state metal3v1alpha1.ProvisioningState, time metav1.Time) (changed bool) {
	if prevMetric := host.OperationMetricForState(state); prevMetric != nil {
		if !prevMetric.Start.IsZero() && prevMetric.End.IsZero() {
			prevMetric.End = time
			info.postSaveCallbacks = append(info.postSaveCallbacks, func() {
				observer := stateTime[state].With(hostMetricLabels(info.request))
				observer.Observe(prevMetric.Duration().Seconds())
			})
			changed = true
		}
	}
	return
}

func (hsm *hostStateMachine) ensureCapacity(info *reconcileInfo, state metal3v1alpha1.ProvisioningState) actionResult {
	hasCapacity, err := hsm.Provisioner.HasCapacity()
	if err != nil {
		return actionError{errors.Wrap(err, "failed to determine current provisioner capacity")}
	}

	if !hasCapacity {
		return recordActionDelayed(info, state)
	}

	return nil
}

func (hsm *hostStateMachine) updateHostStateFrom(initialState metal3v1alpha1.ProvisioningState,
	info *reconcileInfo) actionResult {
	if hsm.NextState != initialState {

		// Check if there is a free slot available when trying to
		// (de)provision an host - if not the action will be delayed.
		// The check is limited to only the (de)provisioning states to
		// avoid putting an excessive pressure on the provisioner
		switch hsm.NextState {
		case metal3v1alpha1.StateInspecting, metal3v1alpha1.StateProvisioning,
			metal3v1alpha1.StateDeprovisioning, metal3v1alpha1.StateDeleting:
			if actionRes := hsm.ensureCapacity(info, hsm.NextState); actionRes != nil {
				return actionRes
			}
		}

		info.log.Info("changing provisioning state",
			"old", initialState,
			"new", hsm.NextState)
		now := metav1.Now()
		recordStateEnd(info, hsm.Host, initialState, now)
		recordStateBegin(hsm.Host, hsm.NextState, now)
		info.postSaveCallbacks = append(info.postSaveCallbacks, func() {
			stateChanges.With(stateChangeMetricLabels(initialState, hsm.NextState)).Inc()
		})
		hsm.Host.Status.Provisioning.State = hsm.NextState
		// Here we assume that if we're being asked to change the
		// state, the return value of ReconcileState (our caller) is
		// set up to ensure the change in the host is written back to
		// the API. That means we can safely update any status fields
		// along with the state.
		switch hsm.NextState {
		case metal3v1alpha1.StateRegistering,
			metal3v1alpha1.StateInspecting,
			metal3v1alpha1.StateProvisioning:
			// TODO: When the user-selectable profile field is
			// removed, move saveHostProvisioningSettings() from the
			// controller to this point. We can't move it yet because
			// it needs error handling logic that we can't support in
			// this function.
			if updateBootModeStatus(hsm.Host) {
				info.log.Info("saving boot mode",
					"new mode", hsm.Host.Status.Provisioning.BootMode)
			}
		}
	}

	return nil
}

func (hsm *hostStateMachine) checkDelayedHost(info *reconcileInfo) actionResult {

	// Check if there's a free slot for hosts that have been previously delayed
	if info.host.Status.OperationalStatus == metal3v1alpha1.OperationalStatusDelayed {
		if actionRes := hsm.ensureCapacity(info, info.host.Status.Provisioning.State); actionRes != nil {
			return actionRes
		}

		// A slot is available, let's cleanup the status and retry
		clearError(info.host)
		return actionUpdate{}
	}

	// Make sure the check is re-applied when provisioning an
	// host not yet tracked by the provisioner
	switch info.host.Status.Provisioning.State {
	case metal3v1alpha1.StateInspecting, metal3v1alpha1.StateProvisioning,
		metal3v1alpha1.StateDeprovisioning, metal3v1alpha1.StateDeleting:
		if actionRes := hsm.ensureCapacity(info, info.host.Status.Provisioning.State); actionRes != nil {
			return actionRes
		}
	}

	return nil
}

func (hsm *hostStateMachine) ReconcileState(info *reconcileInfo) (actionRes actionResult) {
	initialState := hsm.Host.Status.Provisioning.State

	defer func() {
		if overrideAction := hsm.updateHostStateFrom(initialState, info); overrideAction != nil {
			actionRes = overrideAction
		}
	}()

	if delayedResult := hsm.checkDelayedHost(info); delayedResult != nil {
		return delayedResult
	}

	if hsm.checkInitiateDelete() {
		info.log.Info("Initiating host deletion")
		return actionComplete{}
	}

	if detachedResult := hsm.checkDetachedHost(info); detachedResult != nil {
		return detachedResult
	}

	if registerResult := hsm.ensureRegistered(info); registerResult != nil {
		hostRegistrationRequired.Inc()
		return registerResult
	}

	if stateHandler, found := hsm.handlers()[initialState]; found {
		return stateHandler(info)
	}

	info.log.Info("No handler found for state", "state", initialState)
	return actionError{fmt.Errorf("No handler found for state \"%s\"", initialState)}
}

func updateBootModeStatus(host *metal3v1alpha1.BareMetalHost) bool {
	// Make sure we have saved the current boot mode value.
	bootMode := host.BootMode()
	if bootMode == host.Status.Provisioning.BootMode {
		return false
	}
	host.Status.Provisioning.BootMode = bootMode
	return true
}

func (hsm *hostStateMachine) checkInitiateDelete() bool {
	if hsm.Host.DeletionTimestamp.IsZero() {
		// Delete not requested
		return false
	}

	switch hsm.NextState {
	default:
		hsm.NextState = metal3v1alpha1.StateDeleting
	case metal3v1alpha1.StateProvisioning, metal3v1alpha1.StateProvisioned:
		if hsm.Host.OperationalStatus() == metal3v1alpha1.OperationalStatusDetached {
			hsm.NextState = metal3v1alpha1.StateDeleting
		} else {
			hsm.NextState = metal3v1alpha1.StateDeprovisioning
		}
	case metal3v1alpha1.StateDeprovisioning:
		// Allow state machine to run to continue deprovisioning.
		return false
	case metal3v1alpha1.StateDeleting:
		// Already in deleting state. Allow state machine to run.
		return false
	}
	return true
}

// hasInspectAnnotation checks for existence of baremetalhost.metal3.io/detached
func hasDetachedAnnotation(host *metal3v1alpha1.BareMetalHost) bool {
	annotations := host.GetAnnotations()
	if annotations != nil {
		if _, ok := annotations[metal3v1alpha1.DetachedAnnotation]; ok {
			return true
		}
	}
	return false
}

func (hsm *hostStateMachine) checkDetachedHost(info *reconcileInfo) (result actionResult) {
	// If the detached annotation is set we remove any host from the
	// provisioner and take no further action
	// Note this doesn't change the current state, only the OperationalStatus
	if hasDetachedAnnotation(hsm.Host) {
		// Only allow detaching hosts in Provisioned/ExternallyProvisioned states
		switch info.host.Status.Provisioning.State {
		case metal3v1alpha1.StateProvisioned, metal3v1alpha1.StateExternallyProvisioned:
			return hsm.Reconciler.detachHost(hsm.Provisioner, info)
		}
	}
	if info.host.Status.ErrorType == metal3v1alpha1.DetachError {
		clearError(info.host)
		hsm.Host.Status.ErrorCount = 0
		info.log.Info("removed detach error")
		return actionUpdate{}
	}
	if info.host.OperationalStatus() == metal3v1alpha1.OperationalStatusDetached {
		newStatus := metal3v1alpha1.OperationalStatusOK
		if info.host.Status.ErrorType != "" {
			newStatus = metal3v1alpha1.OperationalStatusError
		}
		info.host.SetOperationalStatus(newStatus)
		info.log.Info("removed detached status")
		return actionUpdate{}
	}
	return nil
}

func (hsm *hostStateMachine) ensureRegistered(info *reconcileInfo) (result actionResult) {
	if !hsm.haveCreds {
		// If we are in the process of deletion (which may start with
		// deprovisioning) and we have been unable to obtain any credentials,
		// don't attempt to re-register the Host as this will always fail.
		return
	}

	switch hsm.NextState {
	case metal3v1alpha1.StateNone, metal3v1alpha1.StateUnmanaged:
		// We haven't yet reached the Registration state, so don't attempt
		// to register the Host.
		return
	case metal3v1alpha1.StateMatchProfile:
		// We don't call the provisioner in this state, so there is no point
		// in checking the registration.
		return
	case metal3v1alpha1.StateDeleting:
		// In the deleting state the whole idea is to de-register the host
		return
	case metal3v1alpha1.StateRegistering:
	default:
		if hsm.Host.Status.ErrorType == metal3v1alpha1.RegistrationError ||
			!hsm.Host.Status.GoodCredentials.Match(*info.bmcCredsSecret) {
			info.log.Info("Retrying registration")
			recordStateBegin(hsm.Host, metal3v1alpha1.StateRegistering, metav1.Now())
		}
	}

	result = hsm.Reconciler.registerHost(hsm.Provisioner, info)
	_, complete := result.(actionComplete)
	if (result == nil || complete) &&
		hsm.NextState != metal3v1alpha1.StateRegistering {
		if recordStateEnd(info, hsm.Host, metal3v1alpha1.StateRegistering, metav1.Now()) {
			result = actionUpdate{}
		}
	}
	if complete {
		result = actionUpdate{}
	}
	return
}

func (hsm *hostStateMachine) handleNone(info *reconcileInfo) actionResult {
	// No state is set, so immediately move to either Registering or Unmanaged
	if hsm.Host.HasBMCDetails() {
		hsm.NextState = metal3v1alpha1.StateRegistering
	} else {
		info.publishEvent("Discovered", "Discovered host with no BMC details")
		hsm.Host.SetOperationalStatus(metal3v1alpha1.OperationalStatusDiscovered)
		hsm.NextState = metal3v1alpha1.StateUnmanaged
		hostUnmanaged.Inc()
	}
	return actionComplete{}
}

func (hsm *hostStateMachine) handleUnmanaged(info *reconcileInfo) actionResult {
	actResult := hsm.Reconciler.actionUnmanaged(hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = metal3v1alpha1.StateRegistering
	}
	return actResult
}

func (hsm *hostStateMachine) handleRegistering(info *reconcileInfo) actionResult {
	// Getting to the state handler at all means we have successfully
	// registered using the current BMC credentials, so we can move to the
	// next state. We will not return to the Registering state, even
	// if the credentials change and the Host must be re-registered.
	if hsm.Host.Spec.ExternallyProvisioned {
		hsm.NextState = metal3v1alpha1.StateExternallyProvisioned
	} else {
		hsm.NextState = metal3v1alpha1.StateInspecting
	}
	hsm.Host.Status.ErrorCount = 0
	return actionComplete{}
}

func (hsm *hostStateMachine) handleInspecting(info *reconcileInfo) actionResult {
	actResult := hsm.Reconciler.actionInspecting(hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = metal3v1alpha1.StateMatchProfile
		hsm.Host.Status.ErrorCount = 0
	}
	return actResult
}

func (hsm *hostStateMachine) handleMatchProfile(info *reconcileInfo) actionResult {
	actResult := hsm.Reconciler.actionMatchProfile(hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = metal3v1alpha1.StatePreparing
		hsm.Host.Status.ErrorCount = 0
	}
	return actResult
}

func (hsm *hostStateMachine) handleExternallyProvisioned(info *reconcileInfo) actionResult {
	if hsm.Host.Spec.ExternallyProvisioned {
		// ErrorCount is cleared when appropriate inside actionManageSteadyState
		return hsm.Reconciler.actionManageSteadyState(hsm.Provisioner, info)
	}

	switch {
	case hsm.Host.NeedsHardwareInspection():
		hsm.NextState = metal3v1alpha1.StateInspecting
	case hsm.Host.NeedsHardwareProfile():
		hsm.NextState = metal3v1alpha1.StateMatchProfile
	default:
		hsm.NextState = metal3v1alpha1.StatePreparing
	}
	return actionComplete{}
}

func (hsm *hostStateMachine) handlePreparing(info *reconcileInfo) actionResult {
	actResult := hsm.Reconciler.actionPreparing(hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.Host.Status.ErrorCount = 0
		hsm.NextState = metal3v1alpha1.StateReady
	}
	return actResult
}

func (hsm *hostStateMachine) handleReady(info *reconcileInfo) actionResult {
	if hsm.Host.Spec.ExternallyProvisioned {
		hsm.NextState = metal3v1alpha1.StateExternallyProvisioned
		clearPreparationSettings(info.host)
		return actionComplete{}
	}

	if hasInspectAnnotation(hsm.Host) {
		hsm.NextState = metal3v1alpha1.StateInspecting
		return actionComplete{}
	}

	if dirty, _, err := getPreparationSettings(info.host); err != nil {
		return actionError{err}
	} else if dirty {
		hsm.NextState = metal3v1alpha1.StatePreparing
		return actionComplete{}
	}

	// ErrorCount is cleared when appropriate inside actionManageReady
	actResult := hsm.Reconciler.actionManageReady(hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = metal3v1alpha1.StateProvisioning
	}
	return actResult
}

func (hsm *hostStateMachine) provisioningCancelled() bool {
	if hsm.Host.Spec.Image == nil {
		return true
	}
	if hsm.Host.Spec.Image.URL == "" {
		return true
	}
	if hsm.Host.Status.Provisioning.Image.URL == "" {
		return false
	}
	if hsm.Host.Spec.Image.URL != hsm.Host.Status.Provisioning.Image.URL {
		return true
	}
	return false
}

func (hsm *hostStateMachine) handleProvisioning(info *reconcileInfo) actionResult {
	if hsm.Host.Status.ErrorType != "" || hsm.provisioningCancelled() {
		hsm.NextState = metal3v1alpha1.StateDeprovisioning
		return actionComplete{}
	}

	actResult := hsm.Reconciler.actionProvisioning(hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = metal3v1alpha1.StateProvisioned
		hsm.Host.Status.ErrorCount = 0
	}
	return actResult
}

func (hsm *hostStateMachine) handleProvisioned(info *reconcileInfo) actionResult {
	if hsm.provisioningCancelled() {
		hsm.NextState = metal3v1alpha1.StateDeprovisioning
		return actionComplete{}
	}

	// ErrorCount is cleared when appropriate inside actionManageSteadyState
	return hsm.Reconciler.actionManageSteadyState(hsm.Provisioner, info)
}

func (hsm *hostStateMachine) handleDeprovisioning(info *reconcileInfo) actionResult {
	actResult := hsm.Reconciler.actionDeprovisioning(hsm.Provisioner, info)

	if hsm.Host.DeletionTimestamp.IsZero() {
		if _, complete := actResult.(actionComplete); complete {
			hsm.NextState = metal3v1alpha1.StateReady
			hsm.Host.Status.ErrorCount = 0
		}
	} else {
		skipToDelete := func() actionResult {
			hsm.NextState = metal3v1alpha1.StateDeleting
			info.postSaveCallbacks = append(info.postSaveCallbacks, deleteWithoutDeprov.Inc)
			return actionComplete{}
		}

		switch r := actResult.(type) {
		case actionComplete:
			hsm.NextState = metal3v1alpha1.StateDeleting
			hsm.Host.Status.ErrorCount = 0
		case actionFailed:
			// If the provisioner gives up deprovisioning and
			// deletion has been requested, continue to delete.
			if hsm.Host.Status.ErrorCount > 3 {
				return skipToDelete()
			}
		case actionError:
			if r.NeedsRegistration() && !hsm.haveCreds {
				// If the host is not registered as a node in Ironic and we
				// lack the credentials to deprovision it, just continue to
				// delete.
				return skipToDelete()
			}
		}
	}
	return actResult
}

func (hsm *hostStateMachine) handleDeleting(info *reconcileInfo) actionResult {
	return hsm.Reconciler.actionDeleting(hsm.Provisioner, info)
}
