package ironic

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/ports"
	"github.com/gophercloud/gophercloud/openstack/baremetalintrospection/v1/introspection"
	"github.com/pkg/errors"
	logz "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/yaml"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/bmc"
	"github.com/metal3-io/baremetal-operator/pkg/hardware"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/clients"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/devicehints"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/hardwaredetails"
)

var (
	log                       = logz.New().WithName("provisioner").WithName("ironic")
	deprovisionRequeueDelay   = time.Second * 10
	provisionRequeueDelay     = time.Second * 10
	powerRequeueDelay         = time.Second * 10
	introspectionRequeueDelay = time.Second * 15
	softPowerOffTimeout       = time.Second * 180
	deployKernelURL           string
	deployRamdiskURL          string
	ironicEndpoint            string
	inspectorEndpoint         string
	ironicTrustedCAFile       string
	ironicInsecure            bool
	ironicAuth                clients.AuthConfig
	inspectorAuth             clients.AuthConfig
	maxProvisioningHosts      int = 20

	// Keep pointers to ironic and inspector clients configured with
	// the global auth settings to reuse the connection between
	// reconcilers.
	clientIronicSingleton    *gophercloud.ServiceClient
	clientInspectorSingleton *gophercloud.ServiceClient
)

const (
	// See nodes.Node.PowerState for details
	powerOn      = "power on"
	powerOff     = "power off"
	softPowerOff = "soft power off"
	powerNone    = "None"
)

var bootModeCapabilities = map[metal3v1alpha1.BootMode]string{
	metal3v1alpha1.UEFI:           "boot_mode:uefi",
	metal3v1alpha1.UEFISecureBoot: "boot_mode:uefi,secure_boot:true",
	metal3v1alpha1.Legacy:         "boot_mode:bios",
}

type macAddressConflictError struct {
	Address      string
	ExistingNode string
}

func (e macAddressConflictError) Error() string {
	return fmt.Sprintf("MAC address %s conflicts with existing node %s", e.Address, e.ExistingNode)
}

// NewMacAddressConflictError is a wrap for macAddressConflictError error
func NewMacAddressConflictError(address, node string) error {
	return macAddressConflictError{Address: address, ExistingNode: node}
}

func init() {
	// NOTE(dhellmann): Use Fprintf() to report errors instead of
	// logging, because logging is not configured yet in init().

	var authErr error
	ironicAuth, inspectorAuth, authErr = clients.LoadAuth()
	if authErr != nil {
		fmt.Fprintf(os.Stderr, "Cannot start: %s\n", authErr)
		os.Exit(1)
	}

	deployKernelURL = os.Getenv("DEPLOY_KERNEL_URL")
	if deployKernelURL == "" {
		fmt.Fprintf(os.Stderr, "Cannot start: No DEPLOY_KERNEL_URL variable set\n")
		os.Exit(1)
	}
	deployRamdiskURL = os.Getenv("DEPLOY_RAMDISK_URL")
	if deployRamdiskURL == "" {
		fmt.Fprintf(os.Stderr, "Cannot start: No DEPLOY_RAMDISK_URL variable set\n")
		os.Exit(1)
	}
	ironicEndpoint = os.Getenv("IRONIC_ENDPOINT")
	if ironicEndpoint == "" {
		fmt.Fprintf(os.Stderr, "Cannot start: No IRONIC_ENDPOINT variable set\n")
		os.Exit(1)
	}
	inspectorEndpoint = os.Getenv("IRONIC_INSPECTOR_ENDPOINT")
	if inspectorEndpoint == "" {
		fmt.Fprintf(os.Stderr, "Cannot start: No IRONIC_INSPECTOR_ENDPOINT variable set\n")
		os.Exit(1)
	}
	ironicTrustedCAFile = os.Getenv("IRONIC_CACERT_FILE")
	if ironicTrustedCAFile == "" {
		ironicTrustedCAFile = "/opt/metal3/certs/ca/crt"
	}
	ironicInsecureStr := os.Getenv("IRONIC_INSECURE")
	if strings.ToLower(ironicInsecureStr) == "true" {
		ironicInsecure = true
	}

	if maxHostsStr := os.Getenv("PROVISIONING_LIMIT"); maxHostsStr != "" {
		value, err := strconv.Atoi(maxHostsStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot start: Invalid value set for variable PROVISIONING_LIMIT=%s", maxHostsStr)
			os.Exit(1)
		}
		maxProvisioningHosts = value
	}
}

// Provisioner implements the provisioning.Provisioner interface
// and uses Ironic to manage the host.
type ironicProvisioner struct {
	// the host to be managed by this provisioner
	host metal3v1alpha1.BareMetalHost
	// a shorter path to the provisioning status data structure
	status *metal3v1alpha1.ProvisionStatus
	// access parameters for the BMC
	bmcAccess bmc.AccessDetails
	// credentials to log in to the BMC
	bmcCreds bmc.Credentials
	// a client for talking to ironic
	client *gophercloud.ServiceClient
	// a client for talking to ironic-inspector
	inspector *gophercloud.ServiceClient
	// a logger configured for this host
	log logr.Logger
	// a debug logger configured for this host
	debugLog logr.Logger
	// an event publisher for recording significant events
	publisher provisioner.EventPublisher
}

// LogStartup produces useful logging information that we only want to
// emit once on startup but that is interal to this package.
func LogStartup() {
	log.Info("ironic settings",
		"endpoint", ironicEndpoint,
		"ironicAuthType", ironicAuth.Type,
		"inspectorEndpoint", inspectorEndpoint,
		"inspectorAuthType", inspectorAuth.Type,
		"deployKernelURL", deployKernelURL,
		"deployRamdiskURL", deployRamdiskURL,
	)
}

// A private function to construct an ironicProvisioner (rather than a
// Provisioner interface) in a consistent way for tests.
func newProvisionerWithSettings(host metal3v1alpha1.BareMetalHost, bmcCreds bmc.Credentials, publisher provisioner.EventPublisher, ironicURL string, ironicAuthSettings clients.AuthConfig, inspectorURL string, inspectorAuthSettings clients.AuthConfig) (*ironicProvisioner, error) {
	tlsConf := clients.TLSConfig{
		TrustedCAFile:      ironicTrustedCAFile,
		InsecureSkipVerify: ironicInsecure,
	}
	clientIronic, err := clients.IronicClient(ironicURL, ironicAuthSettings, tlsConf)
	if err != nil {
		return nil, err
	}

	clientInspector, err := clients.InspectorClient(inspectorURL, inspectorAuthSettings, tlsConf)
	if err != nil {
		return nil, err
	}

	return newProvisionerWithIronicClients(host, bmcCreds, publisher,
		clientIronic, clientInspector)
}

func newProvisionerWithIronicClients(host metal3v1alpha1.BareMetalHost, bmcCreds bmc.Credentials, publisher provisioner.EventPublisher, clientIronic *gophercloud.ServiceClient, clientInspector *gophercloud.ServiceClient) (*ironicProvisioner, error) {

	bmcAccess, err := bmc.NewAccessDetails(host.Spec.BMC.Address, host.Spec.BMC.DisableCertificateVerification)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse BMC address information")
	}

	provisionerLogger := log.WithValues("host", host.Name)

	// Ensure we have a microversion high enough to get the features
	// we need.
	clientIronic.Microversion = "1.56"
	p := &ironicProvisioner{
		host:      host,
		status:    &(host.Status.Provisioning),
		bmcAccess: bmcAccess,
		bmcCreds:  bmcCreds,
		client:    clientIronic,
		inspector: clientInspector,
		log:       provisionerLogger,
		debugLog:  provisionerLogger.V(1),
		publisher: publisher,
	}

	return p, nil
}

