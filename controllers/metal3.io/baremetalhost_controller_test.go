package controllers

import (
	"context"
	goctx "context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/bmc"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/fixture"
	"github.com/metal3-io/baremetal-operator/pkg/utils"
)

const (
	namespace         string = "test-namespace"
	defaultSecretName string = "bmc-creds-valid" // #nosec
	statusAnnotation  string = `{"operationalStatus":"OK","lastUpdated":"2020-04-15T15:00:50Z","hardwareProfile":"StatusProfile","hardware":{"systemVendor":{"manufacturer":"QEMU","productName":"Standard PC (Q35 + ICH9, 2009)","serialNumber":""},"firmware":{"bios":{"date":"","vendor":"","version":""}},"ramMebibytes":4096,"nics":[{"name":"eth0","model":"0x1af4 0x0001","mac":"00:b7:8b:bb:3d:f6","ip":"172.22.0.64","speedGbps":0,"vlanId":0,"pxe":true},{"name":"eth1","model":"0x1af4  0x0001","mac":"00:b7:8b:bb:3d:f8","ip":"192.168.111.20","speedGbps":0,"vlanId":0,"pxe":false}],"storage":[{"name":"/dev/sda","rotational":true,"sizeBytes":53687091200,"vendor":"QEMU","model":"QEMU HARDDISK","serialNumber":"drive-scsi0-0-0-0","hctl":"6:0:0:0"}],"cpu":{"arch":"x86_64","model":"Intel Xeon E3-12xx v2 (IvyBridge)","clockMegahertz":2494.224,"flags":["aes","apic","arat","avx","clflush","cmov","constant_tsc","cx16","cx8","de","eagerfpu","ept","erms","f16c","flexpriority","fpu","fsgsbase","fxsr","hypervisor","lahf_lm","lm","mca","mce","mmx","msr","mtrr","nopl","nx","pae","pat","pclmulqdq","pge","pni","popcnt","pse","pse36","rdrand","rdtscp","rep_good","sep","smep","sse","sse2","sse4_1","sse4_2","ssse3","syscall","tpr_shadow","tsc","tsc_adjust","tsc_deadline_timer","vme","vmx","vnmi","vpid","x2apic","xsave","xsaveopt","xtopology"],"count":4},"hostname":"node-0"},"provisioning":{"state":"provisioned","ID":"8a0ede17-7b87-44ac-9293-5b7d50b94b08","image":{"url":"bar","checksum":""}},"goodCredentials":{"credentials":{"name":"node-0-bmc-secret","namespace":"metal3"},"credentialsVersion":"879"},"triedCredentials":{"credentials":{"name":"node-0-bmc-secret","namespace":"metal3"},"credentialsVersion":"879"},"errorMessage":"","poweredOn":true,"operationHistory":{"register":{"start":"2020-04-15T12:06:26Z","end":"2020-04-15T12:07:12Z"},"inspect":{"start":"2020-04-15T12:07:12Z","end":"2020-04-15T12:09:29Z"},"provision":{"start":null,"end":null},"deprovision":{"start":null,"end":null}}}`
	hwdAnnotation     string = `{"systemVendor":{"manufacturer":"QEMU","productName":"Standard PC (Q35 + ICH9, 2009)","serialNumber":""},"firmware":{"bios":{"date":"","vendor":"","version":""}},"ramMebibytes":4096,"nics":[{"name":"eth0","model":"0x1af4 0x0001","mac":"00:b7:8b:bb:3d:f6","ip":"172.22.0.64","speedGbps":0,"vlanId":0,"pxe":true}],"storage":[{"name":"/dev/sda","rotational":true,"sizeBytes":53687091200,"vendor":"QEMU","model":"QEMU HARDDISK","serialNumber":"drive-scsi0-0-0-0","hctl":"6:0:0:0"}],"cpu":{"arch":"x86_64","model":"Intel Xeon E3-12xx v2 (IvyBridge)","clockMegahertz":2494.224,"flags":["foo"],"count":4},"hostname":"hwdAnnotation-0"}`
)

func newSecret(name string, data map[string]string) *corev1.Secret {
	secretData := make(map[string][]byte)
	for k, v := range data {
		secretData[k] = []byte(base64.StdEncoding.EncodeToString([]byte(v)))
	}

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: secretData,
	}

	return secret
}

func newBMCCredsSecret(name, username, password string) *corev1.Secret {
	return newSecret(name, map[string]string{"username": username, "password": password})
}

func newHost(name string, spec *metal3v1alpha1.BareMetalHostSpec) *metal3v1alpha1.BareMetalHost {
	return &metal3v1alpha1.BareMetalHost{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BareMetalHost",
			APIVersion: "metal3.io/v1alpha1",
		}, ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: *spec,
	}
}

func newDefaultNamedHost(name string, t *testing.T) *metal3v1alpha1.BareMetalHost {
	spec := &metal3v1alpha1.BareMetalHostSpec{
		BMC: metal3v1alpha1.BMCDetails{
			Address:         "ipmi://192.168.122.1:6233",
			CredentialsName: defaultSecretName,
		},
	}
	t.Logf("newNamedHost(%s)", name)
	return newHost(name, spec)
}

func newDefaultHost(t *testing.T) *metal3v1alpha1.BareMetalHost {
	return newDefaultNamedHost(t.Name(), t)
}

func newTestReconcilerWithFixture(fix *fixture.Fixture, initObjs ...runtime.Object) *BareMetalHostReconciler {

	c := fakeclient.NewFakeClient(initObjs...)

	// Add a default secret that can be used by most hosts.
	bmcSecret := newBMCCredsSecret(defaultSecretName, "User", "Pass")
	c.Create(goctx.TODO(), bmcSecret)

	return &BareMetalHostReconciler{
		Client:             c,
		ProvisionerFactory: fix,
		Log:                ctrl.Log.WithName("controllers").WithName("BareMetalHost"),
		APIReader:          c,
	}
}

func newTestReconciler(initObjs ...runtime.Object) *BareMetalHostReconciler {
	fix := fixture.Fixture{}
	return newTestReconcilerWithFixture(&fix, initObjs...)
}

type DoneFunc func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool

func newRequest(host *metal3v1alpha1.BareMetalHost) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: host.ObjectMeta.Namespace,
		Name:      host.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func tryReconcile(t *testing.T, r *BareMetalHostReconciler, host *metal3v1alpha1.BareMetalHost, isDone DoneFunc) {

	request := newRequest(host)

	for i := 0; ; i++ {
		t.Logf("tryReconcile: top of loop %d", i)
		if i >= 25 {
			t.Fatal(fmt.Errorf("Exceeded 25 iterations"))
		}

		result, err := r.Reconcile(context.Background(), request)

		if err != nil {
			t.Fatal(err)
			break
		}

		// The FakeClient keeps a copy of the object we update, so we
		// need to replace the one we have with the updated data in
		// order to test it. In case it was not found, let's set it to nil
		updatedHost := &metal3v1alpha1.BareMetalHost{}
		if err = r.Get(goctx.TODO(), request.NamespacedName, updatedHost); errors.IsNotFound(err) {
			host = nil
		} else {
			updatedHost.DeepCopyInto(host)
		}

		if isDone(host, result) {
			t.Logf("tryReconcile: loop done %d", i)
			break
		}

		t.Logf("tryReconcile: loop bottom %d result=%v", i, result)
		if !result.Requeue && result.RequeueAfter == 0 {
			t.Fatal(fmt.Errorf("Ended reconcile at iteration %d without test condition being true", i))
			break
		}
	}
}

