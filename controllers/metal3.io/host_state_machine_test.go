package controllers

import (
	"testing"
	"time"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/bmc"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/hardware"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner"
	promutil "github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func testStateMachine(host *metal3v1alpha1.BareMetalHost) *hostStateMachine {
	r := newTestReconciler()
	p, _ := r.ProvisionerFactory.NewProvisioner(provisioner.BuildHostData(*host, bmc.Credentials{}),
		func(reason, message string) {})
	return newHostStateMachine(host, r, p, true)
}

func TestProvisioningCapacity(t *testing.T) {
	testCases := []struct {
		Scenario string

		HasProvisioningCapacity bool
		Host                    *metal3v1alpha1.BareMetalHost

		ExpectedProvisioningState metal3v1alpha1.ProvisioningState
		ExpectedDelayed           bool
	}{
		{
			Scenario:                "transition-to-inspecting-delayed",
			Host:                    host(metal3v1alpha1.StateRegistering).build(),
			HasProvisioningCapacity: false,

			ExpectedProvisioningState: metal3v1alpha1.StateRegistering,
			ExpectedDelayed:           true,
		},
		{
			Scenario:                "transition-to-provisioning-delayed",
			Host:                    host(metal3v1alpha1.StateReady).SaveHostProvisioningSettings().build(),
			HasProvisioningCapacity: false,

			ExpectedProvisioningState: metal3v1alpha1.StateReady,
			ExpectedDelayed:           true,
		},
		{
			Scenario:                "transition-to-inspecting-ok",
			Host:                    host(metal3v1alpha1.StateRegistering).build(),
			HasProvisioningCapacity: true,

			ExpectedProvisioningState: metal3v1alpha1.StateInspecting,
			ExpectedDelayed:           false,
		},
		{
			Scenario:                "transition-to-provisioning-ok",
			Host:                    host(metal3v1alpha1.StateReady).SaveHostProvisioningSettings().build(),
			HasProvisioningCapacity: true,

			ExpectedProvisioningState: metal3v1alpha1.StateProvisioning,
			ExpectedDelayed:           false,
		},

		{
			Scenario:                "already-delayed-delayed",
			Host:                    host(metal3v1alpha1.StateReady).SetOperationalStatus(metal3v1alpha1.OperationalStatusDelayed).build(),
			HasProvisioningCapacity: false,

			ExpectedProvisioningState: metal3v1alpha1.StateReady,
			ExpectedDelayed:           true,
		},
		{
			Scenario:                "already-delayed-ok",
			Host:                    host(metal3v1alpha1.StateReady).SetOperationalStatus(metal3v1alpha1.OperationalStatusDelayed).build(),
			HasProvisioningCapacity: true,

			ExpectedProvisioningState: metal3v1alpha1.StateReady,
			ExpectedDelayed:           false,
		},

		{
			Scenario:                "untracked-inspecting-delayed",
			Host:                    host(metal3v1alpha1.StateInspecting).build(),
			HasProvisioningCapacity: false,

			ExpectedProvisioningState: metal3v1alpha1.StateInspecting,
			ExpectedDelayed:           true,
		},
		{
			Scenario:                "untracked-inspecting-ok",
			Host:                    host(metal3v1alpha1.StateInspecting).build(),
			HasProvisioningCapacity: true,

			ExpectedProvisioningState: metal3v1alpha1.StateMatchProfile,
			ExpectedDelayed:           false,
		},
		{
			Scenario:                "untracked-inspecting-delayed",
			Host:                    host(metal3v1alpha1.StateProvisioning).build(),
			HasProvisioningCapacity: false,

			ExpectedProvisioningState: metal3v1alpha1.StateProvisioning,
			ExpectedDelayed:           true,
		},
		{
			Scenario:                "untracked-provisioning-ok",
			Host:                    host(metal3v1alpha1.StateProvisioning).build(),
			HasProvisioningCapacity: true,

			ExpectedProvisioningState: metal3v1alpha1.StateProvisioned,
			ExpectedDelayed:           false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {
			prov := newMockProvisioner()
			prov.setHasCapacity(tc.HasProvisioningCapacity)
			hsm := newHostStateMachine(tc.Host, &BareMetalHostReconciler{}, prov, true)
			info := makeDefaultReconcileInfo(tc.Host)
			delayedProvisioningHostCounters.Reset()

			result := hsm.ReconcileState(info)

			assert.Equal(t, tc.ExpectedProvisioningState, tc.Host.Status.Provisioning.State)
			assert.Equal(t, tc.ExpectedDelayed, metal3v1alpha1.OperationalStatusDelayed == tc.Host.Status.OperationalStatus, "Expected OperationalStatusDelayed")
			assert.Equal(t, tc.ExpectedDelayed, assert.ObjectsAreEqual(actionDelayed{}, result), "Expected actionDelayed")

			if tc.ExpectedDelayed {
				counter, _ := delayedProvisioningHostCounters.GetMetricWith(hostMetricLabels(info.request))
				initialCounterValue := promutil.ToFloat64(counter)
				for _, sb := range info.postSaveCallbacks {
					sb()
				}
				assert.Greater(t, promutil.ToFloat64(counter), initialCounterValue)
			}
		})
	}
}

