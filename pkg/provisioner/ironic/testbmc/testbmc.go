package testbmc

import (
	"net/url"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/bmc"
)

func init() {
	bmc.RegisterFactory("test", newTestBMCAccessDetails, []string{})
	bmc.RegisterFactory("test-needs-mac", newTestBMCAccessDetails, []string{})
}

func newTestBMCAccessDetails(parsedURL *url.URL, disableCertificateVerification bool) (bmc.AccessDetails, error) {
	return &testAccessDetails{
		bmcType:                        parsedURL.Scheme,
		hostname:                       parsedURL.Hostname(),
		disableCertificateVerification: disableCertificateVerification,
	}, nil
}

type testAccessDetails struct {
	bmcType                        string
	hostname                       string
	disableCertificateVerification bool
}

func (a *testAccessDetails) Type() string {
	return a.bmcType
}

// NeedsMAC returns true when the host is going to need a separate
// port created rather than having it discovered.
func (a *testAccessDetails) NeedsMAC() bool {
	return a.bmcType == "test-needs-mac"
}

func (a *testAccessDetails) Driver() string {
	return "test"
}

func (a *testAccessDetails) DisableCertificateVerification() bool {
	return a.disableCertificateVerification
}

// DriverInfo returns a data structure to pass as the DriverInfo
// parameter when creating a node in Ironic. The structure is
// pre-populated with the access information, and the caller is
// expected to add any other information that might be needed (such as
// the kernel and ramdisk locations).
func (a *testAccessDetails) DriverInfo(bmcCreds bmc.Credentials) map[string]interface{} {
	result := map[string]interface{}{
		"test_port":     "42",
		"test_username": bmcCreds.Username,
		"test_password": bmcCreds.Password,
		"test_address":  a.hostname,
	}

	if a.disableCertificateVerification {
		result["test_verify_ca"] = false
	}
	return result
}

func (a *testAccessDetails) BootInterface() string {
	return "ipxe"
}

func (a *testAccessDetails) ManagementInterface() string {
	return ""
}

func (a *testAccessDetails) PowerInterface() string {
	return ""
}

func (a *testAccessDetails) RAIDInterface() string {
	return "no-raid"
}

func (a *testAccessDetails) VendorInterface() string {
	return ""
}

func (a *testAccessDetails) SupportsSecureBoot() bool {
	return false
}

func (a *testAccessDetails) SupportsISOPreprovisioningImage() bool {
	return false
}

func (a *testAccessDetails) BuildBIOSSettings(firmwareConfig *metal3v1alpha1.FirmwareConfig) (settings []map[string]string, err error) {
	return nil, nil
}
