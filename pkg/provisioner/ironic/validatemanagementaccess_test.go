package ironic

import (
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/ports"
	"github.com/stretchr/testify/assert"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/bmc"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/clients"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/testserver"
)

func TestValidateManagementAccessNoMAC(t *testing.T) {
	// Create a host without a bootMACAddress and with a BMC that
	// requires one.
	host := makeHost()
	host.Spec.BMC.Address = "test-needs-mac://"
	host.Spec.BootMACAddress = ""
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	ironic := testserver.NewIronic(t).Ready().NoNode(host.Namespace + nameSeparator + host.Name).NoNode(host.Name)
	ironic.Start()
	defer ironic.Stop()

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nil,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Contains(t, result.ErrorMessage, "requires a BootMACAddress")
}

func TestValidateManagementAccessMACOptional(t *testing.T) {
	// Create a host without a bootMACAddress and with a BMC that
	// does not require one.
	host := makeHost()
	host.Spec.BootMACAddress = ""

	// Set up ironic server to return the node
	ironic := testserver.NewIronic(t).Ready().
		Node(nodes.Node{
			Name: host.Namespace + nameSeparator + host.Name,
			UUID: host.Status.Provisioning.ID,
		}).NodeUpdate(nodes.Node{
		UUID: host.Status.Provisioning.ID,
	})
	ironic.Start()
	defer ironic.Stop()

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nil,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "", result.ErrorMessage)
}

func TestValidateManagementAccessCreateNodeNoImage(t *testing.T) {
	// Create a host without a bootMACAddress and with a BMC that
	// does not require one.
	host := makeHost()
	host.Spec.BootMACAddress = ""
	host.Spec.Image = nil
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	var createdNode *nodes.Node

	createCallback := func(node nodes.Node) {
		createdNode = &node
	}

	ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).NoNode(host.Namespace + nameSeparator + host.Name).NoNode(host.Name)
	ironic.AddDefaultResponse("/v1/nodes/node-0", "PATCH", http.StatusOK, "{}")
	ironic.Start()
	defer ironic.Stop()

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, provID, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "", result.ErrorMessage)
	assert.NotEqual(t, "", createdNode.UUID)
	assert.Equal(t, createdNode.UUID, provID)
	assert.Equal(t, createdNode.DeployInterface, "direct")
}

func TestValidateManagementAccessCreateWithImage(t *testing.T) {
	// Create a host with Image specified in the Spec
	host := makeHost()
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid
	host.Spec.Image.URL = "theimagefoo"
	host.Spec.Image.Checksum = "thechecksumxyz"

	var createdNode *nodes.Node

	createCallback := func(node nodes.Node) {
		createdNode = &node
	}

	ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).NoNode(host.Namespace + nameSeparator + host.Name).NoNode(host.Name)
	ironic.AddDefaultResponse("/v1/nodes/node-0", "PATCH", http.StatusOK, "{}")
	ironic.Start()
	defer ironic.Stop()

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, provID, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{CurrentImage: host.Spec.Image.DeepCopy()}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "", result.ErrorMessage)
	assert.Equal(t, createdNode.UUID, provID)
	assert.Equal(t, createdNode.DeployInterface, "direct")
	updates, _ := ironic.GetLastRequestFor("/v1/nodes/node-0", http.MethodPatch)
	assert.Contains(t, updates, "/instance_info/image_source")
	assert.Contains(t, updates, host.Spec.Image.URL)
	assert.Contains(t, updates, "/instance_info/image_checksum")
	assert.Contains(t, updates, host.Spec.Image.Checksum)
}

func TestValidateManagementAccessCreateWithLiveIso(t *testing.T) {
	// Create a host with Image specified in the Spec
	host := makeHostLiveIso()
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	var createdNode *nodes.Node

	createCallback := func(node nodes.Node) {
		createdNode = &node
	}

	ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).NoNode(host.Namespace + nameSeparator + host.Name).NoNode(host.Name)
	ironic.AddDefaultResponse("/v1/nodes/node-0", "PATCH", http.StatusOK, "{}")
	ironic.Start()
	defer ironic.Stop()

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, provID, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{CurrentImage: host.Spec.Image.DeepCopy()}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "", result.ErrorMessage)
	assert.Equal(t, createdNode.UUID, provID)
	assert.Equal(t, createdNode.DeployInterface, "ramdisk")
	updates, _ := ironic.GetLastRequestFor("/v1/nodes/node-0", http.MethodPatch)
	assert.Contains(t, updates, "/instance_info/boot_iso")
	assert.Contains(t, updates, host.Spec.Image.URL)
}