func TestDeprovisioningCapacity(t *testing.T) {
	testCases := []struct {
		Scenario string

		HasDeprovisioningCapacity bool
		Host                      *metal3v1alpha1.BareMetalHost

		ExpectedDeprovisioningState metal3v1alpha1.ProvisioningState
		ExpectedDelayed             bool
	}{
		{
			Scenario:                  "transition-to-deprovisionig-ready",
			Host:                      host(metal3v1alpha1.StateDeprovisioning).build(),
			HasDeprovisioningCapacity: true,

			ExpectedDeprovisioningState: metal3v1alpha1.StateReady,
			ExpectedDelayed:             false,
		},
		{
			Scenario:                  "transition-to-deleting",
			Host:                      host(metal3v1alpha1.StateDeprovisioning).setDeletion().build(),
			HasDeprovisioningCapacity: true,

			ExpectedDeprovisioningState: metal3v1alpha1.StateDeleting,
			ExpectedDelayed:             false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {
			prov := newMockProvisioner()
			prov.setHasCapacity(tc.HasDeprovisioningCapacity)
			hsm := newHostStateMachine(tc.Host, &BareMetalHostReconciler{}, prov, true)
			info := makeDefaultReconcileInfo(tc.Host)
			delayedDeprovisioningHostCounters.Reset()

			result := hsm.ReconcileState(info)

			assert.Equal(t, tc.ExpectedDeprovisioningState, tc.Host.Status.Provisioning.State)
			assert.Equal(t, tc.ExpectedDelayed, metal3v1alpha1.OperationalStatusDelayed == tc.Host.Status.OperationalStatus, "Expected OperationalStatusDelayed")
			assert.Equal(t, tc.ExpectedDelayed, assert.ObjectsAreEqual(actionDelayed{}, result), "Expected actionDelayed")

			if tc.ExpectedDelayed {
				counter, _ := delayedDeprovisioningHostCounters.GetMetricWith(hostMetricLabels(info.request))
				initialCounterValue := promutil.ToFloat64(counter)
				for _, sb := range info.postSaveCallbacks {
					sb()
				}
				assert.Greater(t, promutil.ToFloat64(counter), initialCounterValue)
			}
		})
	}
}

