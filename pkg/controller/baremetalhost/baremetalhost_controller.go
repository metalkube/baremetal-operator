package baremetalhost

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/pkg/apis/metal3/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/bmc"
	"github.com/metal3-io/baremetal-operator/pkg/hardware"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/demo"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/fixture"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic"
	"github.com/metal3-io/baremetal-operator/pkg/utils"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	hostErrorRetryDelay = time.Second * 10
	pauseRetryDelay     = time.Second * 30
)

var runInTestMode bool
var runInDemoMode bool
var maxConcurrentReconciles int = 3

func init() {
	flag.BoolVar(&runInTestMode, "test-mode", false, "disable ironic communication")
	flag.BoolVar(&runInDemoMode, "demo-mode", false,
		"use the demo provisioner to set host states")

	if mcrEnv, ok := os.LookupEnv("BMO_CONCURRENCY"); ok {
		mcr, err := strconv.Atoi(mcrEnv)
		if err != nil {
			log.Error(err, fmt.Sprintf("BMO_CONCURRENCY value: %s is invalid", mcrEnv))
			os.Exit(1)
		}
		if mcr > 0 {
			log.Info(fmt.Sprintf("BMO_CONCURRENCY of %d is set via an environment variable", mcr))
			maxConcurrentReconciles = mcr
		} else {
			log.Info(fmt.Sprintf("Invalid BMO_CONCURRENCY value. Operator Concurrency will be set to a default value of %d", maxConcurrentReconciles))
		}
	} else {
		log.Info(fmt.Sprintf("Operator Concurrency will be set to a default value of %d", maxConcurrentReconciles))
	}
}

var log = logf.Log.WithName("baremetalhost")

// Add creates a new BareMetalHost Controller and adds it to the
// Manager. The Manager will set fields on the Controller and Start it
// when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	var provisionerFactory provisioner.Factory
	switch {
	case runInTestMode:
		log.Info("USING TEST MODE")
		provisionerFactory = fixture.New
	case runInDemoMode:
		log.Info("USING DEMO MODE")
		provisionerFactory = demo.New
	default:
		provisionerFactory = ironic.New
		ironic.LogStartup()
	}
	return &ReconcileBareMetalHost{
		client:             mgr.GetClient(),
		scheme:             mgr.GetScheme(),
		provisionerFactory: provisionerFactory,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("metal3-baremetalhost-controller", mgr,
		controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles,
			Reconciler: r,
		})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource BareMetalHost
	err = c.Watch(&source.Kind{Type: &metal3v1alpha1.BareMetalHost{}},
		&handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secrets being used by hosts
	err = c.Watch(&source.Kind{Type: &corev1.Secret{}},
		&handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &metal3v1alpha1.BareMetalHost{},
		})
	return err
}

var _ reconcile.Reconciler = &ReconcileBareMetalHost{}

// ReconcileBareMetalHost reconciles a BareMetalHost object
type ReconcileBareMetalHost struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client             client.Client
	scheme             *runtime.Scheme
	provisionerFactory provisioner.Factory
}

// Instead of passing a zillion arguments to the action of a phase,
// hold them in a context
type reconcileInfo struct {
	log               logr.Logger
	host              *metal3v1alpha1.BareMetalHost
	request           reconcile.Request
	bmcCredsSecret    *corev1.Secret
	events            []corev1.Event
	errorMessage      string
	postSaveCallbacks []func()
}

// match the provisioner.EventPublisher interface
func (info *reconcileInfo) publishEvent(reason, message string) {
	info.events = append(info.events, info.host.NewEvent(reason, message))
}

