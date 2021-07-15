package fixture

import (
	"time"

	"github.com/go-logr/logr"
	logz "sigs.k8s.io/controller-runtime/pkg/log/zap"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/bmc"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner"
)

var log = logz.New().WithName("provisioner").WithName("fixture")
var deprovisionRequeueDelay = time.Second * 10
var provisionRequeueDelay = time.Second * 10

type fixtureHostConfigData struct {
	userData    string
	networkData string
	metaData    string
}

// NewHostConfigData creates new host configuration data
func NewHostConfigData(userData string, networkData string, metaData string) provisioner.HostConfigData {
	return &fixtureHostConfigData{
		userData:    userData,
		networkData: networkData,
		metaData:    metaData,
	}
}

func (cd *fixtureHostConfigData) UserData() (string, error) {
	return cd.userData, nil
}

func (cd *fixtureHostConfigData) NetworkData() (string, error) {
	return cd.networkData, nil
}

func (cd *fixtureHostConfigData) MetaData() (string, error) {
	return cd.metaData, nil
}

// fixtureProvisioner implements the provisioning.fixtureProvisioner interface
// and uses Ironic to manage the host.
type fixtureProvisioner struct {
	// the provisioning ID for this host
	provID string
	// the bmc credentials
	bmcCreds bmc.Credentials
	// a logger configured for this host
	log logr.Logger
	// an event publisher for recording significant events
	publisher provisioner.EventPublisher
	// state storage for the Host
	state *Fixture
}

// Fixture contains persistent state for a particular host
type Fixture struct {
	// counter to set the provisioner as ready
	BecomeReadyCounter int
	// state to manage deletion
	Deleted bool
	// state to manage the two-step adopt process
	adopted bool
	// state to manage provisioning
	image metal3v1alpha1.Image
	// state to manage power
	poweredOn bool

	validateError string

	customDeploy *metal3v1alpha1.CustomDeploy
}

// New returns a new Fixture Provisioner
func (f *Fixture) NewProvisioner(hostData provisioner.HostData, publisher provisioner.EventPublisher) (provisioner.Provisioner, error) {
	p := &fixtureProvisioner{
		provID:    hostData.ProvisionerID,
		bmcCreds:  hostData.BMCCredentials,
		log:       log.WithValues("host", hostData.ObjectMeta.Name),
		publisher: publisher,
		state:     f,
	}
	return p, nil
}

func (f *Fixture) SetValidateError(message string) {
	f.validateError = message
}

func (p *fixtureProvisioner) HasCapacity() (result bool, err error) {
	return true, nil
}

// ValidateManagementAccess tests the connection information for the
// host to verify that the location and credentials work.
func (p *fixtureProvisioner) ValidateManagementAccess(data provisioner.ManagementAccessData, credentialsChanged, force bool) (result provisioner.Result, provID string, err error) {
	p.log.Info("testing management access")

	if p.state.validateError != "" {
		result.ErrorMessage = p.state.validateError
		return
	}

	// Fill in the ID of the host in the provisioning system
	if p.provID == "" {
		provID = "temporary-fake-id"
		result.Dirty = true
		result.RequeueAfter = time.Second * 5
		p.publisher("Registered", "Registered new host")
		return
	}

	return
}

// InspectHardware updates the HardwareDetails field of the host with
// details of devices discovered on the hardware. It may be called
// multiple times, and should return true for its dirty flag until the
// inspection is completed.
func (p *fixtureProvisioner) InspectHardware(data provisioner.InspectData, force, refresh bool) (result provisioner.Result, started bool, details *metal3v1alpha1.HardwareDetails, err error) {
	// The inspection is ongoing. We'll need to check the fixture
	// status for the server here until it is ready for us to get the
	// inspection details. Simulate that for now by creating the
	// hardware details struct as part of a second pass.
	p.log.Info("continuing inspection by setting details")
	started = true
	details =
		&metal3v1alpha1.HardwareDetails{
			RAMMebibytes: 128 * 1024,
			NIC: []metal3v1alpha1.NIC{
				{
					Name:      "nic-1",
					Model:     "virt-io",
					MAC:       "some:mac:address",
					IP:        "192.168.100.1",
					SpeedGbps: 1,
					PXE:       true,
				},
				{
					Name:      "nic-2",
					Model:     "e1000",
					MAC:       "some:other:mac:address",
					IP:        "192.168.100.2",
					SpeedGbps: 1,
					PXE:       false,
				},
			},
			Storage: []metal3v1alpha1.Storage{
				{
					Name:       "disk-1 (boot)",
					Rotational: false,
					SizeBytes:  metal3v1alpha1.TebiByte * 93,
					Model:      "Dell CFJ61",
				},
				{
					Name:       "disk-2",
					Rotational: false,
					SizeBytes:  metal3v1alpha1.TebiByte * 93,
					Model:      "Dell CFJ61",
				},
			},
			CPU: metal3v1alpha1.CPU{
				Arch:           "x86_64",
				Model:          "FancyPants CPU",
				ClockMegahertz: 3.0 * metal3v1alpha1.GigaHertz,
				Flags:          []string{"fpu", "hypervisor", "sse", "vmx"},
				Count:          1,
			},
		}
	p.publisher("InspectionComplete", "Hardware inspection completed")

	return
}