// New returns a new Ironic Provisioner using the global configuration
// for finding the Ironic services.
func New(host metal3v1alpha1.BareMetalHost, bmcCreds bmc.Credentials, publisher provisioner.EventPublisher) (provisioner.Provisioner, error) {
	var err error
	if clientIronicSingleton == nil || clientInspectorSingleton == nil {
		tlsConf := clients.TLSConfig{
			TrustedCAFile:      ironicTrustedCAFile,
			InsecureSkipVerify: ironicInsecure,
		}
		clientIronicSingleton, err = clients.IronicClient(
			ironicEndpoint, ironicAuth, tlsConf)
		if err != nil {
			return nil, err
		}

		clientInspectorSingleton, err = clients.InspectorClient(
			inspectorEndpoint, inspectorAuth, tlsConf)
		if err != nil {
			return nil, err
		}
	}
	return newProvisionerWithIronicClients(host, bmcCreds, publisher,
		clientIronicSingleton, clientInspectorSingleton)
}

func (p *ironicProvisioner) validateNode(ironicNode *nodes.Node) (errorMessage string, err error) {
	var validationErrors []string

	p.log.Info("validating node settings in ironic")
	validateResult, err := nodes.Validate(p.client, ironicNode.UUID).Extract()
	if err != nil {
		return "", err // do not wrap error so we can check type in caller
	}
	if !validateResult.Boot.Result {
		validationErrors = append(validationErrors, validateResult.Boot.Reason)
	}
	if !validateResult.Deploy.Result {
		validationErrors = append(validationErrors, validateResult.Deploy.Reason)
	}
	if len(validationErrors) > 0 {
		// We expect to see errors of this nature sometimes, so rather
		// than reporting it as a reconcile error we record the error
		// status on the host and return.
		errorMessage = fmt.Sprintf("host validation error: %s",
			strings.Join(validationErrors, "; "))
		return errorMessage, nil
	}
	return "", nil
}

func (p *ironicProvisioner) listAllPorts(address string) ([]ports.Port, error) {
	var allPorts []ports.Port

	opts := ports.ListOpts{
		Fields: []string{"node_uuid"},
	}

	if address != "" {
		opts.Address = address
	}

	pager := ports.List(p.client, opts)

	if pager.Err != nil {
		return allPorts, pager.Err
	}

	allPages, err := pager.AllPages()

	if err != nil {
		return allPorts, err
	}

	return ports.ExtractPorts(allPages)

}

// Look for an existing registration for the host in Ironic.
func (p *ironicProvisioner) findExistingHost() (ironicNode *nodes.Node, err error) {
	// Try to load the node by UUID
	if p.status.ID != "" {
		// Look for the node to see if it exists (maybe Ironic was
		// restarted)
		ironicNode, err = nodes.Get(p.client, p.status.ID).Extract()
		switch err.(type) {
		case nil:
			p.debugLog.Info("found existing node by ID")
			return ironicNode, nil
		case gophercloud.ErrDefault404:
			// Look by ID failed, trying to lookup by hostname in case it was
			// previously created
		default:
			return nil, errors.Wrap(err,
				fmt.Sprintf("failed to find node by ID %s", p.status.ID))
		}
	}

	// Try to load the node by name
	p.log.Info("looking for existing node by name", "name", p.host.Name)
	ironicNode, err = nodes.Get(p.client, p.host.Name).Extract()
	switch err.(type) {
	case nil:
		p.debugLog.Info("found existing node by name")
		return ironicNode, nil
	case gophercloud.ErrDefault404:
		p.log.Info(
			fmt.Sprintf("node with name %s doesn't exist", p.host.Name))
	default:
		return nil, errors.Wrap(err,
			fmt.Sprintf("failed to find node by name %s", p.host.Name))
	}

	// Try to load the node by port address
	p.log.Info("looking for existing node by MAC", "MAC", p.host.Spec.BootMACAddress)
	allPorts, err := p.listAllPorts(p.host.Spec.BootMACAddress)

	if err != nil {
		p.log.Info("failed to find an existing port with address", "MAC", p.host.Spec.BootMACAddress)
		return nil, nil
	}

	if len(allPorts) > 0 {
		nodeUUID := allPorts[0].NodeUUID
		ironicNode, err = nodes.Get(p.client, nodeUUID).Extract()
		switch err.(type) {
		case nil:
			p.debugLog.Info("found existing node by ID")

			// If the node has a name, this means we didn't find it above.
			if ironicNode.Name != "" {
				return nil, NewMacAddressConflictError(p.host.Spec.BootMACAddress, ironicNode.Name)
			}

			return ironicNode, nil
		case gophercloud.ErrDefault404:
			return nil, errors.Wrap(err,
				fmt.Sprintf("port exists but linked node doesn't %s", nodeUUID))
		default:
			return nil, errors.Wrap(err,
				fmt.Sprintf("port exists but failed to find linked node by ID %s", nodeUUID))
		}
	} else {
		p.log.Info("port with address doesn't exist", "MAC", p.host.Spec.BootMACAddress)
	}

	return nil, nil

}