// Reconcile reads that state of the cluster for a BareMetalHost
// object and makes changes based on the state read and what is in the
// BareMetalHost.Spec TODO(user): Modify this Reconcile function to
// implement your Controller logic.  This example creates a Pod as an
// example Note: The Controller will requeue the Request to be
// processed again if the returned error is non-nil or Result.Requeue
// is true, otherwise upon completion it will remove the work from the
// queue.
func (r *ReconcileBareMetalHost) Reconcile(request reconcile.Request) (result reconcile.Result, err error) {
	reconcileCounters.With(hostMetricLabels(request)).Inc()
	defer func() {
		if err != nil {
			reconcileErrorCounter.Inc()
		}
	}()

	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling BareMetalHost")

	// Fetch the BareMetalHost
	host := &metal3v1alpha1.BareMetalHost{}
	err = r.client.Get(context.TODO(), request.NamespacedName, host)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after
			// reconcile request.  Owned objects are automatically
			// garbage collected. For additional cleanup logic use
			// finalizers.  Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, errors.Wrap(err, "could not load host data")
	}

	// If the reconciliation is paused, requeue
	annotations := host.GetAnnotations()
	if annotations != nil {
		if _, ok := annotations[metal3v1alpha1.PausedAnnotation]; ok {
			return reconcile.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
		}
	}

	// NOTE(dhellmann): Handle a few steps outside of the phase
	// structure because they require extra data lookup (like the
	// credential checks) or have to be done "first" (like delete
	// handling) to avoid looping.

	// Add a finalizer to newly created objects.
	if host.DeletionTimestamp.IsZero() && !hostHasFinalizer(host) {
		reqLogger.Info(
			"adding finalizer",
			"existingFinalizers", host.Finalizers,
			"newValue", metal3v1alpha1.BareMetalHostFinalizer,
		)
		host.Finalizers = append(host.Finalizers,
			metal3v1alpha1.BareMetalHostFinalizer)
		err := r.client.Update(context.TODO(), host)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to add finalizer")
		}
		return reconcile.Result{Requeue: true}, nil
	}

	// Retrieve the BMC details from the host spec and validate host
	// BMC details and build the credentials for talking to the
	// management controller.
	bmcCreds, bmcCredsSecret, err := r.buildAndValidateBMCCredentials(request, host)
	if err != nil || bmcCreds == nil {
		if !host.DeletionTimestamp.IsZero() {
			// If we are in the process of deletion, try with empty credentials
			bmcCreds = &bmc.Credentials{}
			bmcCredsSecret = &corev1.Secret{}
		} else {
			return r.credentialsErrorResult(err, request, host)
		}
	}

	initialState := host.Status.Provisioning.State
	info := &reconcileInfo{
		log:            reqLogger.WithValues("provisioningState", initialState),
		host:           host,
		request:        request,
		bmcCredsSecret: bmcCredsSecret,
	}
	prov, err := r.provisionerFactory(host, *bmcCreds, info.publishEvent)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to create provisioner")
	}

	stateMachine := newHostStateMachine(host, r, prov)
	actResult := stateMachine.ReconcileState(info)
	result, err = actResult.Result()

	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("action %q failed", initialState))
		return
	}

	// Only save status when we're told to, otherwise we
	// introduce an infinite loop reconciling the same object over and
	// over when there is an unrecoverable error (tracked through the
	// error state of the host).
	if actResult.Dirty() {
		info.log.Info("saving host status",
			"operational status", host.OperationalStatus(),
			"provisioning state", host.Status.Provisioning.State)
		if err = r.saveStatus(host); err != nil {
			return reconcile.Result{}, errors.Wrap(err,
				fmt.Sprintf("failed to save host status after %q", initialState))
		}

		for _, cb := range info.postSaveCallbacks {
			cb()
		}
	}

	for _, e := range info.events {
		r.publishEvent(request, e)
	}

	logResult(info, result)
	return
}

func logResult(info *reconcileInfo, result reconcile.Result) {
	if result.Requeue || result.RequeueAfter != 0 ||
		!utils.StringInList(info.host.Finalizers,
			metal3v1alpha1.BareMetalHostFinalizer) {
		info.log.Info("done",
			"requeue", result.Requeue,
			"after", result.RequeueAfter)
	} else {
		info.log.Info("stopping on host error",
			"message", info.host.Status.ErrorMessage)
	}
}