func TestValidateManagementAccessExistingNode(t *testing.T) {
	// Create a host without a bootMACAddress and with a BMC that
	// does not require one.
	host := makeHost()
	host.Spec.BootMACAddress = ""
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	createCallback := func(node nodes.Node) {
		t.Fatal("create callback should not be invoked for existing node")
	}

	ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).Node(nodes.Node{
		Name: host.Namespace + nameSeparator + host.Name,
		UUID: "uuid",
	}).NodeUpdate(
		nodes.Node{
			UUID: "uuid",
		})
	ironic.Start()
	defer ironic.Stop()

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, provID, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "", result.ErrorMessage)
	assert.Equal(t, "uuid", provID)
}

func TestValidateManagementAccessExistingNodeNameUpdate(t *testing.T) {
	// Create a host without a bootMACAddress and with a BMC that
	// does not require one.
	host := makeHost()
	host.Spec.BootMACAddress = ""
	host.Status.Provisioning.ID = "uuid"

	ironic := testserver.NewIronic(t).
		Node(
			nodes.Node{
				Name: host.Name,
				UUID: "uuid",
			}).
		NodeUpdate(
			nodes.Node{
				Name: host.Namespace + nameSeparator + host.Name,
				UUID: "uuid",
			})
	ironic.Start()
	defer ironic.Stop()

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "", result.ErrorMessage)
	assert.Equal(t, false, result.Dirty)
}

func TestValidateManagementAccessExistingNodeContinue(t *testing.T) {
	statuses := []nodes.ProvisionState{
		nodes.Manageable,
		nodes.Available,
		nodes.Active,
		nodes.DeployWait,
		nodes.Deploying,
		nodes.DeployFail,
		nodes.DeployDone,
		nodes.Deleting,
		nodes.Deleted,
		nodes.Cleaning,
		nodes.CleanWait,
		nodes.CleanFail,
		nodes.Error,
		nodes.Rebuild,
		nodes.Inspecting,
		nodes.InspectFail,
		nodes.InspectWait,
		nodes.Adopting,
		nodes.AdoptFail,
		nodes.Rescue,
		nodes.RescueFail,
		nodes.Rescuing,
		nodes.UnrescueFail,
	}
	clean := true

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			// Create a host without a bootMACAddress and with a BMC that
			// does not require one.
			host := makeHost()
			host.Spec.BootMACAddress = ""
			host.Status.Provisioning.ID = "" // so we don't lookup by uuid

			createCallback := func(node nodes.Node) {
				t.Fatal("create callback should not be invoked for existing node")
			}

			ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).Node(nodes.Node{
				Name:           host.Namespace + nameSeparator + host.Name,
				UUID:           "uuid", // to match status in host
				ProvisionState: string(status),
				AutomatedClean: &clean,
				DriverInfo: map[string]interface{}{
					"deploy_kernel":  "http://deploy.test/ipa.kernel",
					"deploy_ramdisk": "http://deploy.test/ipa.initramfs",
					"test_address":   "test.bmc",
					"test_username":  "",
					"test_password":  "",
					"test_port":      "42",
				},
			}).NodeUpdate(nodes.Node{
				UUID: "uuid",
			})
			ironic.Start()
			defer ironic.Stop()

			auth := clients.AuthConfig{Type: clients.NoAuth}
			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
				ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
			)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}

			result, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
			if err != nil {
				t.Fatalf("error from ValidateManagementAccess: %s", err)
			}
			assert.Equal(t, "", result.ErrorMessage)
			assert.Equal(t, false, result.Dirty)
			assert.Len(t, ironic.GetLastNodeUpdateRequestFor("uuid"), 0)
		})
	}
}