func waitForStatus(t *testing.T, r *BareMetalHostReconciler, host *metal3v1alpha1.BareMetalHost, desiredStatus metal3v1alpha1.OperationalStatus) {
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			state := host.OperationalStatus()
			t.Logf("Waiting for status %s: %s", desiredStatus, state)
			return state == desiredStatus
		},
	)
}

func waitForError(t *testing.T, r *BareMetalHostReconciler, host *metal3v1alpha1.BareMetalHost) {
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("Waiting for error: %q", host.Status.ErrorMessage)
			return host.Status.ErrorMessage != ""
		},
	)
}

func waitForNoError(t *testing.T, r *BareMetalHostReconciler, host *metal3v1alpha1.BareMetalHost) {
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("Waiting for no error message: %q", host.Status.ErrorMessage)
			return host.Status.ErrorMessage == ""
		},
	)
}

func waitForProvisioningState(t *testing.T, r *BareMetalHostReconciler, host *metal3v1alpha1.BareMetalHost, desiredState metal3v1alpha1.ProvisioningState) {
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("Waiting for state %q have state %q", desiredState, host.Status.Provisioning.State)
			return host.Status.Provisioning.State == desiredState
		},
	)
}

// TestHardwareDetails_EmptyStatus ensures that hardware details in
// the status field are populated when the hardwaredetails annotation
// is present and no existing HarwareDetails are present
func TestHardwareDetails_EmptyStatus(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = map[string]string{
		hardwareDetailsAnnotation: hwdAnnotation,
	}

	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			_, found := host.Annotations[hardwareDetailsAnnotation]
			if host.Status.HardwareDetails != nil && host.Status.HardwareDetails.Hostname == "hwdAnnotation-0" && !found {
				return true
			}
			return false
		},
	)
}

// TestHardwareDetails_StatusPresent ensures that hardware details in
// the hardwaredetails annotation is ignored with existing Status
func TestHardwareDetails_StatusPresent(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = map[string]string{
		hardwareDetailsAnnotation: hwdAnnotation,
	}
	time := metav1.Now()
	host.Status.LastUpdated = &time
	hwd := metal3v1alpha1.HardwareDetails{}
	hwd.Hostname = "existinghost"
	host.Status.HardwareDetails = &hwd

	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			_, found := host.Annotations[hardwareDetailsAnnotation]
			if host.Status.HardwareDetails != nil && host.Status.HardwareDetails.Hostname == "existinghost" && !found {
				return true
			}
			return false
		},
	)
}

// TestHardwareDetails_StatusPresentInspectDisabled ensures that
// hardware details in the hardwaredetails annotation are consumed
// even when existing status exists, when inspection is disabled
func TestHardwareDetails_StatusPresentInspectDisabled(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = map[string]string{
		inspectAnnotationPrefix:   "disabled",
		hardwareDetailsAnnotation: hwdAnnotation,
	}
	time := metav1.Now()
	host.Status.LastUpdated = &time
	hwd := metal3v1alpha1.HardwareDetails{}
	hwd.Hostname = "existinghost"
	host.Status.HardwareDetails = &hwd

	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			_, found := host.Annotations[hardwareDetailsAnnotation]
			if host.Status.HardwareDetails != nil && host.Status.HardwareDetails.Hostname == "hwdAnnotation-0" && !found {
				return true
			}
			return false
		},
	)
}

// TestHardwareDetails_Invalid
// Tests scenario where the hardwaredetails value is invalid
func TestHardwareDetails_Invalid(t *testing.T) {
	host := newDefaultHost(t)
	badAnnotation := fmt.Sprintf("{\"hardware\": %s}", hwdAnnotation)
	host.Annotations = map[string]string{
		inspectAnnotationPrefix:   "disabled",
		hardwareDetailsAnnotation: badAnnotation,
	}
	time := metav1.Now()
	host.Status.LastUpdated = &time
	hwd := metal3v1alpha1.HardwareDetails{}
	hwd.Hostname = "existinghost"
	host.Status.HardwareDetails = &hwd

	r := newTestReconciler(host)
	request := newRequest(host)
	_, err := r.Reconcile(context.Background(), request)
	expectedErr := "json: unknown field"
	assert.Contains(t, err.Error(), expectedErr)
}

// TestStatusAnnotation_EmptyStatus ensures that status is manually populated
// when status annotation is present and status field is empty.
func TestStatusAnnotation_EmptyStatus(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = map[string]string{
		metal3v1alpha1.StatusAnnotation: statusAnnotation,
	}
	host.Spec.Online = true
	host.Spec.Image = &metal3v1alpha1.Image{URL: "foo", Checksum: "123"}

	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			if host.Status.HardwareProfile == "StatusProfile" && host.Status.Provisioning.Image.URL == "bar" {
				return true
			}
			return false
		},
	)
}

// TestStatusAnnotation_StatusPresent tests that if status is present
// status annotation is ignored and deleted.
func TestStatusAnnotation_StatusPresent(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = map[string]string{
		metal3v1alpha1.StatusAnnotation: statusAnnotation,
	}
	host.Spec.Online = true
	time := metav1.Now()
	host.Status.LastUpdated = &time
	host.Status.Provisioning.Image = metal3v1alpha1.Image{URL: "foo", Checksum: "123"}
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			_, found := host.Annotations[metal3v1alpha1.StatusAnnotation]
			if host.Status.HardwareProfile != "StatusProfile" && host.Status.Provisioning.Image.URL == "foo" && !found {
				return true
			}
			return false
		},
	)
}

// TestStatusAnnotation_Partial ensures that if the status annotation
// does not include the LastUpdated value reconciliation does not go
// into an infinite loop.
func TestStatusAnnotation_Partial(t *testing.T) {
	// Build a version of the annotation text that does not include
	// a LastUpdated value.
	unpackedStatus, err := unmarshalStatusAnnotation([]byte(statusAnnotation))
	if err != nil {
		t.Fatal(err)
		return
	}
	unpackedStatus.LastUpdated = nil
	packedStatus, err := json.Marshal(unpackedStatus)
	if err != nil {
		t.Fatal(err)
		return
	}

	host := newDefaultHost(t)
	host.Annotations = map[string]string{
		metal3v1alpha1.StatusAnnotation: string(packedStatus),
	}
	host.Spec.Online = true
	host.Spec.Image = &metal3v1alpha1.Image{URL: "foo", Checksum: "123"}

	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			if host.Status.HardwareProfile == "StatusProfile" && host.Status.Provisioning.Image.URL == "bar" {
				return true
			}
			return false
		},
	)
}

// TestPause ensures that the requeue happens when the pause annotation is there.
func TestPause(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = map[string]string{
		metal3v1alpha1.PausedAnnotation: "true",
	}
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			// Because the host is created with the annotation, we
			// expect it to never have any reconciling done at all, so
			// it has no finalizer.
			t.Logf("requeue: %v  finalizers: %v", result.Requeue, host.Finalizers)
			if !result.Requeue && len(host.Finalizers) == 0 {
				return true
			}
			return false
		},
	)
}

// TestInspectDisabled ensures that Inspection is skipped when disabled
func TestInspectDisabled(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = map[string]string{
		inspectAnnotationPrefix: "disabled",
	}
	r := newTestReconciler(host)
	waitForProvisioningState(t, r, host, metal3v1alpha1.StateMatchProfile)
	assert.Nil(t, host.Status.HardwareDetails)
}