func recordActionFailure(info *reconcileInfo, errorType metal3v1alpha1.ErrorType, errorMessage string) actionFailed {
	dirty := info.host.SetErrorMessage(errorType, errorMessage)
	if dirty {
		eventType := map[metal3v1alpha1.ErrorType]string{
			metal3v1alpha1.RegistrationError:    "RegistrationError",
			metal3v1alpha1.InspectionError:      "InspectionError",
			metal3v1alpha1.ProvisioningError:    "ProvisioningError",
			metal3v1alpha1.PowerManagementError: "PowerManagementError",
		}[errorType]

		counter := actionFailureCounters.WithLabelValues(eventType)
		info.postSaveCallbacks = append(info.postSaveCallbacks, counter.Inc)

		info.publishEvent(eventType, errorMessage)
	}
	return actionFailed{dirty: dirty, ErrorType: errorType}
}

func (r *ReconcileBareMetalHost) credentialsErrorResult(err error, request reconcile.Request, host *metal3v1alpha1.BareMetalHost) (reconcile.Result, error) {
	switch err.(type) {
	// We treat an empty bmc address and empty bmc credentials fields as a
	// trigger the host needs to be put into a discovered status. We also set
	// an error message (but not an error state) on the host so we can understand
	// what we may be waiting on.  Editing the host to set these values will
	// cause the host to be reconciled again so we do not Requeue.
	case *EmptyBMCAddressError, *EmptyBMCSecretError:
		credentialsInvalid.Inc()
		dirty := host.SetOperationalStatus(metal3v1alpha1.OperationalStatusDiscovered)
		if dirty {
			// Set the host error message directly
			// as we cannot use SetErrorCondition which
			// overwrites our discovered state
			host.Status.ErrorMessage = err.Error()
			host.Status.ErrorType = ""
			saveErr := r.saveStatus(host)
			if saveErr != nil {
				return reconcile.Result{Requeue: true}, saveErr
			}
			// Only publish the event if we do not have an error
			// after saving so that we only publish one time.
			r.publishEvent(request,
				host.NewEvent("Discovered", fmt.Sprintf("Discovered host with unusable BMC details: %s", err.Error())))
		}
		return reconcile.Result{}, nil
	// In the event a credential secret is defined, but we cannot find it
	// we requeue the host as we will not know if they create the secret
	// at some point in the future.
	case *ResolveBMCSecretRefError:
		credentialsMissing.Inc()
		changed, saveErr := r.setErrorCondition(request, host, metal3v1alpha1.RegistrationError, err.Error())
		if saveErr != nil {
			return reconcile.Result{Requeue: true}, saveErr
		}
		if changed {
			// Only publish the event if we do not have an error
			// after saving so that we only publish one time.
			r.publishEvent(request, host.NewEvent("BMCCredentialError", err.Error()))
		}
		return reconcile.Result{Requeue: true, RequeueAfter: hostErrorRetryDelay}, nil
	// If we have found the secret but it is missing the required fields
	// or the BMC address is defined but malformed we set the
	// host into an error state but we do not Requeue it
	// as fixing the secret or the host BMC info will trigger
	// the host to be reconciled again
	case *bmc.CredentialsValidationError, *bmc.UnknownBMCTypeError:
		credentialsInvalid.Inc()
		_, saveErr := r.setErrorCondition(request, host, metal3v1alpha1.RegistrationError, err.Error())
		if saveErr != nil {
			return reconcile.Result{Requeue: true}, saveErr
		}
		// Only publish the event if we do not have an error
		// after saving so that we only publish one time.
		r.publishEvent(request, host.NewEvent("BMCCredentialError", err.Error()))
		return reconcile.Result{}, nil
	default:
		unhandledCredentialsError.Inc()
		return reconcile.Result{}, errors.Wrap(err, "An unhandled failure occurred with the BMC secret")
	}
}