// ValidateManagementAccess registers the host with the provisioning
// system and tests the connection information for the host to verify
// that the location and credentials work.
//
// FIXME(dhellmann): We should rename this method to describe what it
// actually does.
func (p *ironicProvisioner) ValidateManagementAccess(credentialsChanged, force bool) (result provisioner.Result, provID string, err error) {
	var ironicNode *nodes.Node

	p.debugLog.Info("validating management access")

	ironicNode, err = p.findExistingHost()
	if err != nil {
		switch err.(type) {
		case macAddressConflictError:
			result, err = operationFailed(err.Error())
		default:
			result, err = transientError(errors.Wrap(err, "failed to find existing host"))
		}
		return
	}

	// Some BMC types require a MAC address, so ensure we have one
	// when we need it. If not, place the host in an error state.
	if p.bmcAccess.NeedsMAC() && p.host.Spec.BootMACAddress == "" {
		msg := fmt.Sprintf("BMC driver %s requires a BootMACAddress value", p.bmcAccess.Type())
		p.log.Info(msg)
		result, err = operationFailed(msg)
		return
	}

	driverInfo := p.bmcAccess.DriverInfo(p.bmcCreds)
	// FIXME(dhellmann): We need to get our IP on the
	// provisioning network from somewhere.
	driverInfo["deploy_kernel"] = deployKernelURL
	driverInfo["deploy_ramdisk"] = deployRamdiskURL

	result, err = operationComplete()

	// If we have not found a node yet, we need to create one
	if ironicNode == nil {
		p.log.Info("registering host in ironic")

		if p.host.Spec.BootMode == metal3v1alpha1.UEFISecureBoot && !p.bmcAccess.SupportsSecureBoot() {
			msg := fmt.Sprintf("BMC driver %s does not support secure boot", p.bmcAccess.Type())
			p.log.Info(msg)
			result, err = operationFailed(msg)
			return
		}

		ironicNode, err = nodes.Create(
			p.client,
			nodes.CreateOpts{
				Driver:              p.bmcAccess.Driver(),
				BootInterface:       p.bmcAccess.BootInterface(),
				Name:                p.host.Name,
				DriverInfo:          driverInfo,
				DeployInterface:     p.deployInterface(),
				InspectInterface:    "inspector",
				ManagementInterface: p.bmcAccess.ManagementInterface(),
				PowerInterface:      p.bmcAccess.PowerInterface(),
				RAIDInterface:       p.bmcAccess.RAIDInterface(),
				VendorInterface:     p.bmcAccess.VendorInterface(),
				Properties: map[string]interface{}{
					"capabilities": bootModeCapabilities[p.host.Status.Provisioning.BootMode],
				},
			}).Extract()
		// FIXME(dhellmann): Handle 409 and 503? errors here.
		if err != nil {
			result, err = transientError(errors.Wrap(err, "failed to register host in ironic"))
			return
		}
		p.publisher("Registered", "Registered new host")

		// Store the ID so other methods can assume it is set and so
		// we can find the node again later.
		provID = ironicNode.UUID

		// If we know the MAC, create a port. Otherwise we will have
		// to do this after we run the introspection step.
		if p.host.Spec.BootMACAddress != "" {
			enable := true
			p.log.Info("creating port for node in ironic", "MAC",
				p.host.Spec.BootMACAddress)
			_, err = ports.Create(
				p.client,
				ports.CreateOpts{
					NodeUUID:   ironicNode.UUID,
					Address:    p.host.Spec.BootMACAddress,
					PXEEnabled: &enable,
				}).Extract()
			if err != nil {
				result, err = transientError(errors.Wrap(err, "failed to create port in ironic"))
				return
			}
		}

		// If there is an image to be provisioned, or an image has
		// previously been provisioned, include those details. Either
		// case may mean we are re-adopting a host that was already
		// known but removed/lost because the pod restarted.
		var imageData *metal3v1alpha1.Image
		switch {
		case p.host.Status.Provisioning.Image.URL != "":
			imageData = &p.host.Status.Provisioning.Image
		case p.host.Spec.Image != nil && p.host.Spec.Image.URL != "":
			imageData = p.host.Spec.Image
		}

		if imageData != nil {
			updates, optsErr := p.getImageUpdateOptsForNode(ironicNode, imageData)
			if optsErr != nil {
				result, err = transientError(errors.Wrap(optsErr, "Could not get Image options for node"))
				return
			}
			if len(updates) != 0 {
				_, err = nodes.Update(p.client, ironicNode.UUID, updates).Extract()
				switch err.(type) {
				case nil:
				case gophercloud.ErrDefault409:
					p.log.Info("could not update host settings in ironic, busy")
					result, err = retryAfterDelay(provisionRequeueDelay)
					return
				default:
					result, err = transientError(errors.Wrap(err, "failed to update host settings in ironic"))
					return
				}
			}
		}
	} else {
		// FIXME(dhellmann): At this point we have found an existing
		// node in ironic by looking it up. We need to check its
		// settings against what we have in the host, and change them
		// if there are differences.
		provID = ironicNode.UUID

		if ironicNode.Name == "" {
			updates := nodes.UpdateOpts{
				nodes.UpdateOperation{
					Op:    nodes.ReplaceOp,
					Path:  "/name",
					Value: p.host.Name,
				},
			}
			ironicNode, err = nodes.Update(p.client, ironicNode.UUID, updates).Extract()
			switch err.(type) {
			case nil:
			case gophercloud.ErrDefault409:
				p.log.Info("could not update ironic node name, busy")
				result, err = retryAfterDelay(provisionRequeueDelay)
				return
			default:
				result, err = transientError(errors.Wrap(err, "failed to update ironc node name"))
				return
			}
			p.log.Info("updated ironic node name")

		}

		// Look for the case where we previously enrolled this node
		// and now the credentials have changed.
		if credentialsChanged {
			updates := nodes.UpdateOpts{
				nodes.UpdateOperation{
					Op:    nodes.ReplaceOp,
					Path:  "/driver_info",
					Value: driverInfo,
				},
			}
			ironicNode, err = nodes.Update(p.client, ironicNode.UUID, updates).Extract()
			switch err.(type) {
			case nil:
			case gophercloud.ErrDefault409:
				p.log.Info("could not update host driver settings, busy")
				result, err = retryAfterDelay(provisionRequeueDelay)
				return
			default:
				result, err = transientError(errors.Wrap(err, "failed to update host driver settings"))
				return
			}
			p.log.Info("updated host driver settings")
			// We don't return here because we also have to set the
			// target provision state to manageable, which happens
			// below.
		}
	}

	// ironicNode, err = nodes.Get(p.client, p.status.ID).Extract()
	// if err != nil {
	// 	return result, errors.Wrap(err, "failed to get provisioning state in ironic")
	// }

	p.log.Info("current provision state",
		"lastError", ironicNode.LastError,
		"current", ironicNode.ProvisionState,
		"target", ironicNode.TargetProvisionState,
	)

	// Ensure the node is marked manageable.
	switch nodes.ProvisionState(ironicNode.ProvisionState) {

	case nodes.Enroll:

		// If ironic is reporting an error, stop working on the node.
		if ironicNode.LastError != "" && !(credentialsChanged || force) {
			result, err = operationFailed(ironicNode.LastError)
			return
		}

		if ironicNode.TargetProvisionState == string(nodes.TargetManage) {
			// We have already tried to manage the node and did not
			// get an error, so do nothing and keep trying.
			result, err = operationContinuing(provisionRequeueDelay)
			return
		}

		result, err = p.changeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{Target: nodes.TargetManage},
		)
		return

	case nodes.Verifying:
		// If we're still waiting for the state to change in Ironic,
		// return true to indicate that we're dirty and need to be
		// reconciled again.
		result, err = operationContinuing(provisionRequeueDelay)
		return

	case nodes.Manageable:
		return

	case nodes.Available:
		// The host is fully registered (and probably wasn't cleanly
		// deleted previously)
		return

	case nodes.Active:
		// The host is already running, maybe it's a master?
		p.debugLog.Info("have active host", "image_source", ironicNode.InstanceInfo["image_source"])
		return

	default:
		return
	}
}

func (p *ironicProvisioner) tryChangeNodeProvisionState(ironicNode *nodes.Node, opts nodes.ProvisionStateOpts) (success bool, result provisioner.Result, err error) {
	p.log.Info("changing provisioning state",
		"current", ironicNode.ProvisionState,
		"existing target", ironicNode.TargetProvisionState,
		"new target", opts.Target,
	)

	changeResult := nodes.ChangeProvisionState(p.client, ironicNode.UUID, opts)
	switch changeResult.Err.(type) {
	case nil:
		success = true
	case gophercloud.ErrDefault409:
		p.log.Info("could not change state of host, busy")
		result, err = retryAfterDelay(provisionRequeueDelay)
		return
	default:
		result, err = transientError(errors.Wrap(changeResult.Err,
			fmt.Sprintf("failed to change provisioning state to %q", opts.Target)))
		return
	}

	result, err = operationContinuing(provisionRequeueDelay)
	return
}

func (p *ironicProvisioner) changeNodeProvisionState(ironicNode *nodes.Node, opts nodes.ProvisionStateOpts) (result provisioner.Result, err error) {
	_, result, err = p.tryChangeNodeProvisionState(ironicNode, opts)
	return
}

// InspectHardware updates the HardwareDetails field of the host with
// details of devices discovered on the hardware. It may be called
// multiple times, and should return true for its dirty flag until the
// inspection is completed.
func (p *ironicProvisioner) InspectHardware(force bool) (result provisioner.Result, details *metal3v1alpha1.HardwareDetails, err error) {
	p.log.Info("inspecting hardware", "status", p.host.OperationalStatus())

	ironicNode, err := p.findExistingHost()
	if err != nil {
		result, err = transientError(errors.Wrap(err, "failed to find existing host"))
		return
	}
	if ironicNode == nil {
		result, err = transientError(provisioner.NeedsRegistration)
		return
	}

	status, err := introspection.GetIntrospectionStatus(p.inspector, ironicNode.UUID).Extract()
	if err != nil {
		if _, isNotFound := err.(gophercloud.ErrDefault404); isNotFound {
			switch nodes.ProvisionState(ironicNode.ProvisionState) {
			case nodes.Inspecting, nodes.InspectWait:
				p.log.Info("inspection already started")
				result, err = operationContinuing(introspectionRequeueDelay)
				return
			default:
				if nodes.ProvisionState(ironicNode.ProvisionState) == nodes.InspectFail && !force {
					p.log.Info("starting inspection failed", "error", status.Error)
					if ironicNode.LastError == "" {
						result.ErrorMessage = "Inspection failed"
					} else {
						result.ErrorMessage = ironicNode.LastError
					}
					err = nil
				}
				p.log.Info("updating boot mode before hardware inspection")
				op, value := buildCapabilitiesValue(ironicNode, p.host.Status.Provisioning.BootMode)
				updates := nodes.UpdateOpts{
					nodes.UpdateOperation{
						Op:    op,
						Path:  "/properties/capabilities",
						Value: value,
					},
				}
				_, err = nodes.Update(p.client, ironicNode.UUID, updates).Extract()
				switch err.(type) {
				case nil:
				case gophercloud.ErrDefault409:
					p.log.Info("could not update host settings in ironic, busy")
					result, err = retryAfterDelay(provisionRequeueDelay)
					return
				default:
					result, err = transientError(errors.Wrap(err, "failed to update host boot mode settings in ironic"))
					return
				}

				p.log.Info("starting new hardware inspection")
				var success bool
				success, result, err = p.tryChangeNodeProvisionState(
					ironicNode,
					nodes.ProvisionStateOpts{Target: nodes.TargetInspect},
				)
				if success {
					p.publisher("InspectionStarted", "Hardware inspection started")
				}
				return
			}
		}
		result, err = transientError(errors.Wrap(err, "failed to extract hardware inspection status"))
		return
	}
	if status.Error != "" {
		p.log.Info("inspection failed", "error", status.Error)
		result, err = operationFailed(status.Error)
		return
	}
	if !status.Finished || (nodes.ProvisionState(ironicNode.ProvisionState) == nodes.Inspecting || nodes.ProvisionState(ironicNode.ProvisionState) == nodes.InspectWait) {
		p.log.Info("inspection in progress", "started_at", status.StartedAt)
		result, err = operationContinuing(introspectionRequeueDelay)
		return
	}

	// Introspection is done
	p.log.Info("getting hardware details from inspection")
	introData := introspection.GetIntrospectionData(p.inspector, ironicNode.UUID)
	data, err := introData.Extract()
	if err != nil {
		result, err = transientError(errors.Wrap(err, "failed to retrieve hardware introspection data"))
		return
	}
	p.log.Info("received introspection data", "data", introData.Body)

	details = hardwaredetails.GetHardwareDetails(data)
	p.publisher("InspectionComplete", "Hardware inspection completed")
	result, err = operationComplete()
	return
}