// TestInspectEnabled ensures that Inspection is completed when not disabled
func TestInspectEnabled(t *testing.T) {
	host := newDefaultHost(t)
	r := newTestReconciler(host)
	waitForProvisioningState(t, r, host, metal3v1alpha1.StateMatchProfile)
	assert.NotNil(t, host.Status.HardwareDetails)
}

// TestAddFinalizers ensures that the finalizers for the host are
// updated as part of reconciling it.
func TestAddFinalizers(t *testing.T) {
	host := newDefaultHost(t)
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("finalizers: %v", host.ObjectMeta.Finalizers)
			if utils.StringInList(host.ObjectMeta.Finalizers, metal3v1alpha1.BareMetalHostFinalizer) {
				return true
			}
			return false
		},
	)
}

// TestSetLastUpdated ensures that the lastUpdated timestamp in the
// status is set to a non-zero value during reconciliation.
func TestSetLastUpdated(t *testing.T) {
	host := newDefaultHost(t)
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("LastUpdated: %v", host.Status.LastUpdated)
			if !host.Status.LastUpdated.IsZero() {
				return true
			}
			return false
		},
	)
}

func TestInspectionDisabledAnnotation(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = make(map[string]string)

	assert.False(t, inspectionDisabled(host))

	host.Annotations[inspectAnnotationPrefix] = "disabled"
	assert.True(t, inspectionDisabled(host))
}

func makeReconcileInfo(host *metal3v1alpha1.BareMetalHost) *reconcileInfo {
	return &reconcileInfo{
		log:  logf.Log.WithName("controllers").WithName("BareMetalHost").WithName("baremetal_controller"),
		host: host,
	}
}

func TestHasRebootAnnotation(t *testing.T) {
	host := newDefaultHost(t)
	info := makeReconcileInfo(host)
	host.Annotations = make(map[string]string)

	hasReboot, rebootMode := hasRebootAnnotation(info)
	assert.False(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeSoft, rebootMode)

	host.Annotations = make(map[string]string)
	suffixedAnnotation := rebootAnnotationPrefix + "/foo"
	host.Annotations[suffixedAnnotation] = ""

	hasReboot, rebootMode = hasRebootAnnotation(info)
	assert.True(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeSoft, rebootMode)

	delete(host.Annotations, suffixedAnnotation)
	host.Annotations[rebootAnnotationPrefix] = ""

	hasReboot, rebootMode = hasRebootAnnotation(info)
	assert.True(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeSoft, rebootMode)

	host.Annotations[suffixedAnnotation] = ""

	hasReboot, rebootMode = hasRebootAnnotation(info)
	assert.True(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeSoft, rebootMode)

	//two suffixed annotations to simulate multiple clients

	host.Annotations[suffixedAnnotation+"bar"] = ""

	hasReboot, rebootMode = hasRebootAnnotation(info)
	assert.True(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeSoft, rebootMode)
}

func TestHasHardRebootAnnotation(t *testing.T) {
	host := newDefaultHost(t)
	info := makeReconcileInfo(host)
	host.Annotations = make(map[string]string)

	hasReboot, rebootMode := hasRebootAnnotation(info)
	assert.False(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeSoft, rebootMode)

	host.Annotations = make(map[string]string)
	suffixedAnnotation := rebootAnnotationPrefix + "/foo/"
	host.Annotations[suffixedAnnotation] = "{\"mode\": \"hard\"}"

	hasReboot, rebootMode = hasRebootAnnotation(info)
	assert.True(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeHard, rebootMode)

	delete(host.Annotations, suffixedAnnotation)
	host.Annotations[rebootAnnotationPrefix] = "{\"mode\": \"soft\"}"

	hasReboot, rebootMode = hasRebootAnnotation(info)
	assert.True(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeSoft, rebootMode)

	delete(host.Annotations, suffixedAnnotation)
	host.Annotations[rebootAnnotationPrefix] = "{\"bad\"= \"json\"]"

	hasReboot, rebootMode = hasRebootAnnotation(info)
	assert.True(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeSoft, rebootMode)

	host.Annotations[suffixedAnnotation] = ""

	hasReboot, rebootMode = hasRebootAnnotation(info)
	assert.True(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeSoft, rebootMode)

	//two suffixed annotations to simulate multiple clients

	host.Annotations[suffixedAnnotation+"bar"] = "{\"mode\": \"hard\"}"

	hasReboot, rebootMode = hasRebootAnnotation(info)
	assert.True(t, hasReboot)
	assert.Equal(t, metal3v1alpha1.RebootModeHard, rebootMode)
}

// TestRebootWithSuffixlessAnnotation tests full reboot cycle with suffixless
// annotation which doesn't wait for annotation removal before power on
func TestRebootWithSuffixlessAnnotation(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = make(map[string]string)
	host.Annotations[rebootAnnotationPrefix] = ""
	host.Status.PoweredOn = true
	host.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
	host.Spec.Online = true
	host.Spec.Image = &metal3v1alpha1.Image{URL: "foo", Checksum: "123"}
	host.Spec.Image.URL = "foo"
	host.Status.Provisioning.Image.URL = "foo"

	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			if host.Status.PoweredOn {
				return false
			}

			return true
		},
	)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			if _, exists := host.Annotations[rebootAnnotationPrefix]; exists {
				return false
			}

			return true
		},
	)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			if !host.Status.PoweredOn {
				return false
			}

			return true
		},
	)

	//make sure we don't go into another reboot
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {

			if !host.Status.PoweredOn {
				return false
			}

			return true
		},
	)
}

// TestRebootWithSuffixedAnnotation tests a full reboot cycle, with suffixed annotation
// to verify that controller holds power off until annotation removal
func TestRebootWithSuffixedAnnotation(t *testing.T) {
	host := newDefaultHost(t)
	host.Annotations = make(map[string]string)
	annotation := rebootAnnotationPrefix + "/foo"
	host.Annotations[annotation] = ""
	host.Status.PoweredOn = true
	host.Status.Provisioning.State = metal3v1alpha1.StateProvisioned
	host.Spec.Online = true
	host.Spec.Image = &metal3v1alpha1.Image{URL: "foo", Checksum: "123"}
	host.Spec.Image.URL = "foo"
	host.Status.Provisioning.Image.URL = "foo"

	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			if host.Status.PoweredOn {
				return false
			}

			return true
		},
	)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			//we expect that the machine will be powered off until we remove annotation
			if host.Status.PoweredOn {
				return false
			}

			return true
		},
	)

	delete(host.Annotations, annotation)
	r.Update(goctx.TODO(), host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {

			if !host.Status.PoweredOn {
				return false
			}

			return true
		},
	)

	//make sure we don't go into another reboot
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			if !host.Status.PoweredOn {
				return false
			}

			return true
		},
	)
}