func TestDetach(t *testing.T) {
	testCases := []struct {
		Scenario                  string
		Host                      *metal3v1alpha1.BareMetalHost
		HasDetachedAnnotation     bool
		ExpectedDetach            bool
		ExpectedDirty             bool
		ExpectedOperationalStatus metal3v1alpha1.OperationalStatus
		ExpectedState             metal3v1alpha1.ProvisioningState
	}{
		{
			Scenario:                  "ProvisionedHost",
			Host:                      host(metal3v1alpha1.StateProvisioned).build(),
			ExpectedDetach:            false,
			ExpectedDirty:             false,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateProvisioned,
		},
		{
			Scenario:                  "DetachProvisionedHost",
			Host:                      host(metal3v1alpha1.StateProvisioned).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            true,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusDetached,
			ExpectedState:             metal3v1alpha1.StateProvisioned,
		},
		{
			Scenario:                  "DeleteDetachedProvisionedHost",
			Host:                      host(metal3v1alpha1.StateProvisioned).SetOperationalStatus(metal3v1alpha1.OperationalStatusDetached).setDeletion().build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusDetached,
			// Should move to Deleting without any Deprovisioning
			ExpectedState: metal3v1alpha1.StateDeleting,
		},
		{
			Scenario:                  "ExternallyProvisionedHost",
			Host:                      host(metal3v1alpha1.StateExternallyProvisioned).SetExternallyProvisioned().build(),
			HasDetachedAnnotation:     false,
			ExpectedDetach:            false,
			ExpectedDirty:             false,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateExternallyProvisioned,
		},
		{
			Scenario:                  "DetachExternallyProvisionedHost",
			Host:                      host(metal3v1alpha1.StateExternallyProvisioned).SetExternallyProvisioned().build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            true,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusDetached,
			ExpectedState:             metal3v1alpha1.StateExternallyProvisioned,
		},
		{
			Scenario:                  "NoneHost",
			Host:                      host(metal3v1alpha1.StateNone).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusDiscovered,
			ExpectedState:             metal3v1alpha1.StateUnmanaged,
		},
		{
			Scenario:                  "UnmanagedHost",
			Host:                      host(metal3v1alpha1.StateUnmanaged).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             false,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateUnmanaged,
		},
		{
			Scenario:                  "RegisteringHost",
			Host:                      host(metal3v1alpha1.StateRegistering).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateInspecting,
		},
		{
			Scenario:                  "InspectingHost",
			Host:                      host(metal3v1alpha1.StateInspecting).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateMatchProfile,
		},
		{
			Scenario:                  "MatchProfileHost",
			Host:                      host(metal3v1alpha1.StateMatchProfile).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StatePreparing,
		},
		{
			Scenario:                  "AvailableHost",
			Host:                      host(metal3v1alpha1.StateAvailable).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateProvisioning,
		},
		{
			Scenario:                  "PreparingHost",
			Host:                      host(metal3v1alpha1.StatePreparing).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateReady,
		},
		{
			Scenario:                  "ReadyHost",
			Host:                      host(metal3v1alpha1.StateReady).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateProvisioning,
		},
		{
			Scenario:                  "ProvisioningHost",
			Host:                      host(metal3v1alpha1.StateProvisioning).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateProvisioned,
		},
		{
			Scenario:                  "DeprovisioningHost",
			Host:                      host(metal3v1alpha1.StateDeprovisioning).build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             true,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateReady,
		},
		{
			Scenario:                  "DeletingHost",
			Host:                      host(metal3v1alpha1.StateDeleting).setDeletion().build(),
			HasDetachedAnnotation:     true,
			ExpectedDetach:            false,
			ExpectedDirty:             false,
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateDeleting,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {
			if tc.HasDetachedAnnotation {
				tc.Host.Annotations = map[string]string{
					metal3v1alpha1.DetachedAnnotation: "true",
				}
			}
			prov := newMockProvisioner()
			hsm := newHostStateMachine(tc.Host, &BareMetalHostReconciler{}, prov, true)
			info := makeDefaultReconcileInfo(tc.Host)
			result := hsm.ReconcileState(info)

			assert.Equal(t, tc.ExpectedDetach, prov.calledNoError("Detach"), "ExpectedDetach mismatch")
			assert.Equal(t, tc.ExpectedDirty, result.Dirty(), "ExpectedDirty mismatch")
			assert.Equal(t, tc.ExpectedOperationalStatus, info.host.OperationalStatus())
			assert.Equal(t, tc.ExpectedState, info.host.Status.Provisioning.State)
		})
	}
}

func TestDetachError(t *testing.T) {
	testCases := []struct {
		Scenario                  string
		Host                      *metal3v1alpha1.BareMetalHost
		ExpectedOperationalStatus metal3v1alpha1.OperationalStatus
		ExpectedState             metal3v1alpha1.ProvisioningState
		ClearError                bool
		RemoveAnnotation          bool
	}{
		{
			Scenario:                  "ProvisionerTemporaryError",
			Host:                      host(metal3v1alpha1.StateProvisioned).build(),
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusDetached,
			ExpectedState:             metal3v1alpha1.StateProvisioned,
			ClearError:                true,
		},
		{
			Scenario:                  "AnnotationRemovedAfterDetachError",
			Host:                      host(metal3v1alpha1.StateProvisioned).build(),
			ExpectedOperationalStatus: metal3v1alpha1.OperationalStatusOK,
			ExpectedState:             metal3v1alpha1.StateProvisioned,
			RemoveAnnotation:          true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {
			tc.Host.Annotations = map[string]string{
				metal3v1alpha1.DetachedAnnotation: "true",
			}
			prov := newMockProvisioner()
			hsm := newHostStateMachine(tc.Host, &BareMetalHostReconciler{}, prov, true)
			info := makeDefaultReconcileInfo(tc.Host)

			prov.setNextError("Detach", "some error")
			result := hsm.ReconcileState(info)
			assert.True(t, result.Dirty())
			assert.Equal(t, 1, tc.Host.Status.ErrorCount)
			assert.Equal(t, metal3v1alpha1.OperationalStatusError, info.host.OperationalStatus())
			assert.Equal(t, v1alpha1.DetachError, info.host.Status.ErrorType)
			assert.Equal(t, tc.ExpectedState, info.host.Status.Provisioning.State)

			if tc.ClearError {
				prov.clearNextError("Detach")
			}
			if tc.RemoveAnnotation {
				tc.Host.Annotations = map[string]string{}
			}
			result = hsm.ReconcileState(info)
			assert.Equal(t, 0, tc.Host.Status.ErrorCount)
			assert.True(t, result.Dirty())
			assert.Equal(t, tc.ExpectedOperationalStatus, info.host.OperationalStatus())
			assert.Equal(t, tc.ExpectedState, info.host.Status.Provisioning.State)
			assert.Empty(t, info.host.Status.ErrorType)
		})
	}
}