func TestValidateManagementAccessExistingSteadyStateNoUpdate(t *testing.T) {
	liveFormat := "live-iso"
	imageTypes := []struct {
		DeployInterface string
		Image           *metal3v1alpha1.Image
		InstanceInfo    map[string]interface{}
	}{
		{
			DeployInterface: "",
			InstanceInfo: map[string]interface{}{
				"capabilities": map[string]interface{}{},
			},
		},
		{
			DeployInterface: "direct",
			Image: &metal3v1alpha1.Image{
				URL:      "theimage",
				Checksum: "thechecksum",
			},
			InstanceInfo: map[string]interface{}{
				"image_source":        "theimage",
				"image_os_hash_algo":  "md5",
				"image_os_hash_value": "thechecksum",
				"image_checksum":      "thechecksum",
				"capabilities":        map[string]interface{}{},
			},
		},
		{
			DeployInterface: "ramdisk",
			Image: &metal3v1alpha1.Image{
				URL:        "theimage",
				DiskFormat: &liveFormat,
			},
			InstanceInfo: map[string]interface{}{
				"boot_iso":     "theimage",
				"capabilities": map[string]interface{}{},
			},
		},
	}
	clean := true

	for _, imageType := range imageTypes {
		t.Run(imageType.DeployInterface, func(t *testing.T) {
			// Create a host without a bootMACAddress and with a BMC that
			// does not require one.
			host := makeHost()
			host.Spec.BootMACAddress = ""
			host.Status.Provisioning.ID = "" // so we don't lookup by uuid

			createCallback := func(node nodes.Node) {
				t.Fatal("create callback should not be invoked for existing node")
			}

			ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).Node(nodes.Node{
				Name:            host.Namespace + nameSeparator + host.Name,
				UUID:            "uuid", // to match status in host
				ProvisionState:  string(nodes.Manageable),
				AutomatedClean:  &clean,
				InstanceUUID:    string(host.UID),
				DeployInterface: imageType.DeployInterface,
				InstanceInfo:    imageType.InstanceInfo,
				DriverInfo: map[string]interface{}{
					"deploy_kernel":  "http://deploy.test/ipa.kernel",
					"deploy_ramdisk": "http://deploy.test/ipa.initramfs",
					"test_address":   "test.bmc",
					"test_username":  "",
					"test_password":  "",
					"test_port":      "42",
				},
			}).NodeUpdate(nodes.Node{
				UUID: "uuid",
			})
			ironic.Start()
			defer ironic.Stop()

			auth := clients.AuthConfig{Type: clients.NoAuth}
			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
				ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
			)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}

			result, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{CurrentImage: imageType.Image}, false, false)
			if err != nil {
				t.Fatalf("error from ValidateManagementAccess: %s", err)
			}
			assert.Equal(t, "", result.ErrorMessage)
			assert.Equal(t, false, result.Dirty)
			assert.Len(t, ironic.GetLastNodeUpdateRequestFor("uuid"), 0)
		})
	}
}

func TestValidateManagementAccessExistingNodeWaiting(t *testing.T) {
	statuses := []nodes.ProvisionState{
		nodes.Enroll,
		nodes.Verifying,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			// Create a host without a bootMACAddress and with a BMC that
			// does not require one.
			host := makeHost()
			host.Spec.BootMACAddress = ""
			host.Status.Provisioning.ID = "uuid"

			createCallback := func(node nodes.Node) {
				t.Fatal("create callback should not be invoked for existing node")
			}

			node := nodes.Node{
				Name:           host.Namespace + nameSeparator + host.Name,
				UUID:           "uuid", // to match status in host
				ProvisionState: string(status),
				DriverInfo: map[string]interface{}{
					"deploy_kernel":  "http://deploy.test/ipa.kernel",
					"deploy_ramdisk": "http://deploy.test/ipa.initramfs",
					"test_address":   "test.bmc",
					"test_username":  "",
					"test_password":  "",
					"test_port":      "42",
				},
			}
			ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).Node(node).NodeUpdate(nodes.Node{
				UUID: "uuid",
			}).WithNodeStatesProvisionUpdate(node.UUID)
			ironic.Start()
			defer ironic.Stop()

			auth := clients.AuthConfig{Type: clients.NoAuth}
			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
				ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
			)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}

			result, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
			if err != nil {
				t.Fatalf("error from ValidateManagementAccess: %s", err)
			}
			assert.Equal(t, "", result.ErrorMessage)
			assert.Equal(t, true, result.Dirty)

			updates := ironic.GetLastNodeUpdateRequestFor("uuid")
			assert.Len(t, updates, 1)
			assert.Equal(t, "/automated_clean", updates[0].Path)
			assert.Equal(t, true, updates[0].Value)
		})
	}
}

