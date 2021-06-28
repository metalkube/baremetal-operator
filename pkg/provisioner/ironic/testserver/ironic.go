package testserver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/ports"
)

// IronicMock is a test server that implements Ironic's semantics
type IronicMock struct {
	*MockServer
	CreatedNodes int
}

// NewIronic builds an ironic mock server
func NewIronic(t *testing.T) *IronicMock {

	return &IronicMock{
		MockServer:   New(t, "ironic"),
		CreatedNodes: 0,
	}
}

// WithDefaultResponses sets a valid answer for all the API calls
func (m *IronicMock) WithDefaultResponses() *IronicMock {
	m.AddDefaultResponseJSON("/v1/nodes/{id}", "", http.StatusOK, nodes.Node{
		UUID: "{id}",
	})
	m.AddDefaultResponse("/v1/nodes/{id}/states/provision", "", http.StatusAccepted, "{}")
	m.AddDefaultResponse("/v1/nodes/{id}/states/power", "", http.StatusAccepted, "{}")
	m.AddDefaultResponse("/v1/nodes/{id}/states/raid", "", http.StatusNoContent, "{}")
	m.AddDefaultResponse("/v1/nodes/{id}/validate", "", http.StatusOK, "{}")
	m.Ready()

	return m
}

// Endpoint returns the URL for accessing the server
func (m *IronicMock) Endpoint() string {
	if m == nil {
		return "https://ironic.test/v1/"
	}
	return m.MockServer.Endpoint()
}

// Ready configures the server with a valid response for /v1
func (m *IronicMock) Ready() *IronicMock {
	m.ResponseWithCode("/v1", "{}", http.StatusOK)
	return m
}

// NotReady configures the server with an error response for /v1
func (m *IronicMock) NotReady(errorCode int) *IronicMock {
	m.ErrorResponse("/v1", errorCode)
	return m
}

// WithDrivers configures the server so /v1/drivers returns a valid value
func (m *IronicMock) WithDrivers() *IronicMock {
	m.ResponseWithCode("/v1/drivers", `
	{
		"drivers": [{
			"hosts": [
			  "master-2.ostest.test.metalkube.org"
			],
			"links": [
			  {
				"href": "http://[fd00:1101::3]:6385/v1/drivers/fake-hardware",
				"rel": "self"
			  },
			  {
				"href": "http://[fd00:1101::3]:6385/drivers/fake-hardware",
				"rel": "bookmark"
			  }
			],
			"name": "fake-hardware"
		}]
	}
	`, http.StatusOK)
	return m
}

func (m *IronicMock) buildURL(url string, method string) string {
	return fmt.Sprintf("%s:%s", url, method)
}

// Delete configures the server with a valid response for [DELETE] /v1/nodes/ on the
// specific node id
func (m *IronicMock) Delete(id string) *IronicMock {
	m.ResponseWithCode(m.buildURL("/v1/nodes/"+id, http.MethodDelete), "", http.StatusAccepted)
	return m
}

// DeleteError configures the server with an error response for [DELETE] /v1/nodes/
func (m *IronicMock) DeleteError(id string, errorCode int) *IronicMock {
	m.ResponseWithCode(m.buildURL("/v1/nodes/"+id, http.MethodDelete), "", errorCode)
	return m
}

// Node configures the server with a valid response for /v1/nodes/{name,uuid}
func (m *IronicMock) Node(node nodes.Node) *IronicMock {
	if node.UUID != "" {
		m.ResponseJSON(m.buildURL("/v1/nodes/"+node.UUID, http.MethodGet), node)
	}
	if node.Name != "" {
		m.ResponseJSON(m.buildURL("/v1/nodes/"+node.Name, http.MethodGet), node)
	}
	return m
}

// NodeUpdateError configures configures the server with an error response for [PATCH] /v1/nodes/{id}
func (m *IronicMock) NodeUpdateError(id string, errorCode int) *IronicMock {
	m.ResponseWithCode(m.buildURL("/v1/nodes/"+id, http.MethodPatch), "", errorCode)
	return m
}

// NodeUpdate configures the server with a valid response for PATCH
// for /v1/nodes/{name,uuid}
func (m *IronicMock) NodeUpdate(node nodes.Node) *IronicMock {
	if node.UUID != "" {
		m.ResponseJSON(m.buildURL("/v1/nodes/"+node.UUID, http.MethodPatch), node)
	}
	if node.Name != "" {
		m.ResponseJSON(m.buildURL("/v1/nodes/"+node.Name, http.MethodPatch), node)
	}
	return m
}