// UpdateHardwareState fetches the latest hardware state of the server
// and updates the HardwareDetails field of the host with details. It
// is expected to do this in the least expensive way possible, such as
// reading from a cache.
func (p *fixtureProvisioner) UpdateHardwareState() (hwState provisioner.HardwareState, err error) {
	hwState.PoweredOn = &p.state.poweredOn
	p.log.Info("updating hardware state")
	return
}

// Prepare remove existing configuration and set new configuration
func (p *fixtureProvisioner) Prepare(data provisioner.PrepareData, unprepared bool) (result provisioner.Result, started bool, err error) {
	p.log.Info("preparing host")
	started = unprepared
	return
}

// Adopt notifies the provisioner that the state machine believes the host
// to be currently provisioned, and that it should be managed as such.
func (p *fixtureProvisioner) Adopt(data provisioner.AdoptData, force bool) (result provisioner.Result, err error) {
	p.log.Info("adopting host")
	if !p.state.adopted {
		p.state.adopted = true
		result.Dirty = true
		result.RequeueAfter = provisionRequeueDelay
	}
	return
}

// Provision writes the image from the host spec to the host. It may
// be called multiple times, and should return true for its dirty flag
// until the provisioning operation is completed.
func (p *fixtureProvisioner) Provision(data provisioner.ProvisionData) (result provisioner.Result, err error) {
	p.log.Info("provisioning image to host")

	if data.CustomDeploy != nil && p.state.customDeploy == nil {
		p.publisher("ProvisioningComplete", "Custom deploy provisioning completed")
		p.log.Info("moving to done")
		p.state.customDeploy = data.CustomDeploy.DeepCopy()
		result.Dirty = true
		result.RequeueAfter = provisionRequeueDelay
		return result, nil
	}

	if data.Image.URL != "" && p.state.image.URL == "" {
		p.publisher("ProvisioningComplete", "Image provisioning completed")
		p.log.Info("moving to done")
		p.state.image = data.Image
		result.Dirty = true
		result.RequeueAfter = provisionRequeueDelay
	}

	return result, nil
}

// Deprovision removes the host from the image. It may be called
// multiple times, and should return true for its dirty flag until the
// deprovisioning operation is completed.
func (p *fixtureProvisioner) Deprovision(force bool) (result provisioner.Result, err error) {
	p.log.Info("ensuring host is deprovisioned")

	result.RequeueAfter = deprovisionRequeueDelay

	// NOTE(dhellmann): In order to simulate a multi-step process,
	// modify some of the status data structures. This is likely not
	// necessary once we really have Fixture doing the deprovisioning
	// and we can monitor it's status.

	if p.state.image.URL != "" {
		p.publisher("DeprovisionStarted", "Image deprovisioning started")
		p.log.Info("clearing hardware details")
		p.state.image = metal3v1alpha1.Image{}
		result.Dirty = true
		return result, nil
	}

	if p.state.customDeploy != nil {
		p.publisher("DeprovisionStarted", "Custom deploy deprovisioning started")
		p.log.Info("clearing hardware details")
		p.state.customDeploy = nil
		result.Dirty = true
		return result, nil
	}

	p.publisher("DeprovisionComplete", "Image deprovisioning completed")
	return result, nil
}

// Delete removes the host from the provisioning system. It may be
// called multiple times, and should return true for its dirty flag
// until the deprovisioning operation is completed.
func (p *fixtureProvisioner) Delete() (result provisioner.Result, err error) {
	p.log.Info("deleting host")

	if !p.state.Deleted {
		p.log.Info("clearing provisioning id")
		p.state.Deleted = true
		result.Dirty = true
		return result, nil
	}

	return result, nil
}

// Detach removes the host from the provisioning system.
// Similar to Delete, but ensures non-interruptive behavior
// for the target system.  It may be called multiple times,
// and should return true for its dirty  flag until the
// deletion operation is completed.
func (p *fixtureProvisioner) Detach() (result provisioner.Result, err error) {
	return p.Delete()
}

// PowerOn ensures the server is powered on independently of any image
// provisioning operation.
func (p *fixtureProvisioner) PowerOn(force bool) (result provisioner.Result, err error) {
	p.log.Info("ensuring host is powered on")

	if !p.state.poweredOn {
		p.publisher("PowerOn", "Host powered on")
		p.log.Info("changing status")
		p.state.poweredOn = true
		result.Dirty = true
		return result, nil
	}

	return result, nil
}

// PowerOff ensures the server is powered off independently of any image
// provisioning operation.
func (p *fixtureProvisioner) PowerOff(rebootMode metal3v1alpha1.RebootMode, force bool) (result provisioner.Result, err error) {
	p.log.Info("ensuring host is powered off")

	if p.state.poweredOn {
		p.publisher("PowerOff", "Host powered off")
		p.log.Info("changing status")
		p.state.poweredOn = false
		result.Dirty = true
		return result, nil
	}

	return result, nil
}

// IsReady returns the current availability status of the provisioner
func (p *fixtureProvisioner) IsReady() (result bool, err error) {
	p.log.Info("checking provisioner status")

	if p.state.BecomeReadyCounter > 0 {
		p.state.BecomeReadyCounter--
	}

	return p.state.BecomeReadyCounter == 0, nil
}

func (p *fixtureProvisioner) GetFirmwareSettings(includeSchema bool) (settings metal3v1alpha1.SettingsMap, schema map[string]metal3v1alpha1.SettingSchema, err error) {
	p.log.Info("getting BIOS settings")
	return
}