func TestValidateManagementAccessNewCredentials(t *testing.T) {
	// Create a host without a bootMACAddress and with a BMC that
	// does not require one.
	host := makeHost()
	host.Spec.BootMACAddress = ""
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	ironic := testserver.NewIronic(t).
		Node(
			nodes.Node{
				Name: host.Namespace + nameSeparator + host.Name,
				UUID: "uuid",
			}).
		NodeUpdate(
			nodes.Node{
				Name: host.Namespace + nameSeparator + host.Name,
				UUID: "uuid",
				DriverInfo: map[string]interface{}{
					"test_address": "test.bmc",
				},
			})
	ironic.Start()
	defer ironic.Stop()

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, provID, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, true, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "", result.ErrorMessage)
	assert.Equal(t, "uuid", provID)

	updates := ironic.GetLastNodeUpdateRequestFor("uuid")
	assert.Equal(t, "/driver_info", updates[0].Path)
	newValues := updates[0].Value.(map[string]interface{})
	assert.Equal(t, "test.bmc", newValues["test_address"])
}

func TestValidateManagementAccessLinkExistingIronicNodeByMAC(t *testing.T) {
	// Create an Ironic node, and then create a host with a matching MAC
	// Test to see if the node was found, and if the link is made

	existingNode := nodes.Node{
		UUID: "33ce8659-7400-4c68-9535-d10766f07a58",
	}

	existingNodePort := ports.Port{
		NodeUUID: existingNode.UUID,
		Address:  "11:11:11:11:11:11",
	}

	createCallback := func(node nodes.Node) {
		t.Fatal("create callback should not be invoked for existing node")
	}

	ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).Node(existingNode).Port(existingNodePort)
	ironic.AddDefaultResponse("/v1/nodes/myns"+nameSeparator+"myhost", "GET", http.StatusNotFound, "")
	ironic.AddDefaultResponse("/v1/nodes/myhost", "GET", http.StatusNotFound, "")
	ironic.AddDefaultResponse("/v1/nodes/"+existingNode.UUID, "PATCH", http.StatusOK, "{}")
	ironic.Start()
	defer ironic.Stop()

	host := makeHost()
	host.Spec.BootMACAddress = "11:11:11:11:11:11"
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, provID, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "", result.ErrorMessage)
	assert.NotEqual(t, "", provID)
}

func TestValidateManagementAccessExistingPortWithWrongUUID(t *testing.T) {
	// Create a node, and a port.  The port has a node uuid that doesn't match the node.
	// ValidateManagementAccess should return an error.

	existingNode := nodes.Node{
		UUID: "33ce8659-7400-4c68-9535-d10766f07a58",
	}

	existingNodePort := ports.Port{
		NodeUUID: "random-wrong-id",
		Address:  "11:11:11:11:11:11",
	}

	createCallback := func(node nodes.Node) {
		t.Fatal("create callback should not be invoked for existing node")
	}

	ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).Node(existingNode).Port(existingNodePort)
	ironic.AddDefaultResponse("/v1/nodes/myns"+nameSeparator+"myhost", "GET", http.StatusNotFound, "")
	ironic.AddDefaultResponse("/v1/nodes/myhost", "GET", http.StatusNotFound, "")
	ironic.AddDefaultResponse("/v1/nodes/random-wrong-id", "GET", http.StatusNotFound, "")
	ironic.Start()
	defer ironic.Stop()

	host := makeHost()
	host.Spec.BootMACAddress = "11:11:11:11:11:11"
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	_, _, err = prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	assert.EqualError(t, err, "failed to find existing host: port exists but linked node doesn't random-wrong-id: Resource not found")
}