// UpdateHardwareState fetches the latest hardware state of the server
// and updates the HardwareDetails field of the host with details. It
// is expected to do this in the least expensive way possible, such as
// reading from a cache.
func (p *ironicProvisioner) UpdateHardwareState() (hwState provisioner.HardwareState, err error) {
	p.debugLog.Info("updating hardware state")

	ironicNode, err := p.findExistingHost()
	if err != nil {
		err = errors.Wrap(err, "failed to find existing host")
		return
	}
	if ironicNode == nil {
		err = provisioner.NeedsRegistration
		return
	}

	switch ironicNode.PowerState {
	case powerOn, powerOff:
		discoveredVal := ironicNode.PowerState == powerOn
		hwState.PoweredOn = &discoveredVal
	case powerNone:
		p.log.Info("could not determine power state", "value", ironicNode.PowerState)
	default:
		p.log.Info("unknown power state", "value", ironicNode.PowerState)
	}
	return
}

func (p *ironicProvisioner) setLiveIsoUpdateOptsForNode(ironicNode *nodes.Node, imageData *metal3v1alpha1.Image, updates nodes.UpdateOpts) (nodes.UpdateOpts, error) {
	var op nodes.UpdateOp

	if _, ok := ironicNode.InstanceInfo["boot_iso"]; !ok {
		op = nodes.AddOp
		p.log.Info("adding boot_iso")
	} else {
		op = nodes.ReplaceOp
		p.log.Info("updating boot_iso")
	}
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    op,
			Path:  "/instance_info/boot_iso",
			Value: imageData.URL,
		},
	)
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    nodes.ReplaceOp,
			Path:  "/deploy_interface",
			Value: "ramdisk",
		},
	)
	// remove any image_source or checksum options
	removals := []string{
		"image_source", "image_os_hash_value", "image_os_hash_algo", "image_checksum"}
	op = nodes.RemoveOp
	for _, item := range removals {
		if _, ok := ironicNode.InstanceInfo[item]; ok {
			p.log.Info("removing " + item)
			updates = append(
				updates,
				nodes.UpdateOperation{
					Op:   op,
					Path: "/instance_info/" + item,
				},
			)
		}
	}
	return updates, nil
}

func (p *ironicProvisioner) setDirectDeployUpdateOptsForNode(ironicNode *nodes.Node, imageData *metal3v1alpha1.Image, updates nodes.UpdateOpts) (nodes.UpdateOpts, error) {
	checksum, checksumType, ok := imageData.GetChecksum()
	if !ok {
		p.log.Info("image/checksum not found for host")
		return updates, nil
	}

	var op nodes.UpdateOp

	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    nodes.ReplaceOp,
			Path:  "/deploy_interface",
			Value: "direct",
		},
	)
	// Remove any boot_iso field
	if _, ok := ironicNode.InstanceInfo["boot_iso"]; ok {
		p.log.Info("removing boot_iso")
		updates = append(
			updates,
			nodes.UpdateOperation{
				Op:   nodes.RemoveOp,
				Path: "/instance_info/boot_iso",
			},
		)
	}
	// image_source
	if _, ok := ironicNode.InstanceInfo["image_source"]; !ok {
		op = nodes.AddOp
		p.log.Info("adding image_source")
	} else {
		op = nodes.ReplaceOp
		p.log.Info("updating image_source")
	}
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    op,
			Path:  "/instance_info/image_source",
			Value: imageData.URL,
		},
	)

	// image_os_hash_algo
	if _, ok := ironicNode.InstanceInfo["image_os_hash_algo"]; !ok {
		op = nodes.AddOp
		p.log.Info("adding image_os_hash_algo")
	} else {
		op = nodes.ReplaceOp
		p.log.Info("updating image_os_hash_algo")
	}
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    op,
			Path:  "/instance_info/image_os_hash_algo",
			Value: checksumType,
		},
	)

	// image_os_hash_value
	if _, ok := ironicNode.InstanceInfo["image_os_hash_value"]; !ok {
		op = nodes.AddOp
		p.log.Info("adding image_os_hash_value")
	} else {
		op = nodes.ReplaceOp
		p.log.Info("updating image_os_hash_value")
	}
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    op,
			Path:  "/instance_info/image_os_hash_value",
			Value: checksum,
		},
	)

	// image_checksum
	//
	// FIXME: For older versions of ironic that do not have
	// https://review.opendev.org/#/c/711816/ failing to include the
	// 'image_checksum' causes ironic to refuse to provision the
	// image, even if the other hash value parameters are given. We
	// only want to do that for MD5, however, because those versions
	// of ironic only support MD5 checksums.
	if checksumType == string(metal3v1alpha1.MD5) {
		if _, ok := ironicNode.InstanceInfo["image_checksum"]; !ok {
			op = nodes.AddOp
			p.log.Info("adding image_checksum")
		} else {
			op = nodes.ReplaceOp
			p.log.Info("updating image_checksum")
		}
		updates = append(
			updates,
			nodes.UpdateOperation{
				Op:    op,
				Path:  "/instance_info/image_checksum",
				Value: checksum,
			},
		)
	}

	if imageData.DiskFormat != nil {
		updates = append(updates, nodes.UpdateOperation{
			Op:    nodes.AddOp,
			Path:  "/instance_info/image_disk_format",
			Value: *imageData.DiskFormat,
		})
	}

	return updates, nil
}

func (p *ironicProvisioner) getImageUpdateOptsForNode(ironicNode *nodes.Node, imageData *metal3v1alpha1.Image) (updates nodes.UpdateOpts, err error) {
	// instance_uuid
	p.log.Info("setting instance_uuid")
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    nodes.ReplaceOp,
			Path:  "/instance_uuid",
			Value: string(p.host.ObjectMeta.UID),
		},
	)

	// Secure boot is a normal capability that goes into instance_info (we
	// also put it to properties for consistency, although it's not
	// strictly required in our case).

	if p.host.Spec.BootMode == metal3v1alpha1.UEFISecureBoot {
		updates = append(updates, nodes.UpdateOperation{
			Op:   nodes.AddOp,
			Path: "/instance_info/capabilities",
			// Instance info capabilities were invented later and
			// use a normal JSON mapping instead of a custom
			// string value.
			Value: map[string]string{
				"secure_boot": "true",
			},
		})
	} else {
		updates = append(updates, nodes.UpdateOperation{
			Op:    nodes.AddOp,
			Path:  "/instance_info/capabilities",
			Value: map[string]string{},
		})
	}

	// Set live-iso format options
	if imageData.DiskFormat != nil && *imageData.DiskFormat == "live-iso" {
		return p.setLiveIsoUpdateOptsForNode(ironicNode, imageData, updates)
	}

	// Set deploy_interface direct options when not booting a live-iso
	return p.setDirectDeployUpdateOptsForNode(ironicNode, imageData, updates)
}