func getHostSecret(t *testing.T, r *BareMetalHostReconciler, host *metal3v1alpha1.BareMetalHost) (secret *corev1.Secret) {
	secret = &corev1.Secret{}
	secretName := types.NamespacedName{
		Namespace: host.Namespace,
		Name:      host.Spec.BMC.CredentialsName,
	}
	err := r.Get(goctx.TODO(), secretName, secret)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestSecretUpdateOwnerRefAndEnvironmentLabelOnStartup(t *testing.T) {
	host := newDefaultHost(t)
	r := newTestReconciler(host)

	secret := getHostSecret(t, r, host)
	assert.Empty(t, secret.OwnerReferences)
	assert.Empty(t, secret.Labels)

	waitForProvisioningState(t, r, host, metal3v1alpha1.StateInspecting)

	secret = getHostSecret(t, r, host)
	assert.Equal(t, host.Name, secret.OwnerReferences[0].Name)
	assert.Equal(t, "BareMetalHost", secret.OwnerReferences[0].Kind)
	assert.True(t, *secret.OwnerReferences[0].Controller)
	assert.True(t, *secret.OwnerReferences[0].BlockOwnerDeletion)

	assert.Equal(t, "baremetal", secret.Labels["environment.metal3.io"])

}

// TestUpdateCredentialsSecretSuccessFields ensures that the
// GoodCredentials fields are updated in the status block of a host
// when the secret used exists and has all of the right fields.
func TestUpdateCredentialsSecretSuccessFields(t *testing.T) {
	host := newDefaultHost(t)
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("ref: %v ver: %s", host.Status.GoodCredentials.Reference,
				host.Status.GoodCredentials.Version)
			if host.Status.GoodCredentials.Version != "" {
				return true
			}
			return false
		},
	)

}

// TestUpdateGoodCredentialsOnNewSecret ensures that the
// GoodCredentials fields are updated when the secret for a host is
// changed to another secret that is also good.
func TestUpdateGoodCredentialsOnNewSecret(t *testing.T) {
	host := newDefaultHost(t)
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("ref: %v ver: %s", host.Status.GoodCredentials.Reference,
				host.Status.GoodCredentials.Version)
			if host.Status.GoodCredentials.Version != "" {
				return true
			}
			return false
		},
	)

	// Define a second valid secret and update the host to use it.
	secret2 := newBMCCredsSecret("bmc-creds-valid2", "User", "Pass")
	err := r.Create(goctx.TODO(), secret2)
	if err != nil {
		t.Fatal(err)
	}

	host.Spec.BMC.CredentialsName = "bmc-creds-valid2"
	err = r.Update(goctx.TODO(), host)
	if err != nil {
		t.Fatal(err)
	}

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("ref: %v ver: %s", host.Status.GoodCredentials.Reference,
				host.Status.GoodCredentials.Version)
			if host.Status.GoodCredentials.Reference != nil && host.Status.GoodCredentials.Reference.Name == "bmc-creds-valid2" {
				return true
			}
			return false
		},
	)
}

// TestUpdateGoodCredentialsOnBadSecret ensures that the
// GoodCredentials fields are *not* updated when the secret is changed
// to one that is missing data.
func TestUpdateGoodCredentialsOnBadSecret(t *testing.T) {
	host := newDefaultHost(t)
	badSecret := newBMCCredsSecret("bmc-creds-no-user", "", "Pass")
	r := newTestReconciler(host, badSecret)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("ref: %v ver: %s", host.Status.GoodCredentials.Reference,
				host.Status.GoodCredentials.Version)
			if host.Status.GoodCredentials.Version != "" {
				return true
			}
			return false
		},
	)

	host.Spec.BMC.CredentialsName = "bmc-creds-no-user"
	err := r.Update(goctx.TODO(), host)
	if err != nil {
		t.Fatal(err)
	}

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {

			t.Logf("ref: %v ver: %s", host.Status.GoodCredentials.Reference,
				host.Status.GoodCredentials.Version)
			if host.Spec.BMC.CredentialsName != "bmc-creds-no-user" {
				return false
			}
			if host.Status.GoodCredentials.Reference != nil && host.Status.GoodCredentials.Reference.Name == "bmc-creds-valid" {
				return true
			}
			return false
		},
	)
}

// TestDiscoveredHost ensures that a host without a BMC IP and
// credentials is placed into the "discovered" state.
func TestDiscoveredHost(t *testing.T) {
	noAddressOrSecret := newHost("missing-bmc-address",
		&metal3v1alpha1.BareMetalHostSpec{
			BMC: metal3v1alpha1.BMCDetails{
				Address:         "",
				CredentialsName: "",
			},
		})
	r := newTestReconciler(noAddressOrSecret)
	waitForStatus(t, r, noAddressOrSecret, metal3v1alpha1.OperationalStatusDiscovered)
	if noAddressOrSecret.Status.ErrorType != "" {
		t.Errorf("Unexpected error type %s", noAddressOrSecret.Status.ErrorType)
	}
	if noAddressOrSecret.Status.ErrorMessage != "" {
		t.Errorf("Unexpected error message %s", noAddressOrSecret.Status.ErrorMessage)
	}
}

// TestMissingBMCParameters ensures that a host that is missing some
// of the required BMC settings is put into an error state.
func TestMissingBMCParameters(t *testing.T) {

	testCases := []struct {
		Scenario string
		Secret   *corev1.Secret
		Host     *metal3v1alpha1.BareMetalHost
	}{
		{
			Scenario: "secret without username",
			Secret:   newBMCCredsSecret("bmc-creds-no-user", "", "Pass"),
			Host: newHost("missing-bmc-username",
				&metal3v1alpha1.BareMetalHostSpec{
					BMC: metal3v1alpha1.BMCDetails{
						Address:         "ipmi://192.168.122.1:6233",
						CredentialsName: "bmc-creds-no-user",
					},
				}),
		},

		{
			Scenario: "secret without password",
			Secret:   newBMCCredsSecret("bmc-creds-no-pass", "User", ""),
			Host: newHost("missing-bmc-password",
				&metal3v1alpha1.BareMetalHostSpec{
					BMC: metal3v1alpha1.BMCDetails{
						Address:         "ipmi://192.168.122.1:6233",
						CredentialsName: "bmc-creds-no-pass",
					},
				}),
		},

		{
			Scenario: "missing address",
			Secret:   newBMCCredsSecret("bmc-creds-ok", "User", "Pass"),
			Host: newHost("missing-bmc-address",
				&metal3v1alpha1.BareMetalHostSpec{
					BMC: metal3v1alpha1.BMCDetails{
						Address:         "",
						CredentialsName: "bmc-creds-ok",
					},
				}),
		},

		{
			Scenario: "missing secret",
			Secret:   newBMCCredsSecret("bmc-creds-ok", "User", "Pass"),
			Host: newHost("missing-bmc-credentials-ref",
				&metal3v1alpha1.BareMetalHostSpec{
					BMC: metal3v1alpha1.BMCDetails{
						Address:         "ipmi://192.168.122.1:6233",
						CredentialsName: "",
					},
				}),
		},

		{
			Scenario: "no such secret",
			Secret:   newBMCCredsSecret("bmc-creds-ok", "User", "Pass"),
			Host: newHost("non-existent-bmc-secret-ref",
				&metal3v1alpha1.BareMetalHostSpec{
					BMC: metal3v1alpha1.BMCDetails{
						Address:         "ipmi://192.168.122.1:6233",
						CredentialsName: "this-secret-does-not-exist",
					},
				}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {
			r := newTestReconciler(tc.Secret, tc.Host)
			waitForError(t, r, tc.Host)
			if tc.Host.Status.OperationalStatus != metal3v1alpha1.OperationalStatusError {
				t.Errorf("Unexpected operational status %s", tc.Host.Status.OperationalStatus)
			}
			if tc.Host.Status.ErrorType != metal3v1alpha1.RegistrationError {
				t.Errorf("Unexpected error type %s", tc.Host.Status.ErrorType)
			}
		})
	}
}

// TestFixSecret ensures that when the secret for a host is updated to
// be correct the status of the host moves out of the error state.
func TestFixSecret(t *testing.T) {

	secret := newBMCCredsSecret("bmc-creds-no-user", "", "Pass")
	host := newHost("fix-secret",
		&metal3v1alpha1.BareMetalHostSpec{
			BMC: metal3v1alpha1.BMCDetails{
				Address:         "ipmi://192.168.122.1:6233",
				CredentialsName: "bmc-creds-no-user",
			},
		})
	r := newTestReconciler(host, secret)
	waitForError(t, r, host)

	secret = &corev1.Secret{}
	secretName := types.NamespacedName{
		Namespace: namespace,
		Name:      "bmc-creds-no-user",
	}
	err := r.Get(goctx.TODO(), secretName, secret)
	if err != nil {
		t.Fatal(err)
	}
	secret.Data["username"] = []byte(base64.StdEncoding.EncodeToString([]byte("username")))
	err = r.Update(goctx.TODO(), secret)
	if err != nil {
		t.Fatal(err)
	}
	waitForNoError(t, r, host)
}

// TestBreakThenFixSecret ensures that when the secret for a known host
// is updated to be broken, and then correct, the status of the host
// moves out of the error state.
func TestBreakThenFixSecret(t *testing.T) {

	// Create the host without any errors and wait for it to be
	// registered and get a provisioning ID.
	secret := newBMCCredsSecret("bmc-creds-toggle-user", "User", "Pass")
	host := newHost("break-then-fix-secret",
		&metal3v1alpha1.BareMetalHostSpec{
			BMC: metal3v1alpha1.BMCDetails{
				Address:         "ipmi://192.168.122.1:6233",
				CredentialsName: "bmc-creds-toggle-user",
			},
		})
	r := newTestReconciler(host, secret)
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			id := host.Status.Provisioning.ID
			t.Logf("Waiting for provisioning ID to be set: %q", id)
			return id != ""
		},
	)

	// Modify the secret to be bad by removing the username. Wait for
	// the host to show the error.
	secret = &corev1.Secret{}
	secretName := types.NamespacedName{
		Namespace: namespace,
		Name:      "bmc-creds-toggle-user",
	}
	err := r.Get(goctx.TODO(), secretName, secret)
	if err != nil {
		t.Fatal(err)
	}
	oldUsername := secret.Data["username"]
	secret.Data["username"] = []byte{}
	err = r.Update(goctx.TODO(), secret)
	if err != nil {
		t.Fatal(err)
	}
	waitForError(t, r, host)

	// Modify the secret to be correct again. Wait for the error to be
	// cleared from the host.
	secret = &corev1.Secret{}
	err = r.Get(goctx.TODO(), secretName, secret)
	if err != nil {
		t.Fatal(err)
	}
	secret.Data["username"] = oldUsername
	err = r.Update(goctx.TODO(), secret)
	if err != nil {
		t.Fatal(err)
	}
	waitForNoError(t, r, host)

}