//GetLastNodeUpdateRequestFor returns the content of the last update request for the specified node
func (m *IronicMock) GetLastNodeUpdateRequestFor(id string) (updates []nodes.UpdateOperation) {

	if bodyRaw, ok := m.GetLastRequestFor("/v1/nodes/"+id, http.MethodPatch); ok {
		json.Unmarshal([]byte(bodyRaw), &updates)
	}

	return
}

func (m *IronicMock) withNodeStatesProvision(nodeUUID string, method string) *IronicMock {
	m.ResponseWithCode(m.buildURL("/v1/nodes/"+nodeUUID+"/states/provision", method), "{}", http.StatusAccepted)
	return m
}

// WithNodeStatesProvision configures the server with a valid response for [GET] /v1/nodes/<node>/states/provision
func (m *IronicMock) WithNodeStatesProvision(nodeUUID string) *IronicMock {
	return m.withNodeStatesProvision(nodeUUID, http.MethodGet)
}

// WithNodeStatesProvisionUpdate configures the server with a valid response for [PATCH] /v1/nodes/<node>/states/provision
func (m *IronicMock) WithNodeStatesProvisionUpdate(nodeUUID string) *IronicMock {
	return m.withNodeStatesProvision(nodeUUID, http.MethodPut)
}

// NoNode configures the server so /v1/nodes/name returns a 404
func (m *IronicMock) NoNode(name string) *IronicMock {
	return m.NodeError(name, http.StatusNotFound)
}

// NodeError configures the server to return the specified error code for /v1/nodes/name
func (m *IronicMock) NodeError(name string, errorCode int) *IronicMock {
	m.ErrorResponse(fmt.Sprintf("/v1/nodes/%s", name), errorCode)
	return m
}

// NodeCreateCallback type is the callback mock for CreateNodes
type NodeCreateCallback func(node nodes.Node)

// CreateNodes configures the server so POSTing to /v1/nodes saves the data
func (m *IronicMock) CreateNodes(callback NodeCreateCallback) *IronicMock {
	m.Handler("/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, fmt.Sprintf("%s not handled for %s", r.Method, r.URL),
				http.StatusNotImplemented)
		}

		bodyRaw, err := ioutil.ReadAll(r.Body)
		if err != nil {
			m.logRequest(r, fmt.Sprintf("ERROR: %s", err))
			http.Error(w, fmt.Sprintf("%s", err), http.StatusInternalServerError)
			return
		}

		body := string(bodyRaw)
		m.t.Logf("%s: create nodes request %v", m.name, body)

		// Unpack the input so we can update it
		node := nodes.Node{}
		err = json.Unmarshal(bodyRaw, &node)
		if err != nil {
			m.logRequest(r, fmt.Sprintf("ERROR: %s", err))
			http.Error(w, fmt.Sprintf("%s", err), http.StatusInternalServerError)
			return
		}

		// The UUID value doesn't actually have to be a UUID, so we
		// just make a new string based on the count of nodes already
		// created.
		node.UUID = fmt.Sprintf("node-%d", m.CreatedNodes)
		m.t.Logf("%s: uuid %s", m.name, node.UUID)
		m.CreatedNodes++

		// Pass the data to the test via the callback
		callback(node)

		// Handle the response to this request
		m.SendJSONResponse(node, http.StatusCreated, w, r)
	})
	return m
}

func (m *IronicMock) withNodeStatesPower(nodeUUID string, code int, method string) *IronicMock {
	m.ResponseWithCode(m.buildURL("/v1/nodes/"+nodeUUID+"/states/power", method), "{}", code)
	return m
}

// WithNodeStatesPower configures the server with a valid response for [GET] /v1/nodes/<node>/states/power
func (m *IronicMock) WithNodeStatesPower(nodeUUID string, code int) *IronicMock {
	return m.withNodeStatesPower(nodeUUID, code, http.MethodGet)
}

// WithNodeStatesPowerUpdate configures the server with a valid response for [PUT] /v1/nodes/<node>/states/power
func (m *IronicMock) WithNodeStatesPowerUpdate(nodeUUID string, code int) *IronicMock {
	return m.withNodeStatesPower(nodeUUID, code, http.MethodPut)
}