func (p *ironicProvisioner) getUpdateOptsForNode(ironicNode *nodes.Node) (updates nodes.UpdateOpts, err error) {

	hwProf, err := hardware.GetProfile(p.host.HardwareProfile())

	if err != nil {
		return updates, errors.Wrap(err,
			fmt.Sprintf("Could not start provisioning with bad hardware profile %s",
				p.host.HardwareProfile()))
	}

	imageOpts, err := p.getImageUpdateOptsForNode(ironicNode, p.host.Spec.Image)
	if err != nil {
		return updates, errors.Wrap(err, "Could not get Image options for node")
	}
	updates = append(updates, imageOpts...)

	// root_device
	//
	// FIXME(dhellmann): We need to specify the root device to receive
	// the image. That should come from some combination of inspecting
	// the host to see what is available and the hardware profile to
	// give us instructions.
	var op nodes.UpdateOp
	if _, ok := ironicNode.Properties["root_device"]; !ok {
		op = nodes.AddOp
		p.log.Info("adding root_device")
	} else {
		op = nodes.ReplaceOp
		p.log.Info("updating root_device")
	}

	// hints
	//
	// If the user has provided explicit root device hints, they take
	// precedence. Otherwise use the values from the hardware profile.
	hints := devicehints.MakeHintMap(p.host.Status.Provisioning.RootDeviceHints)
	p.log.Info("using root device", "hints", hints)
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    op,
			Path:  "/properties/root_device",
			Value: hints,
		},
	)

	// cpu_arch
	//
	// FIXME(dhellmann): This should come from inspecting the
	// host.
	if _, ok := ironicNode.Properties["cpu_arch"]; !ok {
		op = nodes.AddOp
		p.log.Info("adding cpu_arch")
	} else {
		op = nodes.ReplaceOp
		p.log.Info("updating cpu_arch")
	}
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    op,
			Path:  "/properties/cpu_arch",
			Value: hwProf.CPUArch,
		},
	)

	// local_gb
	if _, ok := ironicNode.Properties["local_gb"]; !ok {
		op = nodes.AddOp
		p.log.Info("adding local_gb")
	} else {
		op = nodes.ReplaceOp
		p.log.Info("updating local_gb")
	}
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    op,
			Path:  "/properties/local_gb",
			Value: hwProf.LocalGB,
		},
	)

	// boot_mode
	op, value := buildCapabilitiesValue(ironicNode, p.host.Status.Provisioning.BootMode)
	updates = append(
		updates,
		nodes.UpdateOperation{
			Op:    op,
			Path:  "/properties/capabilities",
			Value: value,
		},
	)

	return updates, nil
}

// We can't just replace the capabilities because we need to keep the
// values provided by inspection. We can't replace only the boot_mode
// because the API isn't fine-grained enough for that. So we have to
// look at the existing value and modify it. This function
// encapsulates the logic for building the value and knowing which
// update operation to use with the results.
func buildCapabilitiesValue(ironicNode *nodes.Node, bootMode metal3v1alpha1.BootMode) (op nodes.UpdateOp, value string) {

	capabilities, ok := ironicNode.Properties["capabilities"]
	if !ok {
		// There is no existing capabilities value
		return nodes.AddOp, bootModeCapabilities[bootMode]
	}
	existingCapabilities := capabilities.(string)

	// The capabilities value is set, so we will want to replace it.
	op = nodes.ReplaceOp

	if existingCapabilities == "" {
		// The existing value is empty so we can replace the whole
		// thing.
		value = bootModeCapabilities[bootMode]
		return
	}

	var filteredCapabilities []string
	for _, item := range strings.Split(existingCapabilities, ",") {
		if !strings.HasPrefix(item, "boot_mode:") && !strings.HasPrefix(item, "secure_boot:") {
			filteredCapabilities = append(filteredCapabilities, item)
		}
	}
	filteredCapabilities = append(filteredCapabilities, bootModeCapabilities[bootMode])

	value = strings.Join(filteredCapabilities, ",")
	return
}

func (p *ironicProvisioner) setUpForProvisioning(ironicNode *nodes.Node, hostConf provisioner.HostConfigData) (result provisioner.Result, err error) {

	p.log.Info("starting provisioning", "node properties", ironicNode.Properties)

	updates, err := p.getUpdateOptsForNode(ironicNode)
	if err != nil {
		return transientError(errors.Wrap(err, "failed to update opts for node"))
	}
	_, err = nodes.Update(p.client, ironicNode.UUID, updates).Extract()
	switch err.(type) {
	case nil:
	case gophercloud.ErrDefault409:
		p.log.Info("could not update host settings in ironic, busy")
		return retryAfterDelay(provisionRequeueDelay)
	default:
		return transientError(errors.Wrap(err, "failed to update host settings in ironic"))
	}

	p.log.Info("validating host settings")

	errorMessage, err := p.validateNode(ironicNode)
	switch err.(type) {
	case nil:
	case gophercloud.ErrDefault409:
		p.log.Info("could not validate host during registration, busy")
		return retryAfterDelay(provisionRequeueDelay)
	default:
		return transientError(errors.Wrap(err, "failed to validate host during registration"))
	}
	if errorMessage != "" {
		return operationFailed(errorMessage)
	}

	// If validation is successful we can start moving the host
	// through the states necessary to make it "available".
	p.log.Info("starting provisioning",
		"lastError", ironicNode.LastError,
		"current", ironicNode.ProvisionState,
		"target", ironicNode.TargetProvisionState,
		"deploy step", ironicNode.DeployStep,
	)
	p.publisher("ProvisioningStarted",
		fmt.Sprintf("Image provisioning started for %s", p.host.Spec.Image.URL))
	return
}

func (p *ironicProvisioner) deployInterface() (result string) {
	result = "direct"
	if p.host.Spec.Image != nil && p.host.Spec.Image.DiskFormat != nil && *p.host.Spec.Image.DiskFormat == "live-iso" {
		result = "ramdisk"
	}
	return result
}

// Adopt allows an externally-provisioned server to be adopted by Ironic.
func (p *ironicProvisioner) Adopt(force bool) (result provisioner.Result, err error) {
	var ironicNode *nodes.Node

	if ironicNode, err = p.findExistingHost(); err != nil {
		return transientError(errors.Wrap(err, "could not find host to adpot"))
	}
	if ironicNode == nil {
		return transientError(provisioner.NeedsRegistration)
	}

	switch nodes.ProvisionState(ironicNode.ProvisionState) {
	case nodes.Enroll, nodes.Verifying:
		return transientError(fmt.Errorf("Invalid state for adopt: %s",
			ironicNode.ProvisionState))
	case nodes.Manageable:
		_, hasImageSource := ironicNode.InstanceInfo["image_source"]
		_, hasBootISO := ironicNode.InstanceInfo["boot_iso"]
		if p.status.State == metal3v1alpha1.StateDeprovisioning &&
			!(hasImageSource || hasBootISO) {
			// If we got here after a fresh registration and image data is
			// available, it should have been added to the node during
			// registration. If it isn't present then we got here due to a
			// failed cleaning on deprovision. The node will be cleaned again
			// before the next provisioning, so just allow the controller to
			// continue without adopting.
			p.log.Info("no image info; not adopting", "state", ironicNode.ProvisionState)
			return operationComplete()
		}
		return p.changeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{
				Target: nodes.TargetAdopt,
			},
		)
	case nodes.Adopting:
		return operationContinuing(provisionRequeueDelay)
	case nodes.AdoptFail:
		if force {
			return p.changeNodeProvisionState(
				ironicNode,
				nodes.ProvisionStateOpts{
					Target: nodes.TargetAdopt,
				},
			)
		}
		return operationFailed(fmt.Sprintf("Host adoption failed: %s",
			ironicNode.LastError))
	case nodes.Active:
	default:
	}
	return operationComplete()
}