// TestSetHardwareProfile ensures that the host has a label with
// the hardware profile name.
func TestSetHardwareProfile(t *testing.T) {
	host := newDefaultHost(t)
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("profile: %v", host.Status.HardwareProfile)
			if host.Status.HardwareProfile != "" {
				return true
			}
			return false
		},
	)
}

// TestCreateHardwareDetails ensures that the HardwareDetails portion
// of the status block is filled in for new hosts.
func TestCreateHardwareDetails(t *testing.T) {
	host := newDefaultHost(t)
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("new host details: %v", host.Status.HardwareDetails)
			if host.Status.HardwareDetails != nil {
				return true
			}
			return false
		},
	)
}

// TestNeedsProvisioning verifies the logic for deciding when a host
// needs to be provisioned.
func TestNeedsProvisioning(t *testing.T) {
	host := newDefaultHost(t)

	if host.NeedsProvisioning() {
		t.Fatal("host without spec image should not need provisioning")
	}

	host.Spec.Image = &metal3v1alpha1.Image{
		URL:      "https://example.com/image-name",
		Checksum: "12345",
	}

	if host.NeedsProvisioning() {
		t.Fatal("host with spec image but not online should not need provisioning")
	}

	host.Spec.Online = true

	if !host.NeedsProvisioning() {
		t.Fatal("host with spec image and online without provisioning image should need provisioning")
	}

	host.Status.Provisioning.Image = *host.Spec.Image

	if host.NeedsProvisioning() {
		t.Fatal("host with spec image matching status image should not need provisioning")
	}
}

// TestNeedsProvisioning verifies the logic for deciding when a host
// needs to be provisioned when custom deploy is used.
func TestNeedsProvisioningCustomDeploy(t *testing.T) {
	cases := []struct {
		name string

		customDeploy        string
		currentCustomDeploy string
		online              bool
		image               *metal3v1alpha1.Image

		needsProvisioning bool
	}{
		{
			name:              "empty host",
			needsProvisioning: false,
		},
		{
			name:              "with custom deploy but not online",
			customDeploy:      "install_everything",
			needsProvisioning: false,
		},
		{
			name:              "with custom deploy and online",
			customDeploy:      "install_everything",
			online:            true,
			needsProvisioning: true,
		},
		{
			name:                "with matching custom deploy and online",
			customDeploy:        "install_everything",
			currentCustomDeploy: "install_everything",
			online:              true,
			needsProvisioning:   false,
		},
		{
			name:                "with custom deploy and new image",
			customDeploy:        "install_everything",
			currentCustomDeploy: "install_everything",
			image: &metal3v1alpha1.Image{
				URL:      "https://example.com/image-name",
				Checksum: "12345",
			},
			online:            true,
			needsProvisioning: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := newDefaultHost(t)

			host.Spec.Online = tc.online
			if tc.customDeploy != "" {
				host.Spec.CustomDeploy = &metal3v1alpha1.CustomDeploy{
					Method: tc.customDeploy,
				}
			}
			if tc.currentCustomDeploy != "" {
				host.Status.Provisioning.CustomDeploy = &metal3v1alpha1.CustomDeploy{
					Method: tc.currentCustomDeploy,
				}
			}
			if tc.image != nil {
				host.Spec.Image = tc.image
			}

			assert.Equal(t, tc.needsProvisioning, host.NeedsProvisioning())
		})
	}
}

// TestProvision ensures that the Provisioning.Image portion of the
// status block is filled in for provisioned hosts.
func TestProvision(t *testing.T) {
	host := newDefaultHost(t)
	host.Spec.Image = &metal3v1alpha1.Image{
		URL:      "https://example.com/image-name",
		Checksum: "12345",
	}
	host.Spec.Online = true
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("image details: %v", host.Spec.Image)
			t.Logf("provisioning image details: %v", host.Status.Provisioning.Image)
			t.Logf("provisioning state: %v", host.Status.Provisioning.State)
			if host.Status.Provisioning.Image.URL != "" {
				return true
			}
			return false
		},
	)
}

// TestProvisionCustomDeploy ensures that the Provisioning.CustomDeploy portion
// of the status block is filled in for provisioned hosts.
func TestProvisionCustomDeploy(t *testing.T) {
	host := newDefaultHost(t)
	host.Spec.CustomDeploy = &metal3v1alpha1.CustomDeploy{
		Method: "install_everything",
	}
	host.Spec.Online = true
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("custom deploy: %v", host.Spec.CustomDeploy)
			t.Logf("provisioning custom deploy: %v", host.Status.Provisioning.CustomDeploy)
			t.Logf("provisioning state: %v", host.Status.Provisioning.State)
			return host.Status.Provisioning.CustomDeploy != nil && host.Status.Provisioning.CustomDeploy.Method == "install_everything" && host.Status.Provisioning.State == metal3v1alpha1.StateProvisioned
		},
	)
}