func TestProvisioningCancelled(t *testing.T) {
	testCases := []struct {
		Scenario string
		Host     metal3v1alpha1.BareMetalHost
		Expected bool
	}{
		{
			Scenario: "with image url, unprovisioned",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image: &metal3v1alpha1.Image{
						URL: "not-empty",
					},
					Online: true,
				},
			},
			Expected: false,
		},

		{
			Scenario: "with custom deploy, unprovisioned",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					CustomDeploy: &metal3v1alpha1.CustomDeploy{
						Method: "install_everything",
					},
					Online: true,
				},
			},
			Expected: false,
		},

		{
			Scenario: "with image, unprovisioned",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image:  &metal3v1alpha1.Image{},
					Online: true,
				},
			},
			Expected: true,
		},

		{
			Scenario: "without, unprovisioned",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Online: true,
				},
			},
			Expected: true,
		},

		{
			Scenario: "with image url, offline",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image: &metal3v1alpha1.Image{
						URL: "not-empty",
					},
					Online: false,
				},
			},
			Expected: false,
		},

		{
			Scenario: "with custom deploy, offline",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					CustomDeploy: &metal3v1alpha1.CustomDeploy{
						Method: "install_everything",
					},
					Online: false,
				},
			},
			Expected: false,
		},

		{
			Scenario: "provisioned with image",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image: &metal3v1alpha1.Image{
						URL: "same",
					},
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						Image: metal3v1alpha1.Image{
							URL: "same",
						},
					},
				},
			},
			Expected: false,
		},

		{
			Scenario: "provisioned with error",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image: &metal3v1alpha1.Image{
						URL: "same",
					},
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					ErrorType:    metal3v1alpha1.ProvisionedRegistrationError,
					ErrorMessage: "Adoption failed",
					ErrorCount:   1,
					Provisioning: metal3v1alpha1.ProvisionStatus{
						Image: metal3v1alpha1.Image{
							URL: "same",
						},
					},
				},
			},
			Expected: false,
		},

		{

			Scenario: "provisioned with custom deploy",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					CustomDeploy: &metal3v1alpha1.CustomDeploy{
						Method: "install_everything",
					},
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						CustomDeploy: &metal3v1alpha1.CustomDeploy{
							Method: "install_everything",
						},
					},
				},
			},
			Expected: false,
		},

		{
			Scenario: "removed image",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						Image: metal3v1alpha1.Image{
							URL: "same",
						},
					},
				},
			},
			Expected: true,
		},

		{
			Scenario: "removed custom deploy",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						CustomDeploy: &metal3v1alpha1.CustomDeploy{
							Method: "install_everything",
						},
					},
				},
			},
			Expected: true,
		},

		{
			Scenario: "changed image",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image: &metal3v1alpha1.Image{
						URL: "not-empty",
					},
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						Image: metal3v1alpha1.Image{
							URL: "also-not-empty",
						},
					},
				},
			},
			Expected: true,
		},

		{
			Scenario: "changed custom deploy",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					CustomDeploy: &metal3v1alpha1.CustomDeploy{
						Method: "install_not_everything",
					},
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						CustomDeploy: &metal3v1alpha1.CustomDeploy{
							Method: "install_everything",
						},
					},
				},
			},
			Expected: true,
		},

		{
			Scenario: "changed image with custom deploy",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image: &metal3v1alpha1.Image{
						URL: "not-empty",
					},
					CustomDeploy: &metal3v1alpha1.CustomDeploy{
						Method: "install_everything",
					},
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						Image: metal3v1alpha1.Image{
							URL: "also-not-empty",
						},
						CustomDeploy: &metal3v1alpha1.CustomDeploy{
							Method: "install_everything",
						},
					},
				},
			},
			Expected: true,
		},

		{
			Scenario: "changed custom deploy with image",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image: &metal3v1alpha1.Image{
						URL: "not-empty",
					},
					CustomDeploy: &metal3v1alpha1.CustomDeploy{
						Method: "install_not_everything",
					},
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						Image: metal3v1alpha1.Image{
							URL: "not-empty",
						},
						CustomDeploy: &metal3v1alpha1.CustomDeploy{
							Method: "install_everything",
						},
					},
				},
			},
			Expected: true,
		},

		{
			Scenario: "removed custom deploy with image",
			Host: metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image: &metal3v1alpha1.Image{
						URL: "not-empty",
					},
					Online: true,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						Image: metal3v1alpha1.Image{
							URL: "not-empty",
						},
						CustomDeploy: &metal3v1alpha1.CustomDeploy{
							Method: "install_everything",
						},
					},
				},
			},
			Expected: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {
			hsm := testStateMachine(&tc.Host)
			actual := hsm.provisioningCancelled()
			if tc.Expected && !actual {
				t.Error("expected to need deprovisioning")
			}
			if !tc.Expected && actual {
				t.Error("did not expect to need deprovisioning")
			}
		})
	}
}