func (p *ironicProvisioner) ironicHasSameImage(ironicNode *nodes.Node) (sameImage bool) {
	// To make it easier to test if ironic is configured with
	// the same image we are trying to provision to the host.
	if p.host.Spec.Image != nil && p.host.Spec.Image.DiskFormat != nil && *p.host.Spec.Image.DiskFormat == "live-iso" {
		sameImage = (ironicNode.InstanceInfo["boot_iso"] == p.host.Spec.Image.URL)
		p.log.Info("checking image settings",
			"boot_iso", ironicNode.InstanceInfo["boot_iso"],
			"same", sameImage,
			"provisionState", ironicNode.ProvisionState)
	} else {
		checksum, checksumType, _ := p.host.GetImageChecksum()
		sameImage = (ironicNode.InstanceInfo["image_source"] == p.host.Spec.Image.URL &&
			ironicNode.InstanceInfo["image_os_hash_algo"] == checksumType &&
			ironicNode.InstanceInfo["image_os_hash_value"] == checksum)
		p.log.Info("checking image settings",
			"source", ironicNode.InstanceInfo["image_source"],
			"image_os_hash_algo", checksumType,
			"image_os_has_value", checksum,
			"same", sameImage,
			"provisionState", ironicNode.ProvisionState)
	}
	return sameImage
}

func (p *ironicProvisioner) buildManualCleaningSteps() (cleanSteps []nodes.CleanStep, err error) {
	// Build raid clean steps
	if p.bmcAccess.RAIDInterface() != "no-raid" {
		cleanSteps = append(cleanSteps, BuildRAIDCleanSteps(p.host.Status.Provisioning.RAID)...)
	} else if p.host.Status.Provisioning.RAID != nil {
		return nil, fmt.Errorf("RAID settings are defined, but the node's driver %s does not support RAID", p.bmcAccess.Driver())
	}

	// TODO: Add manual cleaning steps for host configuration

	return
}

func (p *ironicProvisioner) startManualCleaning(ironicNode *nodes.Node) (success bool, result provisioner.Result, err error) {
	if p.bmcAccess.RAIDInterface() != "no-raid" {
		// Set raid configuration
		err = setTargetRAIDCfg(p, ironicNode)
		if err != nil {
			result, err = transientError(err)
			return
		}
	}

	// Build manual clean steps
	cleanSteps, err := p.buildManualCleaningSteps()
	if err != nil {
		result, err = operationFailed(err.Error())
		return
	}

	// Start manual clean
	if len(cleanSteps) != 0 {
		p.log.Info("remove existing configuration and set new configuration", "steps", cleanSteps)
		return p.tryChangeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{
				Target:     nodes.TargetClean,
				CleanSteps: cleanSteps,
			},
		)
	}
	result, err = operationComplete()
	return
}

// Prepare remove existing configuration and set new configuration.
// If `started` is true,  it means that we successfully executed `tryChangeNodeProvisionState`.
func (p *ironicProvisioner) Prepare(unprepared bool) (result provisioner.Result, started bool, err error) {
	var ironicNode *nodes.Node

	if ironicNode, err = p.findExistingHost(); err != nil {
		result, err = transientError(errors.Wrap(err, "could not find host to clean"))
		return
	}
	if ironicNode == nil {
		result, err = transientError(provisioner.NeedsRegistration)
		return
	}

	switch nodes.ProvisionState(ironicNode.ProvisionState) {
	case nodes.Available:
		var cleanSteps []nodes.CleanStep
		cleanSteps, err = p.buildManualCleaningSteps()
		if err != nil {
			result, err = operationFailed(err.Error())
			return
		}
		if unprepared && len(cleanSteps) != 0 {
			result, err = p.changeNodeProvisionState(
				ironicNode,
				nodes.ProvisionStateOpts{Target: nodes.TargetManage},
			)
			return
		}
		result, err = operationComplete()

	case nodes.Manageable:
		if unprepared {
			started, result, err = p.startManualCleaning(ironicNode)
			return
		}
		// Manual clean finished
		result, err = operationComplete()

	case nodes.CleanFail:
		// When clean failed, we need to clean host provisioning settings.
		// If unprepared is false, means the settings aren't cleared.
		// So we can't set the node's state to manageable, until the settings are cleared.
		if !unprepared {
			result, err = operationFailed(ironicNode.LastError)
			return
		}
		if ironicNode.Maintenance {
			p.log.Info("clearing maintenance flag")
			result, err = p.setMaintenanceFlag(ironicNode, false)
			return
		}
		result, err = p.changeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{Target: nodes.TargetManage},
		)

	case nodes.Cleaning, nodes.CleanWait:
		p.log.Info("waiting for host to become manageable",
			"state", ironicNode.ProvisionState,
			"deploy step", ironicNode.DeployStep)
		result, err = operationContinuing(provisionRequeueDelay)

	default:
		result, err = transientError(fmt.Errorf("Have unexpected ironic node state %s", ironicNode.ProvisionState))
	}
	return
}

// Provision writes the image from the host spec to the host. It may
// be called multiple times, and should return true for its dirty flag
// until the deprovisioning operation is completed.
func (p *ironicProvisioner) Provision(hostConf provisioner.HostConfigData) (result provisioner.Result, err error) {
	var ironicNode *nodes.Node

	if ironicNode, err = p.findExistingHost(); err != nil {
		return transientError(errors.Wrap(err, "could not find host to receive image"))
	}
	if ironicNode == nil {
		return transientError(provisioner.NeedsRegistration)
	}

	p.log.Info("provisioning image to host", "state", ironicNode.ProvisionState)

	ironicHasSameImage := p.ironicHasSameImage(ironicNode)

	// Ironic has the settings it needs, see if it finds any issues
	// with them.
	switch nodes.ProvisionState(ironicNode.ProvisionState) {

	case nodes.DeployFail:
		// Since we were here ironic has recorded an error for this host,
		// with the image and checksum we have been trying to use, so we
		// should stop. (If the image values do not match, we want to try
		// again.)
		if ironicHasSameImage {
			// Save me from "eventually consistent" systems built on
			// top of relational databases...
			if ironicNode.LastError == "" {
				p.log.Info("failed but error message not available")
				return retryAfterDelay(0)
			}
			p.log.Info("found error", "msg", ironicNode.LastError)
			return operationFailed(fmt.Sprintf("Image provisioning failed: %s",
				ironicNode.LastError))
		}
		p.log.Info("recovering from previous failure")
		if provResult, err := p.setUpForProvisioning(ironicNode, hostConf); err != nil || provResult.Dirty || provResult.ErrorMessage != "" {
			return provResult, err
		}

		return p.changeNodeProvisionState(ironicNode,
			nodes.ProvisionStateOpts{Target: nodes.TargetActive})

	case nodes.Manageable:
		return p.changeNodeProvisionState(ironicNode,
			nodes.ProvisionStateOpts{Target: nodes.TargetProvide})

	case nodes.CleanFail:
		if ironicNode.Maintenance {
			p.log.Info("clearing maintenance flag")
			return p.setMaintenanceFlag(ironicNode, false)
		}
		return p.changeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{Target: nodes.TargetManage},
		)

	case nodes.Available:
		if provResult, err := p.setUpForProvisioning(ironicNode, hostConf); err != nil || provResult.Dirty || provResult.ErrorMessage != "" {
			return provResult, err
		}

		// After it is available, we need to start provisioning by
		// setting the state to "active".
		p.log.Info("making host active")

		// Retrieve cloud-init user data
		userData, err := hostConf.UserData()
		if err != nil {
			return transientError(errors.Wrap(err, "could not retrieve user data"))
		}

		// Retrieve cloud-init network_data.json. Default value is empty
		networkDataRaw, err := hostConf.NetworkData()
		if err != nil {
			return transientError(errors.Wrap(err, "could not retrieve network data"))
		}
		var networkData map[string]interface{}
		if err = yaml.Unmarshal([]byte(networkDataRaw), &networkData); err != nil {
			return transientError(errors.Wrap(err, "failed to unmarshal network_data.json from secret"))
		}

		// Retrieve cloud-init meta_data.json with falback to default
		metaData := map[string]interface{}{
			"uuid":             string(p.host.ObjectMeta.UID),
			"metal3-namespace": p.host.ObjectMeta.Namespace,
			"metal3-name":      p.host.ObjectMeta.Name,
			"local-hostname":   p.host.ObjectMeta.Name,
			"local_hostname":   p.host.ObjectMeta.Name,
		}
		metaDataRaw, err := hostConf.MetaData()
		if err != nil {
			return transientError(errors.Wrap(err, "could not retrieve metadata"))
		}
		if metaDataRaw != "" {
			if err = yaml.Unmarshal([]byte(metaDataRaw), &metaData); err != nil {
				return transientError(errors.Wrap(err, "failed to unmarshal metadata from secret"))
			}
		}

		var configDrive nodes.ConfigDrive
		if userData != "" {
			configDrive = nodes.ConfigDrive{
				UserData:    userData,
				MetaData:    metaData,
				NetworkData: networkData,
			}
			if err != nil {
				return transientError(errors.Wrap(err, "failed to build config drive"))
			}
			p.log.Info("triggering provisioning with config drive")
		} else {
			p.log.Info("triggering provisioning without config drive")
		}

		return p.changeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{
				Target:      nodes.TargetActive,
				ConfigDrive: configDrive,
			},
		)

	case nodes.Active:
		// provisioning is done
		p.publisher("ProvisioningComplete",
			fmt.Sprintf("Image provisioning completed for %s", p.host.Spec.Image.URL))
		p.log.Info("finished provisioning")
		return operationComplete()

	default:
		// wait states like cleaning and clean wait
		p.log.Info("waiting for host to become available",
			"state", ironicNode.ProvisionState,
			"deploy step", ironicNode.DeployStep)
		return operationContinuing(provisionRequeueDelay)
	}
}