// TestProvisionCustomDeployWithURL ensures that the Provisioning.CustomDeploy
// portion of the status block is filled in for provisioned hosts.
func TestProvisionCustomDeployWithURL(t *testing.T) {
	host := newDefaultHost(t)
	host.Spec.CustomDeploy = &metal3v1alpha1.CustomDeploy{
		Method: "install_everything",
	}
	host.Spec.Image = &metal3v1alpha1.Image{
		URL:      "https://example.com/image-name",
		Checksum: "12345",
	}
	host.Spec.Online = true
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("image details: %v", host.Spec.Image)
			t.Logf("custom deploy: %v", host.Spec.CustomDeploy)
			t.Logf("provisioning image details: %v", host.Status.Provisioning.Image)
			t.Logf("provisioning custom deploy: %v", host.Status.Provisioning.CustomDeploy)
			t.Logf("provisioning state: %v", host.Status.Provisioning.State)
			return host.Status.Provisioning.CustomDeploy != nil && host.Status.Provisioning.CustomDeploy.Method != "" && host.Status.Provisioning.Image.URL != ""
		},
	)
}

// TestExternallyProvisionedTransitions ensures that host enters the
// expected states when it looks like it has been provisioned by
// another tool.
func TestExternallyProvisionedTransitions(t *testing.T) {

	t.Run("registered to externally provisioned", func(t *testing.T) {
		host := newDefaultHost(t)
		host.Spec.Online = true
		host.Spec.ConsumerRef = &corev1.ObjectReference{} // it doesn't have to point to a real object
		host.Spec.ExternallyProvisioned = true
		r := newTestReconciler(host)

		waitForProvisioningState(t, r, host, metal3v1alpha1.StateExternallyProvisioned)
	})

	t.Run("externally provisioned to inspecting", func(t *testing.T) {
		host := newDefaultHost(t)
		host.Spec.Online = true
		host.Spec.ExternallyProvisioned = true
		r := newTestReconciler(host)

		waitForProvisioningState(t, r, host, metal3v1alpha1.StateExternallyProvisioned)

		host.Spec.ExternallyProvisioned = false
		err := r.Update(goctx.TODO(), host)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("set externally provisioned to false")

		waitForProvisioningState(t, r, host, metal3v1alpha1.StateInspecting)
	})

	t.Run("preparing to externally provisioned", func(t *testing.T) {
		host := newDefaultHost(t)
		host.Spec.Online = true
		r := newTestReconciler(host)

		waitForProvisioningState(t, r, host, metal3v1alpha1.StatePreparing)

		host.Spec.ExternallyProvisioned = true
		err := r.Update(goctx.TODO(), host)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("set externally provisioned to true")

		waitForProvisioningState(t, r, host, metal3v1alpha1.StateExternallyProvisioned)
	})

}

// TestPowerOn verifies that the controller turns the host on when it
// should.
func TestPowerOn(t *testing.T) {
	host := newDefaultHost(t)
	host.Spec.Online = true
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("power status: %v", host.Status.PoweredOn)
			return host.Status.PoweredOn
		},
	)
}

// TestPowerOff verifies that the controller turns the host on when it
// should.
func TestPowerOff(t *testing.T) {
	host := newDefaultHost(t)
	host.Spec.Online = false
	r := newTestReconciler(host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("power status: %v", host.Status.PoweredOn)
			return !host.Status.PoweredOn
		},
	)
}

// TestDeleteHost verifies several delete cases
func TestDeleteHost(t *testing.T) {
	now := metav1.Now()

	type HostFactory func() *metal3v1alpha1.BareMetalHost

	testCases := []HostFactory{
		func() *metal3v1alpha1.BareMetalHost {
			host := newDefaultNamedHost("with-finalizer", t)
			host.Finalizers = append(host.Finalizers,
				metal3v1alpha1.BareMetalHostFinalizer)
			return host
		},
		func() *metal3v1alpha1.BareMetalHost {
			host := newDefaultNamedHost("without-bmc", t)
			host.Spec.BMC = metal3v1alpha1.BMCDetails{}
			host.Finalizers = append(host.Finalizers,
				metal3v1alpha1.BareMetalHostFinalizer)
			return host
		},
		func() *metal3v1alpha1.BareMetalHost {
			t.Logf("host with bad credentials, no user")
			host := newHost("fix-secret",
				&metal3v1alpha1.BareMetalHostSpec{
					BMC: metal3v1alpha1.BMCDetails{
						Address:         "ipmi://192.168.122.1:6233",
						CredentialsName: "bmc-creds-no-user",
					},
				})
			host.Finalizers = append(host.Finalizers,
				metal3v1alpha1.BareMetalHostFinalizer)
			return host
		},
		func() *metal3v1alpha1.BareMetalHost {
			host := newDefaultNamedHost("host-with-hw-details", t)
			host.Status.HardwareDetails = &metal3v1alpha1.HardwareDetails{}
			host.Finalizers = append(host.Finalizers,
				metal3v1alpha1.BareMetalHostFinalizer)
			return host
		},
		func() *metal3v1alpha1.BareMetalHost {
			host := newDefaultNamedHost("provisioned-host", t)
			host.Status.HardwareDetails = &metal3v1alpha1.HardwareDetails{}
			host.Status.Provisioning.Image = metal3v1alpha1.Image{
				URL:      "image-url",
				Checksum: "image-checksum",
			}
			host.Spec.Image = &host.Status.Provisioning.Image
			host.Finalizers = append(host.Finalizers,
				metal3v1alpha1.BareMetalHostFinalizer)
			return host
		},
	}

	for _, factory := range testCases {
		host := factory()
		t.Run(host.Name, func(t *testing.T) {
			host.DeletionTimestamp = &now
			host.Status.Provisioning.ID = "made-up-id"
			badSecret := newBMCCredsSecret("bmc-creds-no-user", "", "Pass")
			fix := fixture.Fixture{}
			r := newTestReconcilerWithFixture(&fix, host, badSecret)

			tryReconcile(t, r, host,
				func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
					return fix.Deleted && host == nil
				},
			)
		})
	}
}