func TestErrorCountIncreasedOnActionFailure(t *testing.T) {

	tests := []struct {
		Scenario           string
		Host               *metal3v1alpha1.BareMetalHost
		ProvisionerErrorOn string
	}{
		{
			Scenario:           "registration",
			Host:               host(metal3v1alpha1.StateRegistering).build(),
			ProvisionerErrorOn: "ValidateManagementAccess",
		},
		{
			Scenario:           "inspecting",
			Host:               host(metal3v1alpha1.StateInspecting).build(),
			ProvisionerErrorOn: "InspectHardware",
		},
		{
			Scenario:           "provisioning",
			Host:               host(metal3v1alpha1.StateProvisioning).SetImageURL("imageSpecUrl").build(),
			ProvisionerErrorOn: "Provision",
		},
		{
			Scenario:           "deprovisioning",
			Host:               host(metal3v1alpha1.StateDeprovisioning).build(),
			ProvisionerErrorOn: "Deprovision",
		},
		{
			Scenario:           "ready-power-on",
			Host:               host(metal3v1alpha1.StateReady).SetImageURL("").SetStatusPoweredOn(false).build(),
			ProvisionerErrorOn: "PowerOn",
		},
		{
			Scenario:           "ready-power-off",
			Host:               host(metal3v1alpha1.StateReady).SetImageURL("").SetOnline(false).build(),
			ProvisionerErrorOn: "PowerOff",
		},
		{
			Scenario:           "externally-provisioned-adopt-failed",
			Host:               host(metal3v1alpha1.StateExternallyProvisioned).SetExternallyProvisioned().build(),
			ProvisionerErrorOn: "Adopt",
		},
		{
			Scenario:           "provisioned-adopt-failed",
			Host:               host(metal3v1alpha1.StateProvisioned).build(),
			ProvisionerErrorOn: "Adopt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Scenario, func(t *testing.T) {
			prov := newMockProvisioner()
			hsm := newHostStateMachine(tt.Host, &BareMetalHostReconciler{}, prov, true)
			info := makeDefaultReconcileInfo(tt.Host)

			prov.setNextError(tt.ProvisionerErrorOn, "some error")
			result := hsm.ReconcileState(info)

			assert.Equal(t, 1, tt.Host.Status.ErrorCount)
			assert.Equal(t, "some error", tt.Host.Status.ErrorMessage)
			assert.True(t, result.Dirty())
		})
	}
}

