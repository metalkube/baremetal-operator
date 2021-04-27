package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidate(t *testing.T) {
	tm := metav1.TypeMeta{
		Kind:       "BareMetalHost",
		APIVersion: "metal3.io/v1alpha1",
	}

	om := metav1.ObjectMeta{
		Name:      "test",
		Namespace: "test-namespace",
	}

	tests := []struct {
		name      string
		newBMH    *BareMetalHost
		oldBMH    *BareMetalHost
		wantedErr string
	}{
		{
			name:      "valid",
			newBMH:    &BareMetalHost{TypeMeta: tm, ObjectMeta: om, Spec: BareMetalHostSpec{}},
			oldBMH:    nil,
			wantedErr: "",
		},
		{
			name: "invalidRAID",
			newBMH: &BareMetalHost{TypeMeta: tm, ObjectMeta: om, Spec: BareMetalHostSpec{RAID: &RAIDConfig{
				HardwareRAIDVolumes: []HardwareRAIDVolume{
					{
						SizeGibibytes:         nil,
						Level:                 "",
						Name:                  "",
						Rotational:            nil,
						NumberOfPhysicalDisks: nil,
					},
				},
				SoftwareRAIDVolumes: []SoftwareRAIDVolume{
					{
						SizeGibibytes: nil,
						Level:         "",
						PhysicalDisks: nil,
					},
				},
			}}},
			oldBMH:    nil,
			wantedErr: "hardwareRAIDVolumes and softwareRAIDVolumes can not be set at the same time",
		},
		{
			name:      "updateAddress",
			newBMH:    &BareMetalHost{TypeMeta: tm, ObjectMeta: om, Spec: BareMetalHostSpec{BMC: BMCDetails{Address: "test-address-changed"}}},
			oldBMH:    &BareMetalHost{TypeMeta: tm, ObjectMeta: om, Spec: BareMetalHostSpec{BMC: BMCDetails{Address: "test-address"}}},
			wantedErr: "BMC Address can not be changed once it is set",
		},
		{
			name:      "updateBootMAC",
			newBMH:    &BareMetalHost{TypeMeta: tm, ObjectMeta: om, Spec: BareMetalHostSpec{BootMACAddress: "test-mac-changed"}},
			oldBMH:    &BareMetalHost{TypeMeta: tm, ObjectMeta: om, Spec: BareMetalHostSpec{BMC: BMCDetails{Address: "test-mac"}}},
			wantedErr: "BootMACAddress can not be changed once it is set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.newBMH.Validate(tt.oldBMH); !errorContains(err, tt.wantedErr) {
				t.Errorf("BareMetalHost.ValidateBareMetalHost() error = %v, wantErr %v", err, tt.wantedErr)
			}
		})
	}
}