// Manage deletion of the host
func (r *ReconcileBareMetalHost) actionDeleting(prov provisioner.Provisioner, info *reconcileInfo) actionResult {
	info.log.Info(
		"marked to be deleted",
		"timestamp", info.host.DeletionTimestamp,
	)

	// no-op if finalizer has been removed.
	if !utils.StringInList(info.host.Finalizers, metal3v1alpha1.BareMetalHostFinalizer) {
		info.log.Info("ready to be deleted")
		return deleteComplete{}
	}

	provResult, err := prov.Delete()
	if err != nil {
		return actionError{errors.Wrap(err, "failed to delete")}
	}
	if provResult.Dirty {
		err = r.saveStatus(info.host)
		if err != nil {
			return actionError{errors.Wrap(err, "failed to save host after deleting")}
		}
		return actionContinue{provResult.RequeueAfter}
	}

	// Remove finalizer to allow deletion
	info.host.Finalizers = utils.FilterStringFromList(
		info.host.Finalizers, metal3v1alpha1.BareMetalHostFinalizer)
	info.log.Info("cleanup is complete, removed finalizer",
		"remaining", info.host.Finalizers)
	if err := r.client.Update(context.Background(), info.host); err != nil {
		return actionError{errors.Wrap(err, "failed to remove finalizer")}
	}

	return deleteComplete{}
}

// Test the credentials by connecting to the management controller.
func (r *ReconcileBareMetalHost) actionRegistering(prov provisioner.Provisioner, info *reconcileInfo) actionResult {
	info.log.Info("registering and validating access to management controller",
		"credentials", info.host.Status.TriedCredentials)

	credsChanged := !info.host.Status.TriedCredentials.Match(*info.bmcCredsSecret)
	if credsChanged {
		info.log.Info("new credentials")
		info.host.UpdateTriedCredentials(*info.bmcCredsSecret)
		info.postSaveCallbacks = append(info.postSaveCallbacks, updatedCredentials.Inc)
	}

	provResult, err := prov.ValidateManagementAccess(credsChanged)
	if err != nil {
		noManagementAccess.Inc()
		return actionError{errors.Wrap(err, "failed to validate BMC access")}
	}

	info.log.Info("response from validate", "provResult", provResult)

	if provResult.ErrorMessage != "" {
		return recordActionFailure(info, metal3v1alpha1.RegistrationError, provResult.ErrorMessage)
	}

	if provResult.Dirty {
		info.log.Info("host not ready", "wait", provResult.RequeueAfter)
		info.host.ClearError()
		return actionContinue{provResult.RequeueAfter}
	}

	// Reaching this point means the credentials are valid and worked,
	// so clear any previous error and record the success in the
	// status block.
	info.log.Info("updating credentials success status fields")
	info.host.UpdateGoodCredentials(*info.bmcCredsSecret)
	info.log.Info("clearing previous error message")
	info.host.ClearError()

	info.publishEvent("BMCAccessValidated", "Verified access to BMC")

	if info.host.Spec.ExternallyProvisioned {
		info.publishEvent("ExternallyProvisioned",
			"Registered host that was externally provisioned")
	}

	return actionComplete{}
}

// Ensure we have the information about the hardware on the host.
func (r *ReconcileBareMetalHost) actionInspecting(prov provisioner.Provisioner, info *reconcileInfo) actionResult {
	info.log.Info("inspecting hardware")

	provResult, details, err := prov.InspectHardware()
	if err != nil {
		return actionError{errors.Wrap(err, "hardware inspection failed")}
	}

	if provResult.ErrorMessage != "" {
		return recordActionFailure(info, metal3v1alpha1.InspectionError, provResult.ErrorMessage)
	}

	if details != nil {
		info.host.Status.HardwareDetails = details
		return actionComplete{}
	}

	if provResult.Dirty {
		info.host.ClearError()
		return actionContinue{provResult.RequeueAfter}
	}

	return actionFailed{}
}

