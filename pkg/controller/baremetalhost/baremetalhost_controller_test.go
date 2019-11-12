package baremetalhost

import (
	goctx "context"
	"encoding/base64"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/client-go/kubernetes/scheme"

	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	metal3apis "github.com/metal3-io/baremetal-operator/pkg/apis"
	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/pkg/apis/metal3/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner/fixture"
	"github.com/metal3-io/baremetal-operator/pkg/utils"
)

const (
	namespace         string = "test-namespace"
	defaultSecretName string = "bmc-creds-valid"
)

func init() {
	logf.SetLogger(logf.ZapLogger(true))
	// Register our package types with the global scheme
	metal3apis.AddToScheme(scheme.Scheme)
}

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
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: "1",
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
		},
		ObjectMeta: metav1.ObjectMeta{
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

func newTestReconciler(initObjs ...runtime.Object) *ReconcileBareMetalHost {

	c := fakeclient.NewFakeClient(initObjs...)

	// Add a default secret that can be used by most hosts.
	bmcSecret := newBMCCredsSecret(defaultSecretName, "User", "Pass")
	c.Create(goctx.TODO(), bmcSecret)

	return &ReconcileBareMetalHost{
		client:             c,
		scheme:             scheme.Scheme,
		provisionerFactory: fixture.New,
	}
}

type DoneFunc func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool

func newRequest(host *metal3v1alpha1.BareMetalHost) reconcile.Request {
	namespacedName := types.NamespacedName{
		Namespace: host.ObjectMeta.Namespace,
		Name:      host.ObjectMeta.Name,
	}
	return reconcile.Request{NamespacedName: namespacedName}
}

func tryReconcile(t *testing.T, r *ReconcileBareMetalHost, host *metal3v1alpha1.BareMetalHost, isDone DoneFunc) {

	request := newRequest(host)

	for i := 0; ; i++ {
		logger := log.WithValues("iteration", i)
		logger.Info("tryReconcile: top of loop")
		if i >= 25 {
			t.Fatal(fmt.Errorf("Exceeded 25 iterations"))
		}

		result, err := r.Reconcile(request)
		if err != nil {
			t.Fatal(err)
			break
		}

		// The FakeClient keeps a copy of the object we update, so we
		// need to replace the one we have with the updated data in
		// order to test it.
		r.client.Get(goctx.TODO(), request.NamespacedName, host)

		if isDone(host, result) {
			logger.Info("tryReconcile: loop done")
			break
		}

		logger.Info("tryReconcile: loop bottom", "result", result)
		if !result.Requeue && result.RequeueAfter == 0 {
			t.Fatal(fmt.Errorf("Ended reconcile at iteration %d without test condition being true", i))
			break
		}
	}
}

func waitForStatus(t *testing.T, r *ReconcileBareMetalHost, host *metal3v1alpha1.BareMetalHost, desiredStatus metal3v1alpha1.OperationalStatus) {
	logger := log.WithValues("host", host.ObjectMeta.Name, "desiredStatus", desiredStatus)
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			state := host.OperationalStatus()
			logger.Info("WAIT FOR STATUS", "State", state)
			return state == desiredStatus
		},
	)
}

func waitForError(t *testing.T, r *ReconcileBareMetalHost, host *metal3v1alpha1.BareMetalHost) {
	logger := log.WithValues("host", host.ObjectMeta.Name)
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			logger.Info("WAIT FOR ERROR", "ErrorMessage", host.Status.ErrorMessage)
			return host.HasError()
		},
	)
}

func waitForNoError(t *testing.T, r *ReconcileBareMetalHost, host *metal3v1alpha1.BareMetalHost) {
	logger := log.WithValues("host", host.ObjectMeta.Name)
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			logger.Info("WAIT FOR NO ERROR", "ErrorMessage", host.Status.ErrorMessage)
			return !host.HasError()
		},
	)
}