// WithNodeValidate configures the server with a valid response for /v1/nodes/<node>/validate
func (m *IronicMock) WithNodeValidate(nodeUUID string) *IronicMock {
	m.ResponseWithCode("/v1/nodes/"+nodeUUID+"/validate", "{}", http.StatusOK)
	return m
}

// Port configures the server with a valid response for
//    [GET] /v1/nodes/<node uuid>/ports
//    [GET] /v1/ports
//    [GET] /v1/ports?address=<mac>
//    [GET] /v1/ports?address=<mac>&fields=node_uuid
func (m *IronicMock) Port(port ports.Port) *IronicMock {
	if port.NodeUUID == "" {
		m.MockServer.t.Error("When using withPort(), the port must include a NodeUUID.")
	}

	resp := map[string][]ports.Port{
		"ports": {port},
	}

	address := url.QueryEscape(port.Address)

	m.ResponseJSON(m.buildURL("/v1/nodes/"+port.NodeUUID+"/ports", http.MethodGet), resp)
	m.ResponseJSON(m.buildURL("/v1/ports", http.MethodGet), resp)
	m.ResponseJSON(m.buildURL("/v1/ports?address="+address, http.MethodGet), resp)
	m.ResponseJSON(m.buildURL("/v1/ports?address="+address+"&fields=node_uuid", http.MethodGet), resp)

	return m
}

// Nodes configure the server with a valid response for /v1/nodes
func (m *IronicMock) Nodes(allNodes []nodes.Node) *IronicMock {
	resp := struct {
		Nodes []nodes.Node `json:"nodes"`
	}{
		Nodes: allNodes,
	}

	m.ResponseJSON(m.buildURL("/v1/nodes", http.MethodGet), resp)
	return m
}

// BIOSSettings configure the server with a valid response for /v1/nodes/<node>/bios
func (m *IronicMock) BIOSSettings(nodeUUID string) *IronicMock {
	settings := []nodes.BIOSSetting{
		{
			Name:  "L2Cache",
			Value: "10x256 KB",
		},
		{
			Name:  "NumCores",
			Value: "10",
		},
		{
			Name:  "ProcVirtualization",
			Value: "Enabled",
		},
	}

	resp := struct {
		Settings []nodes.BIOSSetting `json:"bios"`
	}{
		Settings: settings,
	}

	m.ResponseJSON(m.buildURL("/v1/nodes/"+nodeUUID+"/bios", http.MethodGet), resp)
	return m
}

// BIOSDetailSettings configure the server with a valid response for /v1/nodes/<node>/bios?detail=True
func (m *IronicMock) BIOSDetailSettings(nodeUUID string) *IronicMock {

	iTrue := true
	iFalse := false
	minLength := 0
	maxLength := 16
	lowerBound := 0
	upperBound := 20

	settings := []nodes.BIOSSetting{
		{
			Name:            "L2Cache",
			Value:           "10x256 KB",
			AttributeType:   "String",
			AllowableValues: []string{},
			LowerBound:      nil,
			UpperBound:      nil,
			MinLength:       &minLength,
			MaxLength:       &maxLength,
			ReadOnly:        &iTrue,
			ResetRequired:   nil,
			Unique:          nil,
		},
		{
			Name:            "NumCores",
			Value:           "10",
			AttributeType:   "Integer",
			AllowableValues: []string{},
			LowerBound:      &lowerBound,
			UpperBound:      &upperBound,
			MinLength:       nil,
			MaxLength:       nil,
			ReadOnly:        &iTrue,
			ResetRequired:   nil,
			Unique:          nil,
		},
		{
			Name:            "ProcVirtualization",
			Value:           "Enabled",
			AttributeType:   "Enumeration",
			AllowableValues: []string{"Enabled", "Disabled"},
			LowerBound:      nil,
			UpperBound:      nil,
			MinLength:       nil,
			MaxLength:       nil,
			ReadOnly:        &iFalse,
			ResetRequired:   nil,
			Unique:          nil,
		},
	}

	resp := struct {
		Settings []nodes.BIOSSetting `json:"bios"`
	}{
		Settings: settings,
	}

	m.ResponseJSON(m.buildURL("/v1/nodes/"+nodeUUID+"/bios", http.MethodGet), resp)
	return m
}

// NoBIOS configures the server so /v1/node/<node>/bios returns a 404
func (m *IronicMock) NoBIOS(nodeUUID string) *IronicMock {
	return m.NodeError(nodeUUID, http.StatusNotFound)
}