func (r *ReconcileBareMetalHost) actionMatchProfile(prov provisioner.Provisioner, info *reconcileInfo) actionResult {

	var hardwareProfile string

	info.log.Info("determining hardware profile")

	// Start by looking for an override value from the user
	if info.host.Spec.HardwareProfile != "" {
		info.log.Info("using spec value for profile name",
			"name", info.host.Spec.HardwareProfile)
		hardwareProfile = info.host.Spec.HardwareProfile
		_, err := hardware.GetProfile(hardwareProfile)
		if err != nil {
			info.log.Info("invalid hardware profile", "profile", hardwareProfile)
			return actionError{err}
		}
	}

	// Now do a bit of matching.
	//
	// FIXME(dhellmann): Insert more robust logic to match
	// hardware profiles here.
	if hardwareProfile == "" {
		if strings.HasPrefix(info.host.Spec.BMC.Address, "libvirt") {
			hardwareProfile = "libvirt"
			info.log.Info("determining from BMC address", "name", hardwareProfile)
		}
	}

	// Now default to a value just in case there is no match
	if hardwareProfile == "" {
		hardwareProfile = hardware.DefaultProfileName
		info.log.Info("using the default", "name", hardwareProfile)
	}

	if info.host.SetHardwareProfile(hardwareProfile) {
		info.log.Info("updating hardware profile", "profile", hardwareProfile)
		info.publishEvent("ProfileSet", fmt.Sprintf("Hardware profile set: %s", hardwareProfile))
	}
	info.host.ClearError()
	return actionComplete{}
}

// Start/continue provisioning if we need to.
func (r *ReconcileBareMetalHost) actionProvisioning(prov provisioner.Provisioner, info *reconcileInfo) actionResult {
	hostConf := &hostConfigData{
		host:   info.host,
		log:    info.log,
		client: r.client,
	}
	info.log.Info("provisioning")

	provResult, err := prov.Provision(hostConf)
	if err != nil {
		return actionError{errors.Wrap(err, "failed to provision")}
	}

	if provResult.ErrorMessage != "" {
		info.log.Info("handling provisioning error in controller")
		return recordActionFailure(info, metal3v1alpha1.ProvisioningError, provResult.ErrorMessage)
	}

	if provResult.Dirty {
		// Go back into the queue and wait for the Provision() method
		// to return false, indicating that it has no more work to
		// do.
		info.host.ClearError()
		return actionContinue{provResult.RequeueAfter}
	}

	// If the provisioner had no work, ensure the image settings match.
	if info.host.Status.Provisioning.Image != *(info.host.Spec.Image) {
		info.log.Info("updating deployed image in status")
		info.host.Status.Provisioning.Image = *(info.host.Spec.Image)
	}

	// After provisioning we always requeue to ensure we enter the
	// "provisioned" state and start monitoring power status.
	return actionComplete{}
}

func (r *ReconcileBareMetalHost) actionDeprovisioning(prov provisioner.Provisioner, info *reconcileInfo) actionResult {
	info.log.Info("deprovisioning")

	provResult, err := prov.Deprovision()
	if err != nil {
		return actionError{errors.Wrap(err, "failed to deprovision")}
	}

	if provResult.ErrorMessage != "" {
		return recordActionFailure(info, metal3v1alpha1.ProvisioningError, provResult.ErrorMessage)
	}

	if provResult.Dirty {
		info.host.ClearError()
		return actionContinue{provResult.RequeueAfter}
	}

	// After the provisioner is done, clear the image settings so we
	// transition to the next state.
	info.host.Status.Provisioning.Image = metal3v1alpha1.Image{}

	return actionComplete{}
}