func TestErrorCountClearedOnStateTransition(t *testing.T) {

	tests := []struct {
		Scenario                     string
		Host                         *metal3v1alpha1.BareMetalHost
		TargetState                  metal3v1alpha1.ProvisioningState
		PreserveErrorCountOnComplete bool
	}{
		{
			Scenario:    "registering-to-inspecting",
			Host:        host(metal3v1alpha1.StateRegistering).build(),
			TargetState: metal3v1alpha1.StateInspecting,
		},
		{
			Scenario:    "inspecting-to-matchprofile",
			Host:        host(metal3v1alpha1.StateInspecting).build(),
			TargetState: metal3v1alpha1.StateMatchProfile,
		},
		{
			Scenario:    "matchprofile-to-preparing",
			Host:        host(metal3v1alpha1.StateMatchProfile).build(),
			TargetState: metal3v1alpha1.StatePreparing,
		},
		{
			Scenario:    "preparing-to-ready",
			Host:        host(metal3v1alpha1.StatePreparing).build(),
			TargetState: metal3v1alpha1.StateReady,
		},
		{
			Scenario:    "provisioning-to-provisioned",
			Host:        host(metal3v1alpha1.StateProvisioning).build(),
			TargetState: metal3v1alpha1.StateProvisioned,
		},
		{
			Scenario:    "deprovisioning-to-ready",
			Host:        host(metal3v1alpha1.StateDeprovisioning).build(),
			TargetState: metal3v1alpha1.StateReady,
		},
		{
			Scenario:    "deprovisioning-to-deleting",
			Host:        host(metal3v1alpha1.StateDeprovisioning).setDeletion().build(),
			TargetState: metal3v1alpha1.StateDeleting,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Scenario, func(t *testing.T) {
			prov := newMockProvisioner()
			hsm := newHostStateMachine(tt.Host, &BareMetalHostReconciler{}, prov, true)
			info := makeDefaultReconcileInfo(tt.Host)

			info.host.Status.ErrorCount = 1
			hsm.ReconcileState(info)

			assert.Equal(t, tt.TargetState, info.host.Status.Provisioning.State)
			assert.Equal(t, info.host.Status.ErrorCount, 0)
		})
	}
}

func TestErrorClean(t *testing.T) {

	tests := []struct {
		Scenario    string
		Host        *metal3v1alpha1.BareMetalHost
		SecretName  string
		ExpectError bool
	}{
		{
			Scenario: "clean-after-registration-error",
			Host: host(metal3v1alpha1.StateInspecting).
				SetStatusError(metal3v1alpha1.OperationalStatusError, metal3v1alpha1.RegistrationError, "some error", 1).
				build(),
		},
		{
			Scenario: "clean-after-creds-change",
			Host: host(metal3v1alpha1.StateReady).
				SetStatusError(metal3v1alpha1.OperationalStatusError, metal3v1alpha1.InspectionError, "some error", 1).
				build(),
			SecretName: "NewCreds",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Scenario, func(t *testing.T) {
			prov := newMockProvisioner()
			hsm := newHostStateMachine(tt.Host, &BareMetalHostReconciler{}, prov, true)

			info := makeDefaultReconcileInfo(tt.Host)
			if tt.SecretName != "" {
				info.bmcCredsSecret.Name = tt.SecretName
			}

			hsm.ReconcileState(info)

			if tt.ExpectError {
				assert.Equal(t, v1alpha1.ProvisionedRegistrationError, tt.Host.Status.ErrorType)
				assert.NotEmpty(t, tt.Host.Status.ErrorMessage)
			} else {
				assert.Equal(t, v1alpha1.OperationalStatusOK, tt.Host.Status.OperationalStatus)
				assert.Empty(t, tt.Host.Status.ErrorType)
				assert.Empty(t, tt.Host.Status.ErrorMessage)
			}
		})
	}
}

type hostBuilder struct {
	metal3v1alpha1.BareMetalHost
}

func host(state metal3v1alpha1.ProvisioningState) *hostBuilder {

	creds := metal3v1alpha1.CredentialsStatus{
		Reference: &corev1.SecretReference{
			Name:      "secretRefName",
			Namespace: "secretNs",
		},
		Version: "100",
	}

	return &hostBuilder{
		metal3v1alpha1.BareMetalHost{
			Spec: v1alpha1.BareMetalHostSpec{
				Online: true,
				Image: &v1alpha1.Image{
					URL: "not-empty",
				},
				RootDeviceHints: &v1alpha1.RootDeviceHints{},
			},
			Status: metal3v1alpha1.BareMetalHostStatus{
				HardwareProfile: hardware.DefaultProfileName,
				Provisioning: metal3v1alpha1.ProvisionStatus{
					State:           state,
					BootMode:        v1alpha1.DefaultBootMode,
					RootDeviceHints: &metal3v1alpha1.RootDeviceHints{},
					Image: v1alpha1.Image{
						URL: "", //needs provisioning
					},
					RAID: &metal3v1alpha1.RAIDConfig{
						SoftwareRAIDVolumes: []metal3v1alpha1.SoftwareRAIDVolume{},
					},
				},
				GoodCredentials:   creds,
				TriedCredentials:  creds,
				OperationalStatus: metal3v1alpha1.OperationalStatusOK,
				PoweredOn:         true,
			},
		},
	}
}