func (p *ironicProvisioner) setMaintenanceFlag(ironicNode *nodes.Node, value bool) (result provisioner.Result, err error) {
	_, err = nodes.Update(
		p.client,
		ironicNode.UUID,
		nodes.UpdateOpts{
			nodes.UpdateOperation{
				Op:    nodes.ReplaceOp,
				Path:  "/maintenance",
				Value: value,
			},
		},
	).Extract()
	switch err.(type) {
	case nil:
	case gophercloud.ErrDefault409:
		p.log.Info("could not set host maintenance flag, busy")
		return retryAfterDelay(provisionRequeueDelay)
	default:
		return transientError(errors.Wrap(err, "failed to set host maintenance flag"))
	}
	return operationContinuing(0)
}

// Deprovision removes the host from the image. It may be called
// multiple times, and should return true for its dirty flag until the
// deprovisioning operation is completed.
func (p *ironicProvisioner) Deprovision(force bool) (result provisioner.Result, err error) {
	p.log.Info("deprovisioning")

	ironicNode, err := p.findExistingHost()
	if err != nil {
		return transientError(errors.Wrap(err, "failed to find existing host"))
	}
	if ironicNode == nil {
		return transientError(provisioner.NeedsRegistration)
	}

	p.log.Info("deprovisioning host",
		"ID", ironicNode.UUID,
		"lastError", ironicNode.LastError,
		"current", ironicNode.ProvisionState,
		"target", ironicNode.TargetProvisionState,
		"deploy step", ironicNode.DeployStep,
		"instance_info", ironicNode.InstanceInfo,
	)

	switch nodes.ProvisionState(ironicNode.ProvisionState) {
	case nodes.Error:
		if !force {
			p.log.Info("deprovisioning failed")
			if ironicNode.LastError == "" {
				result.ErrorMessage = "Deprovisioning failed"
			} else {
				result.ErrorMessage = ironicNode.LastError
			}
			return result, nil
		}
		p.log.Info("retrying deprovisioning")
		p.publisher("DeprovisioningStarted", "Image deprovisioning restarted")
		return p.changeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{Target: nodes.TargetDeleted},
		)

	case nodes.CleanFail:
		p.log.Info("cleaning failed")
		if ironicNode.Maintenance {
			p.log.Info("clearing maintenance flag")
			return p.setMaintenanceFlag(ironicNode, false)
		}
		// This will return us to the manageable state without completing
		// cleaning. Because cleaning happens in the process of moving from
		// manageable to available, the node will still get cleaned before
		// we provision it again.
		return p.changeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{Target: nodes.TargetManage},
		)

	case nodes.Manageable:
		// We end up here after CleanFail. Because cleaning happens in the
		// process of moving from manageable to available, the node will still
		// get cleaned before we provision it again. Therefore, just declare
		// deprovisioning complete.
		p.log.Info("deprovisioning node is in manageable state")
		return operationComplete()

	case nodes.Available:
		p.publisher("DeprovisioningComplete", "Image deprovisioning completed")
		return operationComplete()

	case nodes.Deleting:
		p.log.Info("deleting")
		// Transitions to Cleaning upon completion
		return operationContinuing(deprovisionRequeueDelay)

	case nodes.Cleaning:
		p.log.Info("cleaning")
		// Transitions to Available upon completion
		return operationContinuing(deprovisionRequeueDelay)

	case nodes.CleanWait:
		p.log.Info("cleaning")
		return operationContinuing(deprovisionRequeueDelay)

	case nodes.Active, nodes.DeployFail:
		p.log.Info("starting deprovisioning")
		p.publisher("DeprovisioningStarted", "Image deprovisioning started")
		return p.changeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{Target: nodes.TargetDeleted},
		)

	default:
		// FIXME(zaneb): this error is unlikely to actually be transient
		return transientError(fmt.Errorf("Unhandled ironic state %s", ironicNode.ProvisionState))
	}
}

// Delete removes the host from the provisioning system. It may be
// called multiple times, and should return true for its dirty flag
// until the deprovisioning operation is completed.
func (p *ironicProvisioner) Delete(retryPowerOff bool) (result provisioner.Result, err error) {

	ironicNode, err := p.findExistingHost()
	if err != nil {
		return transientError(errors.Wrap(err, "failed to find existing host"))
	}
	if ironicNode == nil {
		p.log.Info("no node found, already deleted")
		return operationComplete()
	}

	p.log.Info("deleting host",
		"ID", ironicNode.UUID,
		"lastError", ironicNode.LastError,
		"current", ironicNode.ProvisionState,
		"target", ironicNode.TargetProvisionState,
		"deploy step", ironicNode.DeployStep,
	)

	if nodes.ProvisionState(ironicNode.ProvisionState) == nodes.Available {
		// Move back to manageable so we can delete it cleanly.
		return p.changeNodeProvisionState(
			ironicNode,
			nodes.ProvisionStateOpts{Target: nodes.TargetManage},
		)
	}

	if !ironicNode.Maintenance {
		// If we see an active node and the controller doesn't think
		// we need to deprovision it, that means the node was
		// ExternallyProvisioned and we should remove it from Ironic
		// without deprovisioning it.
		//
		// If we see a node with an error, we will have to set the
		// maintenance flag before deleting it.
		//
		// Any other state requires us to use maintenance mode to
		// delete while bypassing Ironic's internal checks related to
		// Nova.
		p.log.Info("setting host maintenance flag to force image delete")
		return p.setMaintenanceFlag(ironicNode, true)
	}

	if ironicNode.PowerState == powerOn && retryPowerOff {
		p.log.Info("host ready to be powered off")
		_, err := p.hardPowerOff()
		var hostErr *HostLockedError
		if errors.As(err, &hostErr) {
			p.log.Info("could not power off host, busy")
			return retryAfterDelay(powerRequeueDelay)
		} else {
			p.log.Info("encountered transient error %s while powering off host", err)
			return operationFailed("failed to power off host")
		}
	}

	p.log.Info("host ready to be removed")
	err = nodes.Delete(p.client, ironicNode.UUID).ExtractErr()
	switch err.(type) {
	case nil:
		p.log.Info("removed")
	case gophercloud.ErrDefault409:
		p.log.Info("could not remove host, busy")
		return retryAfterDelay(provisionRequeueDelay)
	case gophercloud.ErrDefault404:
		p.log.Info("did not find host to delete, OK")
	default:
		return transientError(errors.Wrap(err, "failed to remove host"))
	}

	return operationContinuing(0)
}