// TestUpdateRootDeviceHints verifies that we apply the correct
// precedence rules to the root device hints settings for a host.
func TestUpdateRootDeviceHints(t *testing.T) {
	rotational := true

	testCases := []struct {
		Scenario string
		Host     metal3v1alpha1.BareMetalHost
		Dirty    bool
		Expected *metal3v1alpha1.RootDeviceHints
	}{
		{
			Scenario: "override profile with explicit hints",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
					UID:       "27720611-e5d1-45d3-ba3a-222dcfaa4ca2",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					HardwareProfile: "libvirt",
					RootDeviceHints: &metal3v1alpha1.RootDeviceHints{
						DeviceName:         "userd_devicename",
						HCTL:               "1:2:3:4",
						Model:              "userd_model",
						Vendor:             "userd_vendor",
						SerialNumber:       "userd_serial",
						MinSizeGigabytes:   40,
						WWN:                "userd_wwn",
						WWNWithExtension:   "userd_with_extension",
						WWNVendorExtension: "userd_vendor_extension",
						Rotational:         &rotational,
					},
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					HardwareProfile: "libvirt",
				},
			},
			Dirty: true,
			Expected: &metal3v1alpha1.RootDeviceHints{
				DeviceName:         "userd_devicename",
				HCTL:               "1:2:3:4",
				Model:              "userd_model",
				Vendor:             "userd_vendor",
				SerialNumber:       "userd_serial",
				MinSizeGigabytes:   40,
				WWN:                "userd_wwn",
				WWNWithExtension:   "userd_with_extension",
				WWNVendorExtension: "userd_vendor_extension",
				Rotational:         &rotational,
			},
		},

		{
			Scenario: "use profile hints",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
					UID:       "27720611-e5d1-45d3-ba3a-222dcfaa4ca2",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					HardwareProfile: "libvirt",
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					HardwareProfile: "libvirt",
				},
			},
			Dirty: true,
			Expected: &metal3v1alpha1.RootDeviceHints{
				DeviceName: "/dev/vda",
			},
		},

		{
			Scenario: "default profile hints",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
					UID:       "27720611-e5d1-45d3-ba3a-222dcfaa4ca2",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					HardwareProfile: "unknown",
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					HardwareProfile: "unknown",
				},
			},
			Dirty: true,
			Expected: &metal3v1alpha1.RootDeviceHints{
				DeviceName: "/dev/sda",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {
			dirty, newStatus, err := getHostProvisioningSettings(&tc.Host)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, tc.Dirty, dirty, "dirty flag did not match")
			assert.Equal(t, tc.Expected, newStatus.Provisioning.RootDeviceHints)

			dirty, err = saveHostProvisioningSettings(&tc.Host)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, tc.Dirty, dirty, "dirty flag did not match")
			assert.Equal(t, tc.Expected, tc.Host.Status.Provisioning.RootDeviceHints)
		})
	}
}

func TestProvisionerIsReady(t *testing.T) {
	host := newDefaultHost(t)

	fix := fixture.Fixture{BecomeReadyCounter: 5}
	r := newTestReconcilerWithFixture(&fix, host)

	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			return host.Status.Provisioning.State != metal3v1alpha1.StateNone
		},
	)
}

func TestUpdateEventHandler(t *testing.T) {
	cases := []struct {
		name            string
		event           event.UpdateEvent
		expectedProcess bool
	}{
		{
			name: "process-non-bmh-events",
			event: event.UpdateEvent{
				ObjectOld: &corev1.Secret{},
				ObjectNew: &corev1.Secret{},
			},
			expectedProcess: true,
		},
		{
			name: "process-generation-change",
			event: event.UpdateEvent{
				ObjectOld: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{Generation: 0}},
				ObjectNew: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			},

			expectedProcess: true,
		},
		{
			name: "skip-if-same-generation-finalizers-and-annotations",
			event: event.UpdateEvent{
				ObjectOld: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{
					Generation: 0,
					Finalizers: []string{metal3v1alpha1.BareMetalHostFinalizer},
					Annotations: map[string]string{
						metal3v1alpha1.PausedAnnotation: "true",
					},
				}},
				ObjectNew: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{
					Generation: 0,
					Finalizers: []string{metal3v1alpha1.BareMetalHostFinalizer},
					Annotations: map[string]string{
						metal3v1alpha1.PausedAnnotation: "true",
					},
				}},
			},

			expectedProcess: false,
		},
		{
			name: "process-same-generation-annotations-change",
			event: event.UpdateEvent{
				ObjectOld: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{
					Generation:  0,
					Finalizers:  []string{metal3v1alpha1.BareMetalHostFinalizer},
					Annotations: map[string]string{},
				}},
				ObjectNew: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{
					Generation: 0,
					Finalizers: []string{metal3v1alpha1.BareMetalHostFinalizer},
					Annotations: map[string]string{
						metal3v1alpha1.PausedAnnotation: "true",
					},
				}},
			},

			expectedProcess: true,
		},
		{
			name: "process-same-generation-finalizers-change",
			event: event.UpdateEvent{
				ObjectOld: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{
					Generation: 0,
					Finalizers: []string{},
					Annotations: map[string]string{
						metal3v1alpha1.PausedAnnotation: "true",
					},
				}},
				ObjectNew: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{
					Generation: 0,
					Finalizers: []string{metal3v1alpha1.BareMetalHostFinalizer},
					Annotations: map[string]string{
						metal3v1alpha1.PausedAnnotation: "true",
					},
				}},
			},

			expectedProcess: true,
		},
		{
			name: "process-same-generation-finalizers-and-annotation-change",
			event: event.UpdateEvent{
				ObjectOld: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{
					Generation:  0,
					Finalizers:  []string{},
					Annotations: map[string]string{},
				}},
				ObjectNew: &metal3v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{
					Generation: 0,
					Finalizers: []string{metal3v1alpha1.BareMetalHostFinalizer},
					Annotations: map[string]string{
						metal3v1alpha1.PausedAnnotation: "true",
					},
				}},
			},

			expectedProcess: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestReconciler()
			assert.Equal(t, tc.expectedProcess, r.updateEventHandler(tc.event))
		})
	}
}

func TestErrorCountIncrementsAlways(t *testing.T) {

	errorTypes := []metal3v1alpha1.ErrorType{metal3v1alpha1.RegistrationError, metal3v1alpha1.InspectionError, metal3v1alpha1.ProvisioningError, metal3v1alpha1.PowerManagementError}

	b := &metal3v1alpha1.BareMetalHost{}
	assert.Equal(t, b.Status.ErrorCount, 0)

	for _, c := range errorTypes {
		before := b.Status.ErrorCount
		setErrorMessage(b, c, "An error message")
		assert.Equal(t, before+1, b.Status.ErrorCount)
	}
}

func TestGetImageReady(t *testing.T) {
	host := metal3v1alpha1.BareMetalHost{
		Spec: metal3v1alpha1.BareMetalHostSpec{
			Image: &metal3v1alpha1.Image{
				URL: "http://example.test/image",
			},
		},
		Status: metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				State: metal3v1alpha1.StateReady,
			},
		},
	}

	img := getCurrentImage(&host)

	assert.Nil(t, img)
}

func TestGetImageProvisioning(t *testing.T) {
	host := metal3v1alpha1.BareMetalHost{
		Spec: metal3v1alpha1.BareMetalHostSpec{
			Image: &metal3v1alpha1.Image{
				URL: "http://example.test/image",
			},
		},
		Status: metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				State: metal3v1alpha1.StateProvisioning,
			},
		},
	}

	img := getCurrentImage(&host)

	assert.NotNil(t, img)
	assert.NotSame(t, host.Spec.Image, img)
	assert.Exactly(t, *host.Spec.Image, *img)
}

func TestGetImageProvisioned(t *testing.T) {
	host := metal3v1alpha1.BareMetalHost{
		Spec: metal3v1alpha1.BareMetalHostSpec{
			Image: &metal3v1alpha1.Image{
				URL: "http://example.test/image2",
			},
		},
		Status: metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				State: metal3v1alpha1.StateProvisioned,
				Image: metal3v1alpha1.Image{
					URL: "http://example.test/image",
				},
			},
		},
	}

	img := getCurrentImage(&host)

	assert.NotNil(t, img)
	assert.NotSame(t, &host.Status.Provisioning.Image, img)
	assert.Exactly(t, host.Status.Provisioning.Image, *img)
}