func (hb *hostBuilder) build() *metal3v1alpha1.BareMetalHost {
	return &hb.BareMetalHost
}

func (hb *hostBuilder) SaveHostProvisioningSettings() *hostBuilder {
	saveHostProvisioningSettings(&hb.BareMetalHost)
	return hb
}

func (hb *hostBuilder) SetTriedCredentials() *hostBuilder {
	hb.Status.TriedCredentials = hb.Status.GoodCredentials
	return hb
}

func (hb *hostBuilder) SetExternallyProvisioned() *hostBuilder {
	hb.Spec.ExternallyProvisioned = true
	return hb
}

func (hb *hostBuilder) SetImageURL(url string) *hostBuilder {
	hb.Spec.Image = &metal3v1alpha1.Image{
		URL: url,
	}
	return hb
}

func (hb *hostBuilder) SetStatusError(opStatus metal3v1alpha1.OperationalStatus, errType metal3v1alpha1.ErrorType, errMsg string, errCount int) *hostBuilder {
	hb.Status.OperationalStatus = opStatus
	hb.Status.ErrorType = errType
	hb.Status.ErrorMessage = errMsg
	hb.Status.ErrorCount = errCount

	return hb
}

func (hb *hostBuilder) SetStatusImageURL(url string) *hostBuilder {
	hb.Status.Provisioning.Image.URL = url
	return hb
}

func (hb *hostBuilder) SetStatusPoweredOn(status bool) *hostBuilder {
	hb.Status.PoweredOn = status
	return hb
}

func (hb *hostBuilder) SetOnline(status bool) *hostBuilder {
	hb.Spec.Online = status
	return hb
}

func (hb *hostBuilder) SetOperationalStatus(status metal3v1alpha1.OperationalStatus) *hostBuilder {
	hb.Status.OperationalStatus = status
	return hb
}

func (hb *hostBuilder) setDeletion() *hostBuilder {
	date := metav1.Date(2021, time.January, 18, 10, 18, 0, 0, time.UTC)
	hb.DeletionTimestamp = &date
	return hb
}

func makeDefaultReconcileInfo(host *metal3v1alpha1.BareMetalHost) *reconcileInfo {
	return &reconcileInfo{
		log:     logf.Log.WithName("controllers").WithName("BareMetalHost").WithName("host_state_machine"),
		host:    host,
		request: ctrl.Request{},
		bmcCredsSecret: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            host.Status.GoodCredentials.Reference.Name,
				Namespace:       host.Status.GoodCredentials.Reference.Namespace,
				ResourceVersion: host.Status.GoodCredentials.Version,
			},
		},
	}
}

func newMockProvisioner() *mockProvisioner {
	return &mockProvisioner{
		hasCapacity:  true,
		nextResults:  make(map[string]provisioner.Result),
		callsNoError: make(map[string]bool),
	}
}

type mockProvisioner struct {
	hasCapacity  bool
	nextResults  map[string]provisioner.Result
	callsNoError map[string]bool
}

func (m *mockProvisioner) getNextResultByMethod(name string) (result provisioner.Result) {
	if value, ok := m.nextResults[name]; ok {
		result = value
	} else {
		m.callsNoError[name] = true
	}
	return
}

func (m *mockProvisioner) setHasCapacity(hasCapacity bool) {
	m.hasCapacity = hasCapacity
}

func (m *mockProvisioner) HasCapacity() (result bool, err error) {
	return m.hasCapacity, nil
}

func (m *mockProvisioner) setNextError(methodName, msg string) {
	m.nextResults[methodName] = provisioner.Result{
		ErrorMessage: msg,
	}
}

func (m *mockProvisioner) clearNextError(methodName string) {
	m.nextResults[methodName] = provisioner.Result{}
}

func (m *mockProvisioner) calledNoError(methodName string) bool {
	return m.callsNoError[methodName]
}