// Check the current power status against the desired power status.
func (r *ReconcileBareMetalHost) manageHostPower(prov provisioner.Provisioner, info *reconcileInfo) actionResult {
	var provResult provisioner.Result

	// Check the current status and save it before trying to update it.
	provResult, err := prov.UpdateHardwareState()
	if err != nil {
		return actionError{errors.Wrap(err, "failed to update the host power status")}
	}

	if provResult.ErrorMessage != "" {
		return recordActionFailure(info, metal3v1alpha1.PowerManagementError, provResult.ErrorMessage)
	}

	if provResult.Dirty {
		info.host.ClearError()
		return actionContinue{provResult.RequeueAfter}
	}

	// Power state needs to be monitored regularly, so if we leave
	// this function without an error we always want to requeue after
	// a delay.
	steadyStateResult := actionContinue{time.Second * 60}
	if info.host.Status.PoweredOn == info.host.Spec.Online {
		return steadyStateResult
	}

	info.log.Info("power state change needed",
		"expected", info.host.Spec.Online,
		"actual", info.host.Status.PoweredOn)

	if info.host.Spec.Online {
		provResult, err = prov.PowerOn()
	} else {
		provResult, err = prov.PowerOff()
	}
	if err != nil {
		return actionError{errors.Wrap(err, "failed to manage power state of host")}
	}

	if provResult.ErrorMessage != "" {
		return recordActionFailure(info, metal3v1alpha1.PowerManagementError, provResult.ErrorMessage)
	}

	if provResult.Dirty {
		info.postSaveCallbacks = append(info.postSaveCallbacks, func() {
			metricLabels := hostMetricLabels(info.request)
			if info.host.Spec.Online {
				metricLabels[labelPowerOnOff] = "on"
			} else {
				metricLabels[labelPowerOnOff] = "off"
			}
			powerChangeAttempts.With(metricLabels).Inc()
		})
		info.host.ClearError()
		return actionContinue{provResult.RequeueAfter}
	}

	// The provisioner did not have to do anything to change the power
	// state and there were no errors, so reflect the new state in the
	// host status field.
	info.host.Status.PoweredOn = info.host.Spec.Online
	return steadyStateResult
}

// A host reaching this action handler should be provisioned or
// externally provisioned -- a state that it will stay in until the
// user takes further action. Both of those states mean that it has
// been registered with the provisioner once, so we use the Adopt()
// API to ensure that is still true. Then we monitor its power status.
func (r *ReconcileBareMetalHost) actionManageSteadyState(prov provisioner.Provisioner, info *reconcileInfo) actionResult {

	provResult, err := prov.Adopt()
	if err != nil {
		return actionError{err}
	}
	if provResult.ErrorMessage != "" {
		return recordActionFailure(info, metal3v1alpha1.RegistrationError, provResult.ErrorMessage)
	}
	if provResult.Dirty {
		info.host.ClearError()
		return actionContinue{provResult.RequeueAfter}
	}

	return r.manageHostPower(prov, info)
}

// A host reaching this action handler should be available -- a state that
// it will stay in until the user takes further action. It has been
// registered with the provisioner once, so we use
// ValidateManagementAccess() to ensure that is still true. We don't
// use Adopt() because we don't want Ironic to treat the host as
// having been provisioned. Then we monitor its power status.
func (r *ReconcileBareMetalHost) actionManageAvailable(prov provisioner.Provisioner, info *reconcileInfo) actionResult {

	// We always pass false for credentialsChanged because if they had
	// changed we would have ended up in actionRegister() instead of
	// here.
	provResult, err := prov.ValidateManagementAccess(false)
	if err != nil {
		return actionError{err}
	}
	if provResult.ErrorMessage != "" {
		return recordActionFailure(info, metal3v1alpha1.RegistrationError, provResult.ErrorMessage)
	}
	if provResult.Dirty {
		info.host.ClearError()
		return actionContinue{provResult.RequeueAfter}
	}

	if info.host.NeedsProvisioning() {
		info.host.ClearError()
		return actionComplete{}
	}
	return r.manageHostPower(prov, info)
}

func (r *ReconcileBareMetalHost) saveStatus(host *metal3v1alpha1.BareMetalHost) error {
	t := metav1.Now()
	host.Status.LastUpdated = &t
	return r.client.Status().Update(context.TODO(), host)
}

func (r *ReconcileBareMetalHost) setErrorCondition(request reconcile.Request, host *metal3v1alpha1.BareMetalHost, errType metal3v1alpha1.ErrorType, message string) (changed bool, err error) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name)

	changed = host.SetErrorMessage(errType, message)
	if changed {
		reqLogger.Info(
			"adding error message",
			"message", message,
		)
		err = r.saveStatus(host)
		if err != nil {
			err = errors.Wrap(err, "failed to update error message")
		}
	}

	return
}

