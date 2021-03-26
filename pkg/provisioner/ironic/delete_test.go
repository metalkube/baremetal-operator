package ironic

import (
	"net/http"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
	"github.com/stretchr/testify/assert"

	"github.com/metal3-io/baremetal-operator/pkg/bmc"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/clients"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/ironic/testserver"
)

func TestDelete(t *testing.T) {

	nodeUUID := "33ce8659-7400-4c68-9535-d10766f07a58"

	cases := []struct {
		name      string
		ironic    *testserver.IronicMock
		inspector *testserver.InspectorMock
		hostName  string

		expectedDirty        bool
		expectedRequestAfter time.Duration
		expectedUpdate       *nodes.UpdateOperation

		expectedError string
	}{
		{
			name: "delete-host-fail",
			ironic: testserver.NewIronic(t).Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    true,
				},
			).DeleteError(nodeUUID, http.StatusInternalServerError),
			expectedError: "failed to remove host",
		},
		{
			name: "delete-host-busy",
			ironic: testserver.NewIronic(t).Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    true,
				},
			).DeleteError(nodeUUID, http.StatusConflict),
			expectedDirty:        true,
			expectedRequestAfter: provisionRequeueDelay,
		},
		{
			name: "delete-host-not-found",
			ironic: testserver.NewIronic(t).Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    true,
				},
			).DeleteError(nodeUUID, http.StatusNotFound),
			expectedDirty:        true,
			expectedRequestAfter: 0,
		},
		{
			name: "delete-ok",
			ironic: testserver.NewIronic(t).Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    true,
				},
			).Delete(nodeUUID),
			expectedDirty:        true,
			expectedRequestAfter: 0,
		},
		{
			name: "host-not-found",

			hostName: "worker-0",
			ironic:   testserver.NewIronic(t).Ready().NodeError(nodeUUID, http.StatusGatewayTimeout),

			expectedError: "failed to find existing host: failed to find node by ID 33ce8659-7400-4c68-9535-d10766f07a58: Expected HTTP response code \\[200\\].*",
		},
		{
			name:   "not-ironic-node",
			ironic: testserver.NewIronic(t).Ready().NoNode(nodeUUID).NoNode("myhost"),
		},
		{
			name: "available-node",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "available",
				},
			),
			expectedDirty:        true,
			expectedRequestAfter: provisionRequeueDelay,
		},
		{
			name: "not-in-maintenance-update-fail",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    false,
				},
			).NodeUpdateError(nodeUUID, http.StatusInternalServerError),

			expectedError: "failed to set host maintenance flag",
		},
		{
			name: "not-in-maintenance-update-busy",
			ironic: testserver.NewIronic(t).WithDefaultResponses().Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    false,
				},
			).NodeUpdateError(nodeUUID, http.StatusConflict),

			expectedDirty:        true,
			expectedRequestAfter: provisionRequeueDelay,
		},
		{
			name: "not-in-maintenance-update",
			ironic: testserver.NewIronic(t).Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    false,
				},
			).NodeUpdate(nodes.Node{
				UUID: nodeUUID,
			}),
			expectedDirty:        true,
			expectedRequestAfter: 0,
			expectedUpdate: &nodes.UpdateOperation{
				Op:    "replace",
				Path:  "/maintenance",
				Value: true,
			},
		},
		{
			name: "power-change-with-locked-host",
			ironic: testserver.NewIronic(t).Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    true,
					PowerState:     powerOn,
				},
			).WithNodeStatesPower(nodeUUID, http.StatusConflict).WithNodeStatesPowerUpdate(nodeUUID, http.StatusConflict),
			expectedDirty:        false,
			expectedRequestAfter: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ironic != nil {
				tc.ironic.Start()
				defer tc.ironic.Stop()
			}

			if tc.inspector != nil {
				tc.inspector.Start()
				defer tc.inspector.Stop()
			}

			host := makeHost()
			host.Status.Provisioning.ID = nodeUUID

			if tc.hostName != "" {
				host.Name = tc.hostName
			}

			auth := clients.AuthConfig{Type: clients.NoAuth}
			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
				tc.ironic.Endpoint(), auth, tc.inspector.Endpoint(), auth,
			)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}

			result, err := prov.Delete(true)

			assert.Equal(t, tc.expectedDirty, result.Dirty)
			assert.Equal(t, tc.expectedRequestAfter, result.RequeueAfter)

			if tc.expectedUpdate != nil {
				assert.Equal(t, *tc.expectedUpdate, tc.ironic.GetLastNodeUpdateRequestFor(nodeUUID)[0])
			}

			if tc.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Regexp(t, tc.expectedError, err.Error())
			}
		})
	}
}

func TestDeleteWithPowerOff(t *testing.T) {

	nodeUUID := "33ce8659-7400-4c68-9535-d10766f07a58"

	cases := []struct {
		name      string
		ironic    *testserver.IronicMock
		inspector *testserver.InspectorMock
		retryFlag bool

		expectedPowerStateInRequest string
		expectedError               string
	}{
		{
			name: "power-change-without-retry",
			ironic: testserver.NewIronic(t).Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    true,
					PowerState:     powerOn,
				},
			).Delete(nodeUUID),
			retryFlag:                   false,
			expectedPowerStateInRequest: "",
			expectedError:               "",
		},
		{
			name: "power-change-with-retry",
			ironic: testserver.NewIronic(t).Node(
				nodes.Node{
					UUID:           nodeUUID,
					ProvisionState: "active",
					Maintenance:    true,
					PowerState:     powerOn,
				},
			).Delete(nodeUUID),
			retryFlag:                   true,
			expectedPowerStateInRequest: "power off",
			expectedError:               "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ironic != nil {
				tc.ironic.Start()
				defer tc.ironic.Stop()
			}

			if tc.inspector != nil {
				tc.inspector.Start()
				defer tc.inspector.Stop()
			}

			host := makeHost()
			host.Status.Provisioning.ID = nodeUUID

			auth := clients.AuthConfig{Type: clients.NoAuth}
			prov, err := newProvisionerWithSettings(host, bmc.Credentials{}, nullEventPublisher,
				tc.ironic.Endpoint(), auth, tc.inspector.Endpoint(), auth,
			)
			if err != nil {
				t.Fatalf("could not create provisioner: %s", err)
			}
			prov.status.ID = nodeUUID

			_, err = prov.Delete(tc.retryFlag)

			updates, _ := tc.ironic.GetLastRequestFor("/v1/nodes/"+nodeUUID+"/states/power", http.MethodPut)
			assert.Contains(t, updates, tc.expectedPowerStateInRequest)

			if tc.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Regexp(t, tc.expectedError, err.Error())
			}

		})
	}
}