func waitForProvisioningState(t *testing.T, r *ReconcileBareMetalHost, host *metal3v1alpha1.BareMetalHost, desiredState metal3v1alpha1.ProvisioningState) {
	tryReconcile(t, r, host,
		func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
			t.Logf("Waiting for state %q have state %q", desiredState, host.Status.Provisioning.State)
			return host.Status.Provisioning.State == desiredState
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
			if result.Requeue && result.RequeueAfter == pauseRetryDelay &&
				len(host.Finalizers) == 0 {
				return true
			}
			return false
		},
	)
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
	err := r.client.Create(goctx.TODO(), secret2)
	if err != nil {
		t.Fatal(err)
	}

	host.Spec.BMC.CredentialsName = "bmc-creds-valid2"
	err = r.client.Update(goctx.TODO(), host)
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
	err := r.client.Update(goctx.TODO(), host)
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
	noAddress := newHost("missing-bmc-address",
		&metal3v1alpha1.BareMetalHostSpec{
			BMC: metal3v1alpha1.BMCDetails{
				Address:         "",
				CredentialsName: "bmc-creds-valid",
			},
		})
	r := newTestReconciler(noAddress)
	waitForStatus(t, r, noAddress, metal3v1alpha1.OperationalStatusDiscovered)

	noAddressOrSecret := newHost("missing-bmc-address",
		&metal3v1alpha1.BareMetalHostSpec{
			BMC: metal3v1alpha1.BMCDetails{
				Address:         "",
				CredentialsName: "",
			},
		})
	r = newTestReconciler(noAddressOrSecret)
	waitForStatus(t, r, noAddressOrSecret, metal3v1alpha1.OperationalStatusDiscovered)
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
			Scenario: "malformed address",
			Secret:   newBMCCredsSecret("bmc-creds-ok", "User", "Pass"),
			Host: newHost("invalid-bmc-address",
				&metal3v1alpha1.BareMetalHostSpec{
					BMC: metal3v1alpha1.BMCDetails{
						Address:         "unknown://notAvalidIPMIURL",
						CredentialsName: "bmc-creds-ok",
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
	err := r.client.Get(goctx.TODO(), secretName, secret)
	if err != nil {
		t.Fatal(err)
	}
	secret.Data["username"] = []byte(base64.StdEncoding.EncodeToString([]byte("username")))
	err = r.client.Update(goctx.TODO(), secret)
	if err != nil {
		t.Fatal(err)
	}
	waitForNoError(t, r, host)
}

// TestBreakThenFixSecret ensures that when the secret for a known host
// is updated to be broken, and then correct, the status of the host
// moves out of the error state.
func TestBreakThenFixSecret(t *testing.T) {

	logger := log.WithValues("Test", "TestBreakThenFixSecret")

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
			logger.Info("WAIT FOR PROVISIONING ID", "ID", id)
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
	err := r.client.Get(goctx.TODO(), secretName, secret)
	if err != nil {
		t.Fatal(err)
	}
	oldUsername := secret.Data["username"]
	secret.Data["username"] = []byte{}
	err = r.client.Update(goctx.TODO(), secret)
	if err != nil {
		t.Fatal(err)
	}
	waitForError(t, r, host)

	// Modify the secret to be correct again. Wait for the error to be
	// cleared from the host.
	secret = &corev1.Secret{}
	err = r.client.Get(goctx.TODO(), secretName, secret)
	if err != nil {
		t.Fatal(err)
	}
	secret.Data["username"] = oldUsername
	err = r.client.Update(goctx.TODO(), secret)
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
		err := r.client.Update(goctx.TODO(), host)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("set externally provisioned to false")

		waitForProvisioningState(t, r, host, metal3v1alpha1.StateInspecting)
	})

	t.Run("ready to externally provisioned", func(t *testing.T) {
		host := newDefaultHost(t)
		host.Spec.Online = true
		r := newTestReconciler(host)

		waitForProvisioningState(t, r, host, metal3v1alpha1.StateAvailable)

		host.Spec.ExternallyProvisioned = true
		err := r.client.Update(goctx.TODO(), host)
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
			r := newTestReconciler(host, badSecret)

			tryReconcile(t, r, host,
				func(host *metal3v1alpha1.BareMetalHost, result reconcile.Result) bool {
					t.Logf("provisioning id: %q", host.Status.Provisioning.ID)
					return host.Status.Provisioning.ID == ""
				},
			)
		})
	}
}