func TestGetImageDeprovisioning(t *testing.T) {
	host := metal3v1alpha1.BareMetalHost{
		Spec: metal3v1alpha1.BareMetalHostSpec{
			Image: &metal3v1alpha1.Image{
				URL: "http://example.test/image2",
			},
		},
		Status: metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				State: metal3v1alpha1.StateDeprovisioning,
				Image: metal3v1alpha1.Image{
					URL: "http://example.test/image",
				},
			},
		},
	}

	img := getCurrentImage(&host)

	assert.NotNil(t, img)
	assert.NotSame(t, &host.Status.Provisioning.Image, img)
	assert.Exactly(t, host.Status.Provisioning.Image, *img)
}

func TestGetImageExternallyPprovisioned(t *testing.T) {
	host := metal3v1alpha1.BareMetalHost{
		Spec: metal3v1alpha1.BareMetalHostSpec{
			Image: &metal3v1alpha1.Image{
				URL: "http://example.test/image",
			},
		},
		Status: metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				State: metal3v1alpha1.StateExternallyProvisioned,
			},
		},
	}

	img := getCurrentImage(&host)

	assert.NotNil(t, img)
	assert.NotSame(t, host.Spec.Image, img)
	assert.Exactly(t, *host.Spec.Image, *img)
}

func TestUpdateRAID(t *testing.T) {
	host := metal3v1alpha1.BareMetalHost{
		Spec: metal3v1alpha1.BareMetalHostSpec{
			HardwareProfile: "libvirt",
			RootDeviceHints: &metal3v1alpha1.RootDeviceHints{
				DeviceName:         "userd_devicename",
				HCTL:               "1:2:3:4",
				Model:              "userd_model",
				Vendor:             "userd_vendor",
				SerialNumber:       "userd_serial",
				MinSizeGigabytes:   40,
				WWN:                "userd_wwn",
				WWNWithExtension:   "userd_with_extension",
				WWNVendorExtension: "userd_vendor_extension",
			},
			RAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Name: "root",
					},
					{
						Name: "v1",
					},
				},
			},
		},
		Status: metal3v1alpha1.BareMetalHostStatus{
			Provisioning: metal3v1alpha1.ProvisionStatus{
				RootDeviceHints: &metal3v1alpha1.RootDeviceHints{
					DeviceName:         "userd_devicename",
					HCTL:               "1:2:3:4",
					Model:              "userd_model",
					Vendor:             "userd_vendor",
					SerialNumber:       "userd_serial",
					MinSizeGigabytes:   40,
					WWN:                "userd_wwn",
					WWNWithExtension:   "userd_with_extension",
					WWNVendorExtension: "userd_vendor_extension",
				},
			},
		},
	}
	cases := []struct {
		name       string
		specRAID   *metal3v1alpha1.RAIDConfig
		statusRAID *metal3v1alpha1.RAIDConfig
		dirty      bool
		expected   *metal3v1alpha1.RAIDConfig
	}{
		{
			name:       "not configured, not saved",
			specRAID:   nil,
			statusRAID: nil,
			dirty:      false,
		},
		{
			name:       "not configured, not saved",
			specRAID:   &metal3v1alpha1.RAIDConfig{},
			statusRAID: &metal3v1alpha1.RAIDConfig{},
			dirty:      false,
			expected:   &metal3v1alpha1.RAIDConfig{},
		},
		{
			name:       "not configured, not saved",
			specRAID:   &metal3v1alpha1.RAIDConfig{},
			statusRAID: nil,
			dirty:      true,
			expected:   &metal3v1alpha1.RAIDConfig{},
		},
		{
			name:       "not configured, not saved",
			specRAID:   nil,
			statusRAID: &metal3v1alpha1.RAIDConfig{},
			dirty:      true,
			expected:   nil,
		},
		{
			name: "HardwareRAIDVolumes configured, not saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: nil,
			dirty:      true,
			expected: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "SoftwareRAIDVolumes configured, not saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: nil,
			dirty:      true,
			expected: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "both configured, not saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: nil,
			dirty:      true,
			expected: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "HardwareRAIDVolumes configured, HardwareRAIDVolumes saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			dirty: false,
			expected: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "HardwareRAIDVolumes configured, SoftwareRAIDVolumes saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			dirty: true,
			expected: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "HardwareRAIDVolumes configured, both saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			dirty: false,
			expected: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "SoftwareRAIDVolumes configured, HardwareRAIDVolumes saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			dirty: true,
			expected: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "SoftwareRAIDVolumes configured, SoftwareRAIDVolumes saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			dirty: false,
			expected: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "SoftwareRAIDVolumes configured, both saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			dirty: true,
			expected: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "both configured, HardwareRAIDVolumes saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			dirty: false,
			expected: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "both configured, SoftwareRAIDVolumes saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: &metal3v1alpha1.RAIDConfig{
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			dirty: true,
			expected: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
		{
			name: "both configured, both saved",
			specRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			statusRAID: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
				SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
			dirty: false,
			expected: &metal3v1alpha1.RAIDConfig{
				HardwareRAIDVolumes: []metal3v1alpha1.HardwareRAIDVolume{
					{
						Level: "1",
					},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host.Spec.RAID = c.specRAID
			host.Status.Provisioning.RAID = c.statusRAID
			dirty, _ := saveHostProvisioningSettings(&host)
			assert.Equal(t, c.dirty, dirty)
			assert.Equal(t, c.expected, host.Status.Provisioning.RAID)
		})
	}
}

func doDeleteHost(host *metal3v1alpha1.BareMetalHost, reconciler *BareMetalHostReconciler) {
	now := metav1.Now()
	host.DeletionTimestamp = &now
	reconciler.Client.Update(context.Background(), host)
}

func TestInvalidBMHCanBeDeleted(t *testing.T) {
	host := newDefaultHost(t)
	host.Spec.BMC.Address = fmt.Sprintf("%s%s%s", "<", host.Spec.BMC.Address, ">")

	var fix fixture.Fixture
	r := newTestReconcilerWithFixture(&fix, host)

	fix.SetValidateError("malformed url")
	waitForError(t, r, host)
	assert.Equal(t, metal3v1alpha1.StateRegistering, host.Status.Provisioning.State)
	assert.Equal(t, metal3v1alpha1.OperationalStatusError, host.Status.OperationalStatus)
	assert.Equal(t, metal3v1alpha1.RegistrationError, host.Status.ErrorType)
	assert.Equal(t, "malformed url", host.Status.ErrorMessage)

	doDeleteHost(host, r)

	tryReconcile(t, r, host, func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
		return host == nil
	})
}

func TestCredentialsFromSecret(t *testing.T) {
	cases := []struct {
		name     string
		input    corev1.Secret
		expected bmc.Credentials
	}{
		{
			name:     "empty",
			input:    corev1.Secret{},
			expected: bmc.Credentials{},
		},
		{
			name: "clean",
			input: corev1.Secret{
				Data: map[string][]byte{
					"username": []byte("username"),
					"password": []byte("password"),
				},
			},
			expected: bmc.Credentials{
				Username: "username",
				Password: "password",
			},
		},
		{
			name: "newline",
			input: corev1.Secret{
				Data: map[string][]byte{
					"username": []byte("username\n"),
					"password": []byte("password\n"),
				},
			},
			expected: bmc.Credentials{
				Username: "username",
				Password: "password",
			},
		},
		{
			name: "non-newline",
			input: corev1.Secret{
				Data: map[string][]byte{
					"username": []byte(" username\t"),
					"password": []byte(" password\t"),
				},
			},
			expected: bmc.Credentials{
				Username: "username",
				Password: "password",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := credentialsFromSecret(&c.input)
			assert.Equal(t, c.expected, *actual)
		})
	}
}
