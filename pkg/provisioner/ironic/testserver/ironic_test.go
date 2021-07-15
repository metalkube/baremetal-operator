package testserver

import (
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/baremetal/v1/nodes"
)

func TestIronicDatabaseClearing(t *testing.T) {
	ironic := NewIronic(t).WithDefaultResponses()
	ironic.AddDefaultResponse("/v1/nodes", "POST", http.StatusCreated, "{}")
	ironic.Start()
	defer ironic.Stop()

	endpoint := ironic.Endpoint()

	url := endpoint + "nodes/uuid"

	resp, err := http.Get(url) // #nosec

	if err != nil {
		t.Error(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fail()
	}

	ironic.ClearDatabase()

	resp, err = http.Get(url) // #nosec

	if err != nil {
		t.Error(err)
	}

	if resp.StatusCode != http.StatusNotFound {
		t.Fail()
	}

	// After clearing the db, POSTs should still work
	resp, err = http.PostForm(endpoint+"nodes", nil)

	if err != nil {
		t.Error(err)
	}

	if resp.StatusCode != http.StatusCreated {
		t.Fail()
	}
}

func TestIronicDatabaseClearingNode(t *testing.T) {
	ironic := NewIronic(t).WithDefaultResponses().Node(nodes.Node{
		UUID: "abc",
	})
	ironic.AddDefaultResponse("/v1/nodes", "POST", http.StatusCreated, "{}")
	ironic.Start()
	defer ironic.Stop()

	endpoint := ironic.Endpoint()

	url := endpoint + "nodes/uuid"

	resp, err := http.Get(url) // #nosec

	if err != nil {
		t.Error(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fail()
	}

	ironic.ClearDatabase()

	resp, err = http.Get(url) // #nosec

	if err != nil {
		t.Error(err)
	}

	if resp.StatusCode != http.StatusNotFound {
		t.Fail()
	}

	// After clearing the db, POSTs should still work
	resp, err = http.PostForm(endpoint+"nodes", nil)

	if err != nil {
		t.Error(err)
	}

	if resp.StatusCode != http.StatusCreated {
		t.Fail()
	}
}
