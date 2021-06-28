package ironic

import (
	"net/url"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
	"github.com/gophercloud/gophercloud/openstack/baremetalintrospection/v1/introspection"
	"github.com/stretchr/testify/assert"

	"github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/bmc"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/fixture"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/clients"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/testbmc"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/testserver"

	"k8s.io/apimachinery/pkg/util/intstr"

	_ "github.com/metal3-io/baremetal-operator/pkg/bmc"
)

func TestProvision(t *testing.T) {

	nodeUUID := "33ce8659-7400-4c68-9535-d10766f07a58"
	cases := []struct {
		name                 string
		ironic               *testserver.IronicMock
		expectedDirty        bool
		expectedError        bool
		expectedErrorMessage bool
		expectedRequestAfter int
	}{
		{
			name: "deployFail state",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.DeployFail),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 0,
			expectedDirty:        false,
			expectedErrorMessage: true,
		},
		{
			name: "cleanFail state",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.CleanFail),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 10,
			expectedDirty:        true,
		},
		{
			name: "manageable state",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.Manageable),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 10,
			expectedDirty:        true,
		},
		{
			name: "available state",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.Available),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 0,
			expectedDirty:        false,
			expectedErrorMessage: true,
		},
		{
			name: "active state",
			ironic: testserver.NewIronic(t).Ready().Node(nodes.Node{
				ProvisionState: string(nodes.Active),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 0,
			expectedDirty:        false,
		},
		{
			name: "other state: Cleaning",
			ironic: testserver.NewIronic(t).Ready().Node(nodes.Node{
				ProvisionState: string(nodes.Cleaning),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 10,
			expectedDirty:        true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ironic != nil {
				tc.ironic.Start()
				defer tc.ironic.Stop()
			}

			inspector := testserver.NewInspector(t).Ready().WithIntrospection(nodeUUID, introspection.Introspection{
				Finished: false,
			})
			inspector.Start()
			defer inspector.Stop()

			host := makeHost()
			host.Status.Provisioning.ID = nodeUUID
			publisher := func(reason, message string) {}
			auth := clients.AuthConfig{Type: clients.NoAuth}
			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, publisher,
				tc.ironic.Endpoint(), auth, inspector.Endpoint(), auth,
			)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}

			result, err := prov.Provision(provisioner.ProvisionData{
				HostConfig: fixture.NewHostConfigData("testUserData", "test: NetworkData", "test: Meta"),
				BootMode:   v1alpha1.DefaultBootMode,
			})

			assert.Equal(t, tc.expectedDirty, result.Dirty)
			assert.Equal(t, time.Second*time.Duration(tc.expectedRequestAfter), result.RequeueAfter)
			if !tc.expectedErrorMessage {
				assert.Equal(t, "", result.ErrorMessage)
			} else {
				assert.NotEqual(t, "", result.ErrorMessage)
			}
			if !tc.expectedError {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestDeprovision(t *testing.T) {

	nodeUUID := "33ce8659-7400-4c68-9535-d10766f07a58"
	cases := []struct {
		name                 string
		ironic               *testserver.IronicMock
		expectedDirty        bool
		expectedError        bool
		expectedErrorMessage bool
		expectedRequestAfter int
	}{
		{
			name: "active state",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.Active),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 10,
			expectedDirty:        true,
		},
		{
			name: "deploy failed state",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.DeployFail),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 10,
			expectedDirty:        true,
		},
		{
			name: "error state",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.Error),
				UUID:           nodeUUID,
			}),
			expectedErrorMessage: true,
		},
		{
			name: "available state",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.Available),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 0,
			expectedDirty:        false,
		},
		{
			name: "deleting state",
			ironic: testserver.NewIronic(t).Ready().Node(nodes.Node{
				ProvisionState: string(nodes.Deleting),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 10,
			expectedDirty:        true,
		},
		{
			name: "cleaning state",
			ironic: testserver.NewIronic(t).Ready().Node(nodes.Node{
				ProvisionState: string(nodes.Cleaning),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 10,
			expectedDirty:        true,
		},
		{
			name: "cleanWait state",
			ironic: testserver.NewIronic(t).Ready().Node(nodes.Node{
				ProvisionState: string(nodes.CleanWait),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 10,
			expectedDirty:        true,
		},
		{
			name: "clean fail state",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.CleanFail),
				UUID:           nodeUUID,
			}),
			expectedRequestAfter: 10,
			expectedDirty:        true,
		},
		{
			name: "manageable state",
			ironic: testserver.NewIronic(t).Ready().Node(nodes.Node{
				ProvisionState: string(nodes.Manageable),
				UUID:           nodeUUID,
			}),
			expectedDirty: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ironic != nil {
				tc.ironic.Start()
				defer tc.ironic.Stop()
			}

			inspector := testserver.NewInspector(t).Ready().WithIntrospection(nodeUUID, introspection.Introspection{
				Finished: false,
			})
			inspector.Start()
			defer inspector.Stop()

			host := makeHost()
			host.Status.Provisioning.ID = nodeUUID
			publisher := func(reason, message string) {}
			auth := clients.AuthConfig{Type: clients.NoAuth}
			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, publisher,
				tc.ironic.Endpoint(), auth, inspector.Endpoint(), auth,
			)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}

			result, err := prov.Deprovision(false)

			assert.Equal(t, tc.expectedDirty, result.Dirty)
			assert.Equal(t, tc.expectedErrorMessage, result.ErrorMessage != "")
			assert.Equal(t, time.Second*time.Duration(tc.expectedRequestAfter), result.RequeueAfter)
			if !tc.expectedError {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestIronicHasSameImage(t *testing.T) {
	nodeUUID := "33ce8659-7400-4c68-9535-d10766f07a58"
	cases := []struct {
		name             string
		expected         bool
		node             nodes.Node
		liveImage        bool
		hostImage        string
		hostChecksum     string
		hostChecksumType v1alpha1.ChecksumType
	}{
		{
			name:      "image same",
			expected:  true,
			liveImage: false,
			node: nodes.Node{
				InstanceInfo: map[string]interface{}{
					"image_source":        "theimage",
					"image_os_hash_value": "thechecksum",
					"image_os_hash_algo":  "md5",
				},
			},
			hostImage:        "theimage",
			hostChecksum:     "thechecksum",
			hostChecksumType: v1alpha1.MD5,
		},
		{
			name:      "image different",
			expected:  false,
			liveImage: false,
			node: nodes.Node{
				InstanceInfo: map[string]interface{}{
					"image_source":        "theimage",
					"image_os_hash_value": "thechecksum",
					"image_os_hash_algo":  "md5",
				},
			},
			hostImage:        "different",
			hostChecksum:     "thechecksum",
			hostChecksumType: v1alpha1.MD5,
		},
		{
			name:      "image checksum different",
			expected:  false,
			liveImage: false,
			node: nodes.Node{
				InstanceInfo: map[string]interface{}{
					"image_source":        "theimage",
					"image_os_hash_value": "thechecksum",
					"image_os_hash_algo":  "md5",
				},
			},
			hostImage:        "theimage",
			hostChecksum:     "different",
			hostChecksumType: v1alpha1.MD5,
		},
		{
			name:      "image checksum type different",
			expected:  false,
			liveImage: false,
			node: nodes.Node{
				InstanceInfo: map[string]interface{}{
					"image_source":        "theimage",
					"image_os_hash_value": "thechecksum",
					"image_os_hash_algo":  "md5",
				},
			},
			hostImage:        "theimage",
			hostChecksum:     "thechecksum",
			hostChecksumType: v1alpha1.SHA512,
		},
		{
			name:      "live image same",
			liveImage: true,
			expected:  true,
			node: nodes.Node{
				InstanceInfo: map[string]interface{}{
					"boot_iso": "theimage",
				},
			},
			hostImage: "theimage",
		},
		{
			name:      "live image different",
			liveImage: true,
			expected:  false,
			node: nodes.Node{
				InstanceInfo: map[string]interface{}{
					"boot_iso": "theimage",
				},
			},
			hostImage: "different",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ironic := testserver.NewIronic(t).WithDefaultResponses().Node(tc.node)
			ironic.Start()
			defer ironic.Stop()

			inspector := testserver.NewInspector(t).Ready().WithIntrospection(nodeUUID, introspection.Introspection{
				Finished: false,
			})
			inspector.Start()
			defer inspector.Stop()

			var host v1alpha1.BareMetalHost
			if tc.liveImage {
				host = makeHostLiveIso()
				host.Spec.Image.URL = tc.hostImage
			} else {
				host = makeHost()
				host.Spec.Image.URL = tc.hostImage
				host.Spec.Image.Checksum = tc.hostChecksum
				host.Spec.Image.ChecksumType = tc.hostChecksumType
			}
			host.Status.Provisioning.ID = nodeUUID
			publisher := func(reason, message string) {}
			auth := clients.AuthConfig{Type: clients.NoAuth}
			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, publisher,
				ironic.Endpoint(), auth, inspector.Endpoint(), auth,
			)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}

			sameImage := prov.ironicHasSameImage(&tc.node, *host.Spec.Image)
			assert.Equal(t, tc.expected, sameImage)
		})
	}
}