func (p *ironicProvisioner) changePower(ironicNode *nodes.Node, target nodes.TargetPowerState) (result provisioner.Result, err error) {
	p.log.Info("changing power state")

	if ironicNode.TargetProvisionState != "" {
		p.log.Info("host in state that does not allow power change, try again after delay",
			"state", ironicNode.ProvisionState,
			"target state", ironicNode.TargetProvisionState,
		)
		return operationContinuing(powerRequeueDelay)
	}

	powerStateOpts := nodes.PowerStateOpts{
		Target: target,
	}
	if target == softPowerOff {
		powerStateOpts.Timeout = int(softPowerOffTimeout.Seconds())
	}

	changeResult := nodes.ChangePowerState(
		p.client,
		ironicNode.UUID,
		powerStateOpts)

	switch changeResult.Err.(type) {
	case nil:
		p.log.Info("power change OK")
		return operationContinuing(0)
	case gophercloud.ErrDefault409:
		p.log.Info("host is locked, trying again after delay", "delay", powerRequeueDelay)
		result, _ = retryAfterDelay(powerRequeueDelay)
		return result, &HostLockedError{Address: p.host.Spec.BMC.Address}
	case gophercloud.ErrDefault400:
		// Error 400 Bad Request means target power state is not supported by vendor driver
		p.log.Info("power change error", "message", changeResult.Err)
		return result, SoftPowerOffUnsupportedError{Address: p.host.Spec.BMC.Address}
	default:
		p.log.Info("power change error", "message", changeResult.Err)
		return transientError(errors.Wrap(changeResult.Err, "failed to change power state"))
	}
}

// PowerOn ensures the server is powered on independently of any image
// provisioning operation.
func (p *ironicProvisioner) PowerOn() (result provisioner.Result, err error) {
	p.log.Info("ensuring host is powered on")

	ironicNode, err := p.findExistingHost()
	if err != nil {
		return transientError(errors.Wrap(err, "failed to find existing host"))
	}

	p.log.Info("checking current state",
		"current", p.host.Status.PoweredOn,
		"target", ironicNode.TargetPowerState)

	if ironicNode.PowerState != powerOn {
		if ironicNode.TargetPowerState == powerOn {
			p.log.Info("waiting for power status to change")
			return operationContinuing(powerRequeueDelay)
		}
		result, err = p.changePower(ironicNode, nodes.PowerOn)
		switch err.(type) {
		case nil:
		case HostLockedError:
		default:
			return transientError(errors.Wrap(err, "failed to power on host"))
		}
		p.publisher("PowerOn", "Host powered on")
	}

	return result, nil
}

// PowerOff ensures the server is powered off independently of any image
// provisioning operation.
func (p *ironicProvisioner) PowerOff(rebootMode metal3v1alpha1.RebootMode) (result provisioner.Result, err error) {
	p.log.Info(fmt.Sprintf("ensuring host is powered off (mode: %s)", rebootMode))

	softPowerOffSuccessful := true

	if rebootMode != metal3v1alpha1.RebootModeHard {
		if result, err := p.softPowerOff(); err == nil {
			return result, nil
		} else {
			switch err.(type) {
			// In case of soft power off is unsupported or has failed,
			// we activate hard power off.
			case SoftPowerOffUnsupportedError, SoftPowerOffFailed:
				// Try to hard power off now
				softPowerOffSuccessful = false
			case HostLockedError:
				return retryAfterDelay(powerRequeueDelay)
			default:
				return transientError(err)
			}
		}
	}

	if rebootMode == metal3v1alpha1.RebootModeHard || !softPowerOffSuccessful {
		result, err = p.hardPowerOff()
		if err != nil {
			var hostErr *HostLockedError
			if errors.As(err, &hostErr) {
				p.log.Info("could not power off host, busy")
				return retryAfterDelay(powerRequeueDelay)
			} else {
				return transientError(err)
			}
		}
	}
	return result, nil
}

// hardPowerOff sends 'power off' request to BM node and waits for the result
func (p *ironicProvisioner) hardPowerOff() (result provisioner.Result, err error) {
	p.log.Info("ensuring host is powered off by \"hard power off\" command")

	ironicNode, err := p.findExistingHost()
	if err != nil {
		return transientError(errors.Wrap(err, "failed to find existing host"))
	}

	if ironicNode.PowerState != powerOff {
		if ironicNode.TargetPowerState == powerOff {
			p.log.Info("waiting for power status to change")
			return operationContinuing(powerRequeueDelay)
		}
		result, err = p.changePower(ironicNode, nodes.PowerOff)
		if err != nil {
			var hostErr *HostLockedError
			if errors.As(err, &hostErr) {
				p.log.Info("could not power off host, busy")
				return retryAfterDelay(powerRequeueDelay)
			} else {
				return transientError(errors.Wrap(err, "failed to power off host"))
			}
		}
		p.publisher("PowerOff", "Host powered off")
		return result, err
	}

	return operationComplete()
}

// softPowerOff sends 'soft power off' request to BM node.
// If soft power off is not supported, the request ends with an error.
// Otherwise the request ends with no error and the result should be
// checked later via node fields "power_state", "target_power_state"
// and "last_error".
func (p *ironicProvisioner) softPowerOff() (result provisioner.Result, err error) {
	p.log.Info("ensuring host is powered off by \"soft power off\" command")

	ironicNode, err := p.findExistingHost()
	if err != nil {
		return transientError(errors.Wrap(err, "failed to find existing host"))
	}

	if ironicNode.PowerState != powerOff {
		targetState := ironicNode.TargetPowerState
		// If the target state is either powerOff or softPowerOff, then we should wait
		if targetState == powerOff || targetState == softPowerOff {
			p.log.Info("waiting for power status to change")
			return operationContinuing(powerRequeueDelay)
		}
		// If the target state is unset while the last error is set,
		// then the last execution of soft power off has failed.
		if targetState == "" && ironicNode.LastError != "" {
			return result, SoftPowerOffFailed{Address: p.host.Spec.BMC.Address}
		}
		result, err = p.changePower(ironicNode, nodes.SoftPowerOff)
		if err != nil {
			return transientError(err)
		}
		p.publisher("PowerOff", "Host soft powered off")
	}

	return result, nil
}

// IsReady checks if the provisioning backend is available
func (p *ironicProvisioner) IsReady() (result bool, err error) {
	p.debugLog.Info("verifying ironic provisioner dependencies")

	checker := newIronicDependenciesChecker(p.client, p.inspector, p.log)
	return checker.IsReady()
}

func (p *ironicProvisioner) HasProvisioningCapacity() (result bool, err error) {

	hosts, err := p.loadProvisioningHosts()
	if err != nil {
		p.log.Error(err, "Unable to get hosts currently being provisioned")
		return false, err
	}

	// If the current host is already under processing then let's skip the test
	if _, ok := hosts[p.host.Name]; ok {
		return true, nil
	}

	return len(hosts) < maxProvisioningHosts, nil
}

func (p *ironicProvisioner) loadProvisioningHosts() (hosts map[string]struct{}, err error) {

	hosts = make(map[string]struct{})
	pager := nodes.List(p.client, nodes.ListOpts{
		Fields: []string{"uuid,name,provision_state,driver_internal_info,target_provision_state"},
	})
	if pager.Err != nil {
		return nil, pager.Err
	}

	page, err := pager.AllPages()
	if err != nil {
		return nil, err
	}

	allNodes, err := nodes.ExtractNodes(page)
	if err != nil {
		return nil, err
	}

	for _, node := range allNodes {

		switch nodes.ProvisionState(node.ProvisionState) {
		case nodes.Cleaning, nodes.CleanWait,
			nodes.Inspecting, nodes.InspectWait,
			nodes.Deploying, nodes.DeployWait:
			hosts[node.Name] = struct{}{}
		}
	}

	return hosts, nil
}