// Retrieve the secret containing the credentials for talking to the BMC.
func (r *ReconcileBareMetalHost) getBMCSecretAndSetOwner(request reconcile.Request, host *metal3v1alpha1.BareMetalHost) (bmcCredsSecret *corev1.Secret, err error) {

	if host.Spec.BMC.CredentialsName == "" {
		return nil, &EmptyBMCSecretError{message: "The BMC secret reference is empty"}
	}
	secretKey := host.CredentialsKey()
	bmcCredsSecret = &corev1.Secret{}
	err = r.client.Get(context.TODO(), secretKey, bmcCredsSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, &ResolveBMCSecretRefError{message: fmt.Sprintf("The BMC secret %s does not exist", secretKey)}
		}
		return nil, err
	}

	// Make sure the secret has the correct owner as soon as we can.
	// This can return an SaveBMCSecretOwnerError
	// which isn't handled causing us to immediately try again
	// which seems fine as we expect this to be a transient failure
	err = r.setBMCCredentialsSecretOwner(request, host, bmcCredsSecret)
	if err != nil {
		return bmcCredsSecret, err
	}

	return bmcCredsSecret, nil
}

// Make sure the credentials for the management controller look
// right and manufacture bmc.Credentials.  This does not actually try
// to use the credentials.
func (r *ReconcileBareMetalHost) buildAndValidateBMCCredentials(request reconcile.Request, host *metal3v1alpha1.BareMetalHost) (bmcCreds *bmc.Credentials, bmcCredsSecret *corev1.Secret, err error) {

	// Retrieve the BMC secret from Kubernetes for this host
	bmcCredsSecret, err = r.getBMCSecretAndSetOwner(request, host)
	if err != nil {
		return nil, nil, err
	}

	// Check for a "discovered" host vs. one that we have all the info for
	// and find empty Address or CredentialsName fields
	if host.Spec.BMC.Address == "" {
		return nil, nil, &EmptyBMCAddressError{message: "Missing BMC connection detail 'Address'"}
	}

	// pass the bmc address to bmc.NewAccessDetails which will do
	// more in-depth checking on the url to ensure it is
	// a valid bmc address, returning a bmc.UnknownBMCTypeError
	// if it is not conformant
	_, err = bmc.NewAccessDetails(host.Spec.BMC.Address, host.Spec.BMC.DisableCertificateVerification)
	if err != nil {
		return nil, nil, err
	}

	bmcCreds = &bmc.Credentials{
		Username: string(bmcCredsSecret.Data["username"]),
		Password: string(bmcCredsSecret.Data["password"]),
	}

	// Verify that the secret contains the expected info.
	err = bmcCreds.Validate()
	if err != nil {
		return nil, bmcCredsSecret, err
	}

	return bmcCreds, bmcCredsSecret, nil
}

func (r *ReconcileBareMetalHost) setBMCCredentialsSecretOwner(request reconcile.Request, host *metal3v1alpha1.BareMetalHost, secret *corev1.Secret) (err error) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name)
	if metav1.IsControlledBy(secret, host) {
		return nil
	}
	reqLogger.Info("updating owner of secret")
	err = controllerutil.SetControllerReference(host, secret, r.scheme)
	if err != nil {
		return &SaveBMCSecretOwnerError{message: fmt.Sprintf("cannot set owner: %q", err.Error())}
	}
	err = r.client.Update(context.TODO(), secret)
	if err != nil {
		return &SaveBMCSecretOwnerError{message: fmt.Sprintf("cannot save owner: %q", err.Error())}
	}
	return nil
}

func (r *ReconcileBareMetalHost) publishEvent(request reconcile.Request, event corev1.Event) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name)
	log.Info("publishing event", "reason", event.Reason, "message", event.Message)
	err := r.client.Create(context.TODO(), &event)
	if err != nil {
		reqLogger.Info("failed to record event, ignoring",
			"reason", event.Reason, "message", event.Message, "error", err)
	}
	return
}

func hostHasFinalizer(host *metal3v1alpha1.BareMetalHost) bool {
	return utils.StringInList(host.Finalizers, metal3v1alpha1.BareMetalHostFinalizer)
}