func (m *mockProvisioner) ValidateManagementAccess(data provisioner.ManagementAccessData, credentialsChanged, force bool) (result provisioner.Result, provID string, err error) {
	return m.getNextResultByMethod("ValidateManagementAccess"), "", err
}

func (m *mockProvisioner) InspectHardware(data provisioner.InspectData, force, refresh bool) (result provisioner.Result, started bool, details *metal3v1alpha1.HardwareDetails, err error) {
	details = &metal3v1alpha1.HardwareDetails{}
	return m.getNextResultByMethod("InspectHardware"), true, details, err
}

func (m *mockProvisioner) UpdateHardwareState() (hwState provisioner.HardwareState, err error) {
	return
}

func (m *mockProvisioner) Prepare(data provisioner.PrepareData, unprepared bool) (result provisioner.Result, started bool, err error) {
	return m.getNextResultByMethod("Prepare"), m.nextResults["Prepare"].Dirty, err
}

func (m *mockProvisioner) Adopt(data provisioner.AdoptData, force bool) (result provisioner.Result, err error) {
	return m.getNextResultByMethod("Adopt"), err
}

func (m *mockProvisioner) Provision(data provisioner.ProvisionData) (result provisioner.Result, err error) {
	return m.getNextResultByMethod("Provision"), err
}

func (m *mockProvisioner) Deprovision(force bool) (result provisioner.Result, err error) {
	return m.getNextResultByMethod("Deprovision"), err
}

func (m *mockProvisioner) Delete() (result provisioner.Result, err error) {
	return m.getNextResultByMethod("Delete"), err
}

func (m *mockProvisioner) Detach() (result provisioner.Result, err error) {
	res := m.getNextResultByMethod("Detach")
	return res, err
}

func (m *mockProvisioner) PowerOn(force bool) (result provisioner.Result, err error) {
	return m.getNextResultByMethod("PowerOn"), err
}

func (m *mockProvisioner) PowerOff(rebootMode metal3v1alpha1.RebootMode, force bool) (result provisioner.Result, err error) {
	return m.getNextResultByMethod("PowerOff"), err
}

func (m *mockProvisioner) IsReady() (result bool, err error) {
	return
}

func TestUpdateBootModeStatus(t *testing.T) {
	testCases := []struct {
		Scenario       string
		SpecValue      metal3v1alpha1.BootMode
		StatusValue    metal3v1alpha1.BootMode
		ExpectedValue  metal3v1alpha1.BootMode
		ExpectedChange bool
	}{
		{
			Scenario:       "default",
			SpecValue:      "",
			StatusValue:    "",
			ExpectedValue:  metal3v1alpha1.DefaultBootMode,
			ExpectedChange: true,
		},

		{
			Scenario:       "set UEFI",
			SpecValue:      metal3v1alpha1.UEFI,
			StatusValue:    "",
			ExpectedValue:  metal3v1alpha1.UEFI,
			ExpectedChange: true,
		},

		{
			Scenario:       "already UEFI",
			SpecValue:      metal3v1alpha1.UEFI,
			StatusValue:    metal3v1alpha1.UEFI,
			ExpectedValue:  metal3v1alpha1.UEFI,
			ExpectedChange: false,
		},

		{
			Scenario:       "set Legacy",
			SpecValue:      metal3v1alpha1.Legacy,
			StatusValue:    "",
			ExpectedValue:  metal3v1alpha1.Legacy,
			ExpectedChange: true,
		},

		{
			Scenario:       "already Legacy",
			SpecValue:      metal3v1alpha1.Legacy,
			StatusValue:    metal3v1alpha1.Legacy,
			ExpectedValue:  metal3v1alpha1.Legacy,
			ExpectedChange: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {
			host := metal3v1alpha1.BareMetalHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myhost",
					Namespace: "myns",
				},
				Spec: metal3v1alpha1.BareMetalHostSpec{
					Image: &metal3v1alpha1.Image{
						URL: "not-empty",
					},
					Online:   true,
					BootMode: tc.SpecValue,
				},
				Status: metal3v1alpha1.BareMetalHostStatus{
					Provisioning: metal3v1alpha1.ProvisionStatus{
						Image: metal3v1alpha1.Image{
							URL: "also-not-empty",
						},
						BootMode: tc.StatusValue,
					},
				},
			}
			changed := updateBootModeStatus(&host)
			assert.Equal(t, tc.ExpectedChange, changed, "unexpected change response")
			assert.Equal(t, tc.ExpectedValue, host.Status.Provisioning.BootMode)
		})
	}
}