func TestValidateManagementAccessExistingPortButHasName(t *testing.T) {
	// Create a node, and a port.
	// The port is linked to the node.
	// The port address matches the BMH BootMACAddress.
	// The node has a name, and the name doesn't match the BMH.
	// ValidateManagementAccess should return an error.

	existingNode := nodes.Node{
		UUID: "33ce8659-7400-4c68-9535-d10766f07a58",
		Name: "wrong-name",
	}

	existingNodePort := ports.Port{
		NodeUUID: existingNode.UUID,
		Address:  "11:11:11:11:11:11",
	}

	createCallback := func(node nodes.Node) {
		t.Fatal("create callback should not be invoked for existing node")
	}

	ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).Node(existingNode).Port(existingNodePort)
	ironic.AddDefaultResponse("/v1/nodes/myns"+nameSeparator+"myhost", "GET", http.StatusNotFound, "")
	ironic.AddDefaultResponse("/v1/nodes/myhost", "GET", http.StatusNotFound, "")
	ironic.Start()
	defer ironic.Stop()

	host := makeHost()
	host.Spec.BootMACAddress = "11:11:11:11:11:11"
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	res, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	assert.Nil(t, err)
	assert.Equal(t, res.ErrorMessage, "MAC address 11:11:11:11:11:11 conflicts with existing node wrong-name")
}

func TestValidateManagementAccessAddTwoHostsWithSameMAC(t *testing.T) {

	existingNode := nodes.Node{
		UUID: "33ce8659-7400-4c68-9535-d10766f07a58",
		Name: "myns" + nameSeparator + "myhost",
	}

	existingNodePort := ports.Port{
		NodeUUID: existingNode.UUID,
		Address:  "11:11:11:11:11:11",
	}

	createCallback := func(node nodes.Node) {
		t.Fatal("create callback should not be invoked for existing node")
	}

	ironic := testserver.NewIronic(t).Ready().CreateNodes(createCallback).Node(existingNode).NodeUpdate(nodes.Node{
		UUID: "33ce8659-7400-4c68-9535-d10766f07a58",
	}).Port(existingNodePort)
	ironic.Start()
	defer ironic.Stop()

	host := makeHost()
	// This value is different than the port that actually exists
	host.Spec.BootMACAddress = "22:22:22:22:22:22"
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	// MAC address value is different than the port that actually exists
	result, provID, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "", result.ErrorMessage)
	assert.NotEqual(t, "", provID)
}

func TestValidateManagementAccessUnsupportedSecureBoot(t *testing.T) {
	// Create a host without a bootMACAddress and with a BMC that
	// requires one.
	host := makeHost()
	host.Status.Provisioning.ID = "" // so we don't lookup by uuid

	ironic := testserver.NewIronic(t).Ready().NoNode("myns" + nameSeparator + host.Name).NoNode(host.Name)
	ironic.Start()
	defer ironic.Stop()

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nil,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{BootMode: metal3v1alpha1.UEFISecureBoot}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Contains(t, result.ErrorMessage, "does not support secure boot")
}

func TestValidateManagementAccessNoBMCDetails(t *testing.T) {
	ironic := testserver.NewIronic(t).Ready()
	ironic.Start()
	defer ironic.Stop()

	host := makeHost()
	host.Spec.BMC = metal3v1alpha1.BMCDetails{}

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "failed to parse BMC address information: missing BMC address", result.ErrorMessage)
}

func TestValidateManagementAccessMalformedBMCAddress(t *testing.T) {
	ironic := testserver.NewIronic(t).Ready()
	ironic.Start()
	defer ironic.Stop()

	host := makeHost()
	host.Spec.BMC = metal3v1alpha1.BMCDetails{
		Address: "<ipmi://192.168.122.1:6233>",
	}

	auth := clients.AuthConfig{Type: clients.NoAuth}
	prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
		ironic.Endpoint(), auth, testserver.NewInspector(t).Endpoint(), auth,
	)
	if err != nil {
		t.Fatalf("could not create provisioner: %s", err)
	}

	result, _, err := prov.ValidateManagementAccess(provisioner.ManagementAccessData{}, false, false)
	if err != nil {
		t.Fatalf("error from ValidateManagementAccess: %s", err)
	}
	assert.Equal(t, "failed to parse BMC address information: failed to parse BMC address information: parse \"<ipmi://192.168.122.1:6233>\": first path segment in URL cannot contain colon", result.ErrorMessage)
}