func TestBuildCleanSteps(t *testing.T) {

	var True bool = true
	var False bool = false

	nodeUUID := "33ce8659-7400-4c68-9535-d10766f07a58"
	cases := []struct {
		name             string
		ironic           *testserver.IronicMock
		currentSettings  v1alpha1.SettingsMap
		desiredSettings  v1alpha1.DesiredSettingsMap
		firmwareConfig   *v1alpha1.FirmwareConfig
		expectedSettings []map[string]string
	}{
		{
			name: "no current settings",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.DeployFail),
				UUID:           nodeUUID,
			}),
			currentSettings: nil,
			desiredSettings: nil,
			firmwareConfig: &v1alpha1.FirmwareConfig{
				VirtualizationEnabled:             &True,
				SimultaneousMultithreadingEnabled: &False,
			},
			expectedSettings: []map[string]string{
				{
					"name":  "ProcVirtualization",
					"value": "Enabled",
				},
				{
					"name":  "ProcHyperthreading",
					"value": "Disabled",
				},
			},
		},
		{
			name: "current settings same as bmc",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.DeployFail),
				UUID:           nodeUUID,
			}),
			currentSettings: map[string]string{
				"L2Cache":            "10x256 KB",
				"NumCores":           "10",
				"ProcVirtualization": "Enabled",
				"ProcHyperthreading": "Disabled",
			},
			desiredSettings: nil,
			firmwareConfig: &v1alpha1.FirmwareConfig{
				VirtualizationEnabled:             &True,
				SimultaneousMultithreadingEnabled: &False,
			},
			expectedSettings: nil,
		},
		{
			name: "current settings different than bmc",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.DeployFail),
				UUID:           nodeUUID,
			}),
			currentSettings: v1alpha1.SettingsMap{
				"L2Cache":            "10x256 KB",
				"NumCores":           "10",
				"ProcVirtualization": "Disabled",
				"ProcHyperthreading": "Enabled",
			},
			desiredSettings: nil,
			firmwareConfig: &v1alpha1.FirmwareConfig{
				VirtualizationEnabled:             &True,
				SimultaneousMultithreadingEnabled: &False,
			},
			expectedSettings: []map[string]string{
				{
					"name":  "ProcVirtualization",
					"value": "Enabled",
				},
				{
					"name":  "ProcHyperthreading",
					"value": "Disabled",
				},
			},
		},
		{
			name: "current settings same as bmc different than desired",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.DeployFail),
				UUID:           nodeUUID,
			}),
			currentSettings: v1alpha1.SettingsMap{
				"L2Cache":               "10x256 KB",
				"NumCores":              "10",
				"NetworkBootRetryCount": "20",
				"ProcVirtualization":    "Enabled",
				"ProcHyperthreading":    "Disabled",
			},
			desiredSettings: v1alpha1.DesiredSettingsMap{
				"NetworkBootRetryCount": intstr.FromString("10"),
				"ProcVirtualization":    intstr.FromString("Disabled"),
				"ProcHyperthreading":    intstr.FromString("Enabled"),
			},
			firmwareConfig: &v1alpha1.FirmwareConfig{
				VirtualizationEnabled:             &True,
				SimultaneousMultithreadingEnabled: &False,
			},
			expectedSettings: []map[string]string{
				{
					"name":  "NetworkBootRetryCount",
					"value": "10",
				},
				{
					"name":  "ProcVirtualization",
					"value": "Disabled",
				},
				{
					"name":  "ProcHyperthreading",
					"value": "Enabled",
				},
			},
		},
		{
			name: "current settings different than bmc and desired",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.DeployFail),
				UUID:           nodeUUID,
			}),
			currentSettings: v1alpha1.SettingsMap{
				"L2Cache":               "10x256 KB",
				"NumCores":              "10",
				"NetworkBootRetryCount": "20",
				"ProcVirtualization":    "Enabled",
				"ProcHyperthreading":    "Disabled",
			},
			desiredSettings: v1alpha1.DesiredSettingsMap{
				"NetworkBootRetryCount": intstr.FromString("5"),
				"ProcVirtualization":    intstr.FromString("Enabled"),
				"ProcHyperthreading":    intstr.FromString("Disabled"),
			},
			firmwareConfig: &v1alpha1.FirmwareConfig{
				VirtualizationEnabled:             &False,
				SimultaneousMultithreadingEnabled: &True,
			},
			expectedSettings: []map[string]string{
				{
					"name":  "ProcVirtualization",
					"value": "Disabled",
				},
				{
					"name":  "ProcHyperthreading",
					"value": "Enabled",
				},
				{
					"name":  "NetworkBootRetryCount",
					"value": "5",
				},
			},
		},
		{
			name: "bmc and desired duplicate settings",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(nodes.Node{
				ProvisionState: string(nodes.DeployFail),
				UUID:           nodeUUID,
			}),
			currentSettings: v1alpha1.SettingsMap{
				"L2Cache":            "10x256 KB",
				"NumCores":           "10",
				"ProcVirtualization": "Enabled",
				"ProcHyperthreading": "Disabled",
			},
			desiredSettings: v1alpha1.DesiredSettingsMap{
				"ProcVirtualization": intstr.FromString("Disabled"),
				"ProcHyperthreading": intstr.FromString("Enabled"),
			},
			firmwareConfig: &v1alpha1.FirmwareConfig{
				VirtualizationEnabled:             &False,
				SimultaneousMultithreadingEnabled: &True,
			},
			expectedSettings: []map[string]string{
				{
					"name":  "ProcHyperthreading",
					"value": "Enabled",
				},
				{
					"name":  "ProcVirtualization",
					"value": "Disabled",
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ironic != nil {
				tc.ironic.Start()
				defer tc.ironic.Stop()
			}

			inspector := testserver.NewInspector(t).Ready().WithIntrospection(nodeUUID, introspection.Introspection{
				Finished: false,
			})
			inspector.Start()
			defer inspector.Stop()

			host := makeHost()
			host.Status.Provisioning.ID = nodeUUID
			publisher := func(reason, message string) {}
			auth := clients.AuthConfig{Type: clients.NoAuth}
			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, publisher,
				tc.ironic.Endpoint(), auth, inspector.Endpoint(), auth,
			)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}

			parsedURL := &url.URL{Scheme: "redfish", Host: "10.1.1.1"}

			testBMC, _ := testbmc.NewTestBMCAccessDetails(parsedURL, false)

			cleanSteps, err := prov.buildManualCleaningSteps(testBMC, provisioner.PrepareData{
				FirmwareConfig:          tc.firmwareConfig,
				CurrentFirmwareSettings: tc.currentSettings,
				DesiredFirmwareSettings: tc.desiredSettings,
			})

			assert.Equal(t, nil, err)
			if cleanSteps == nil {
				assert.Equal(t, tc.expectedSettings, []map[string]string(nil))
			} else {
				settings := cleanSteps[0].Args["settings"]
				assert.ElementsMatch(t, tc.expectedSettings, settings)
			}
		})
	}
}
