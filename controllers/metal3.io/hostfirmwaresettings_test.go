package controllers

import (
	"context"
	"testing"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/baremetal-operator/pkg/provisioner"

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	hostName      string = "myHostName"
	hostNamespace string = "myHostNamespace"
	schemaName    string = "schema-6579d6c1" // Hash generated from schema, change this if the schema is changed
)

var (
	iTrue      bool = true
	iFalse     bool = false
	minLength  int  = 0
	maxLength  int  = 20
	lowerBound int  = 0
	upperBound int  = 20
)

// Test support for HostFirmwareSettings in the BareMetalHostReconciler
func getTestHostReconciler(host *metal3v1alpha1.BareMetalHost) *BareMetalHostReconciler {

	c := fakeclient.NewFakeClient(host)
	reconciler := &BareMetalHostReconciler{
		Client:             c,
		ProvisionerFactory: nil,
		Log:                ctrl.Log.WithName("host_state_machine").WithName("BareMetalHost"),
	}

	return reconciler
}

// Test support for HostFirmwareSettings in the HostFirmwareSettingsReconciler
func getTestHFSReconciler(host *metal3v1alpha1.HostFirmwareSettings) *HostFirmwareSettingsReconciler {

	c := fakeclient.NewFakeClient(host)
	reconciler := &HostFirmwareSettingsReconciler{
		Client: c,
		Log:    ctrl.Log.WithName("test_reconciler").WithName("HostFirmwareSettings"),
	}

	return reconciler
}

func getDefaultHostReconcileInfo(host *metal3v1alpha1.BareMetalHost, name string, namespace string) *reconcileInfo {
	r := &reconcileInfo{
		log:     logf.Log.WithName("controllers").WithName("HostFirmwareSettings"),
		host:    host,
		request: ctrl.Request{},
	}
	r.request.NamespacedName = types.NamespacedName{Namespace: namespace, Name: name}

	return r
}

func getMockProvisioner(settings metal3v1alpha1.SettingsMap, schema map[string]metal3v1alpha1.SettingSchema) *hsfMockProvisioner {
	return &hsfMockProvisioner{
		Settings: settings,
		Schema:   schema,
		Error:    nil,
	}
}

type hsfMockProvisioner struct {
	Settings metal3v1alpha1.SettingsMap
	Schema   map[string]metal3v1alpha1.SettingSchema
	Error    error
}

func (m *hsfMockProvisioner) HasCapacity() (result bool, err error) {
	return
}

func (m *hsfMockProvisioner) ValidateManagementAccess(data provisioner.ManagementAccessData, credentialsChanged, force bool) (result provisioner.Result, provID string, err error) {
	return
}

func (m *hsfMockProvisioner) InspectHardware(data provisioner.InspectData, force, refresh bool) (result provisioner.Result, started bool, details *metal3v1alpha1.HardwareDetails, err error) {
	return
}

func (m *hsfMockProvisioner) UpdateHardwareState() (hwState provisioner.HardwareState, err error) {
	return
}

func (m *hsfMockProvisioner) Prepare(data provisioner.PrepareData, unprepared bool) (result provisioner.Result, started bool, err error) {
	return
}

func (m *hsfMockProvisioner) Adopt(data provisioner.AdoptData, force bool) (result provisioner.Result, err error) {
	return
}

func (m *hsfMockProvisioner) Provision(data provisioner.ProvisionData) (result provisioner.Result, err error) {
	return
}

func (m *hsfMockProvisioner) Deprovision(force bool) (result provisioner.Result, err error) {
	return
}

func (m *hsfMockProvisioner) Delete() (result provisioner.Result, err error) {
	return
}

func (m *hsfMockProvisioner) Detach() (result provisioner.Result, err error) {
	return
}

func (m *hsfMockProvisioner) PowerOn(force bool) (result provisioner.Result, err error) {
	return
}

func (m *hsfMockProvisioner) PowerOff(rebootMode metal3v1alpha1.RebootMode, force bool) (result provisioner.Result, err error) {
	return
}

func (m *hsfMockProvisioner) IsReady() (result bool, err error) {
	return
}

func (m *hsfMockProvisioner) GetFirmwareSettings(includeSchema bool) (settings metal3v1alpha1.SettingsMap, schema map[string]metal3v1alpha1.SettingSchema, err error) {

	return m.Settings, m.Schema, m.Error
}

func createSchemaResource(ctx context.Context, r *BareMetalHostReconciler) {

	firmwareSchema := &metal3v1alpha1.FirmwareSchema{
		TypeMeta: metav1.TypeMeta{
			Kind:       "FirmwareSchema",
			APIVersion: "metal3.io/v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      schemaName,
			Namespace: hostNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "metal3.io/v1alpha1",
					Kind:       "HostFirmwareSettings",
					Name:       hostName,
				},
			},
		},
		Spec: metal3v1alpha1.FirmwareSchemaSpec{
			Schema: map[string]metal3v1alpha1.SettingSchema{
				"AssetTag": {
					AttributeType: "String",
					MinLength:     &minLength,
					MaxLength:     &maxLength,
					Unique:        &iTrue,
				},
				"CustomPostMessage": {
					AttributeType: "String",
					MinLength:     &minLength,
					MaxLength:     &maxLength,
					Unique:        &iFalse,
					ReadOnly:      &iFalse,
				},
				"L2Cache": {
					AttributeType:   "String",
					AllowableValues: []string(nil),
					MinLength:       &minLength,
					MaxLength:       &maxLength,
					ReadOnly:        &iTrue,
				},
				"NetworkBootRetryCount": {
					AttributeType:   "Integer",
					AllowableValues: []string(nil),
					LowerBound:      &lowerBound,
					UpperBound:      &upperBound,
					ReadOnly:        &iFalse,
				},
				"ProcVirtualization": {
					AttributeType:   "Enumeration",
					AllowableValues: []string{"Enabled", "Disabled"},
					ReadOnly:        &iFalse,
				},
			},
		},
	}

	r.Client.Create(ctx, firmwareSchema)
}

func createHFSResource(ctx context.Context, r *BareMetalHostReconciler, hfs *metal3v1alpha1.HostFirmwareSettings, createSchema bool) {

	hfs.Status = metal3v1alpha1.HostFirmwareSettingsStatus{
		Settings: metal3v1alpha1.SettingsMap{
			"CustomPostMessage":     "All tests passed",
			"L2Cache":               "10x256 KB",
			"NetworkBootRetryCount": "10",
			"ProcVirtualization":    "Enabled",
			"SecureBoot":            "Enabled",
			"AssetTag":              "X45672917",
		},
	}
	hfs.TypeMeta = metav1.TypeMeta{
		Kind:       "HostFirmwareSettings",
		APIVersion: "metal3.io/v1alpha1"}
	hfs.ObjectMeta = metav1.ObjectMeta{
		Name:      hostName,
		Namespace: hostNamespace}

	if createSchema {
		hfs.Status.FirmwareSchema =
			&metal3v1alpha1.SchemaReference{
				Name:      schemaName,
				Namespace: hostNamespace}
	}

	r.Client.Create(ctx, hfs)
	r.Client.Update(ctx, hfs)
}

func TestStoreHostFirmwareSettings(t *testing.T) {

	testCases := []struct {
		Scenario string
		// the existing resources
		CurrentHFSResource   *metal3v1alpha1.HostFirmwareSettings
		CreateSchemaResource bool
		// mock data returned from Ironic via the provisioner
		CurrentSettings metal3v1alpha1.SettingsMap
		CurrentSchema   map[string]metal3v1alpha1.SettingSchema
		// the expected created or updated resources
		ExpectedSettings *metal3v1alpha1.HostFirmwareSettings
		ExpectedSchema   *metal3v1alpha1.FirmwareSchema
	}{
		{
			Scenario:             "new settings only",
			CurrentHFSResource:   nil,
			CreateSchemaResource: false,
			CurrentSettings: metal3v1alpha1.SettingsMap{
				"L2Cache":               "10x256 KB",
				"NetworkBootRetryCount": "10",
				"ProcVirtualization":    "Enabled",
				"SysPassword":           "",
			},
			CurrentSchema: map[string]metal3v1alpha1.SettingSchema{},
			ExpectedSettings: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"L2Cache":               intstr.FromString("10x256 KB"),
						"NetworkBootRetryCount": intstr.FromString("10"),
						"ProcVirtualization":    intstr.FromString("Enabled"),
					},
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Settings: metal3v1alpha1.SettingsMap{
						"L2Cache":               "10x256 KB",
						"NetworkBootRetryCount": "10",
						"ProcVirtualization":    "Enabled",
					},
				},
			},
			ExpectedSchema: nil,
		},
		{
			Scenario:             "new settings and schema",
			CurrentHFSResource:   nil,
			CreateSchemaResource: false,
			CurrentSettings: metal3v1alpha1.SettingsMap{
				"AssetTag":              "X45672917",
				"L2Cache":               "10x256 KB",
				"NetworkBootRetryCount": "10",
				"ProcVirtualization":    "Enabled",
			},
			CurrentSchema: map[string]metal3v1alpha1.SettingSchema{
				"AssetTag": {
					AttributeType: "String",
					MinLength:     &minLength,
					MaxLength:     &maxLength,
					Unique:        &iTrue,
				},
				"CustomPostMessage": {
					AttributeType: "String",
					MinLength:     &minLength,
					MaxLength:     &maxLength,
					Unique:        &iFalse,
					ReadOnly:      &iFalse,
				},
				"L2Cache": {
					AttributeType: "String",
					MinLength:     &minLength,
					MaxLength:     &maxLength,
					ReadOnly:      &iTrue,
				},
				"NetworkBootRetryCount": {
					AttributeType: "Integer",
					LowerBound:    &lowerBound,
					UpperBound:    &upperBound,
					ReadOnly:      &iFalse,
				},
				"ProcVirtualization": {
					AttributeType:   "Enumeration",
					AllowableValues: []string{"Enabled", "Disabled"},
					ReadOnly:        &iFalse,
				},
			},
			ExpectedSettings: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						// ReadOnly and Unique settings are not in Spec
						"NetworkBootRetryCount": intstr.FromString("10"),
						"ProcVirtualization":    intstr.FromString("Enabled"),
					},
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					FirmwareSchema: &metal3v1alpha1.SchemaReference{
						Name:      schemaName,
						Namespace: hostNamespace,
					},
					Settings: metal3v1alpha1.SettingsMap{
						"AssetTag":              "X45672917",
						"L2Cache":               "10x256 KB",
						"NetworkBootRetryCount": "10",
						"ProcVirtualization":    "Enabled",
					},
				},
			},
			ExpectedSchema: &metal3v1alpha1.FirmwareSchema{
				TypeMeta: metav1.TypeMeta{
					Kind:       "FirmwareSchema",
					APIVersion: "metal3.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:            schemaName,
					Namespace:       hostNamespace,
					ResourceVersion: "2",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "metal3.io/v1alpha1",
							Kind:       "HostFirmwareSettings",
							Name:       hostName,
						},
					},
				},
				Spec: metal3v1alpha1.FirmwareSchemaSpec{
					Schema: map[string]metal3v1alpha1.SettingSchema{
						"AssetTag": {
							AttributeType:   "String",
							AllowableValues: []string(nil),
							MinLength:       &minLength,
							MaxLength:       &maxLength,
							Unique:          &iTrue,
						},
						"CustomPostMessage": {
							AttributeType: "String",
							MinLength:     &minLength,
							MaxLength:     &maxLength,
							Unique:        &iFalse,
							ReadOnly:      &iFalse,
						},

						"L2Cache": {
							AttributeType:   "String",
							AllowableValues: []string(nil),
							MinLength:       &minLength,
							MaxLength:       &maxLength,
							ReadOnly:        &iTrue,
						},
						"NetworkBootRetryCount": {
							AttributeType:   "Integer",
							AllowableValues: []string(nil),
							LowerBound:      &lowerBound,
							UpperBound:      &upperBound,
							ReadOnly:        &iFalse,
						},
						"ProcVirtualization": {
							AttributeType:   "Enumeration",
							AllowableValues: []string{"Enabled", "Disabled"},
							ReadOnly:        &iFalse,
						},
					},
				},
			},
		},
		{
			Scenario: "updated settings no schema",
			CurrentHFSResource: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"L2Cache":               intstr.FromString("10x256 KB"),
						"NetworkBootRetryCount": intstr.FromString("10"),
						"ProcVirtualization":    intstr.FromString("Enabled"),
					},
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Settings: metal3v1alpha1.SettingsMap{
						"L2Cache":               "10x256 KB",
						"NetworkBootRetryCount": "10",
						"ProcVirtualization":    "Enabled",
					},
				},
			},
			CreateSchemaResource: false,
			CurrentSettings: metal3v1alpha1.SettingsMap{
				"L2Cache":               "10x512 KB",
				"NetworkBootRetryCount": "20",
				"ProcVirtualization":    "Disabled",
				"SecureBoot":            "Enabled",
				"AssetTag":              "X45672917",
			},
			CurrentSchema: map[string]metal3v1alpha1.SettingSchema{},
			ExpectedSettings: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"AssetTag":              intstr.FromString("X45672917"),
						"L2Cache":               intstr.FromString("10x256 KB"),
						"NetworkBootRetryCount": intstr.FromString("10"),
						"ProcVirtualization":    intstr.FromString("Enabled"),
						"SecureBoot":            intstr.FromString("Enabled"),
					},
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					Settings: metal3v1alpha1.SettingsMap{
						"AssetTag":              "X45672917",
						"L2Cache":               "10x512 KB",
						"NetworkBootRetryCount": "20",
						"ProcVirtualization":    "Disabled",
						"SecureBoot":            "Enabled",
					},
				},
			},
			ExpectedSchema: nil,
		},
		{
			Scenario:             "new settings existing schema",
			CurrentHFSResource:   nil,
			CreateSchemaResource: true,
			CurrentSettings: metal3v1alpha1.SettingsMap{
				"L2Cache":               "10x512 KB",
				"NetworkBootRetryCount": "10",
				"ProcVirtualization":    "Disabled",
			},
			CurrentSchema: map[string]metal3v1alpha1.SettingSchema{
				"AssetTag": {
					AttributeType: "String",
					MinLength:     &minLength,
					MaxLength:     &maxLength,
					Unique:        &iTrue,
				},
				"CustomPostMessage": {
					AttributeType: "String",
					MinLength:     &minLength,
					MaxLength:     &maxLength,
					Unique:        &iFalse,
					ReadOnly:      &iFalse,
				},
				"L2Cache": {
					AttributeType: "String",
					MinLength:     &minLength,
					MaxLength:     &maxLength,
					ReadOnly:      &iTrue,
				},
				"NetworkBootRetryCount": {
					AttributeType:   "Integer",
					AllowableValues: []string(nil),
					LowerBound:      &lowerBound,
					UpperBound:      &upperBound,
					ReadOnly:        &iFalse,
				},
				"ProcVirtualization": {
					AttributeType:   "Enumeration",
					AllowableValues: []string{"Enabled", "Disabled"},
					ReadOnly:        &iFalse,
				},
			},
			ExpectedSettings: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"NetworkBootRetryCount": intstr.FromString("10"),
						"ProcVirtualization":    intstr.FromString("Disabled"),
					},
				},
				Status: metal3v1alpha1.HostFirmwareSettingsStatus{
					FirmwareSchema: &metal3v1alpha1.SchemaReference{
						Name:      schemaName,
						Namespace: hostNamespace,
					},
					Settings: metal3v1alpha1.SettingsMap{
						"L2Cache":               "10x512 KB",
						"NetworkBootRetryCount": "10",
						"ProcVirtualization":    "Disabled",
					},
				},
			},
			ExpectedSchema: &metal3v1alpha1.FirmwareSchema{
				TypeMeta: metav1.TypeMeta{
					Kind:       "FirmwareSchema",
					APIVersion: "metal3.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:            schemaName,
					Namespace:       hostNamespace,
					ResourceVersion: "2",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "metal3.io/v1alpha1",
							Kind:       "HostFirmwareSettings",
							Name:       hostName,
						},
					},
				},
				Spec: metal3v1alpha1.FirmwareSchemaSpec{
					Schema: map[string]metal3v1alpha1.SettingSchema{
						"AssetTag": {
							AttributeType: "String",
							MinLength:     &minLength,
							MaxLength:     &maxLength,
							Unique:        &iTrue,
						},
						"CustomPostMessage": {
							AttributeType: "String",
							MinLength:     &minLength,
							MaxLength:     &maxLength,
							Unique:        &iFalse,
							ReadOnly:      &iFalse,
						},
						"L2Cache": {
							AttributeType:   "String",
							AllowableValues: []string(nil),
							MinLength:       &minLength,
							MaxLength:       &maxLength,
							ReadOnly:        &iTrue,
						},
						"NetworkBootRetryCount": {
							AttributeType:   "Integer",
							AllowableValues: []string(nil),
							LowerBound:      &lowerBound,
							UpperBound:      &upperBound,
							ReadOnly:        &iFalse,
						},
						"ProcVirtualization": {
							AttributeType:   "Enumeration",
							AllowableValues: []string{"Enabled", "Disabled"},
							ReadOnly:        &iFalse,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {

			ctx := context.TODO()
			prov := getMockProvisioner(tc.CurrentSettings, tc.CurrentSchema)
			host := &metal3v1alpha1.BareMetalHost{}
			host.ObjectMeta.Name = hostName
			host.ObjectMeta.Namespace = hostNamespace
			tc.ExpectedSettings.TypeMeta = metav1.TypeMeta{
				Kind:       "HostFirmwareSettings",
				APIVersion: "metal3.io/v1alpha1"}
			tc.ExpectedSettings.ObjectMeta = metav1.ObjectMeta{
				Name:            hostName,
				Namespace:       hostNamespace,
				ResourceVersion: "2"}
			if tc.CreateSchemaResource || len(tc.CurrentSchema) > 0 {
				tc.ExpectedSettings.Annotations = map[string]string{
					"firmwareSchemaName":      schemaName,
					"firmwareSchemaNamespace": hostNamespace,
				}

			}

			r := getTestHostReconciler(host)
			info := getDefaultHostReconcileInfo(host, hostName, hostNamespace)
			// Create the resources using fakeclient
			if tc.CurrentHFSResource != nil {
				tc.CurrentHFSResource.TypeMeta = metav1.TypeMeta{
					Kind:       "HostFirmwareSettings",
					APIVersion: "metal3.io/v1alpha1"}
				tc.CurrentHFSResource.ObjectMeta = metav1.ObjectMeta{
					Name:      hostName,
					Namespace: hostNamespace}

				r.Client.Create(ctx, tc.CurrentHFSResource)
				r.Client.Update(ctx, tc.CurrentHFSResource)
				tc.ExpectedSettings.ObjectMeta.ResourceVersion = "3"
			}
			if tc.CreateSchemaResource {
				createSchemaResource(ctx, r)
			}

			err := r.storeHostFirmwareSettings(prov, info)
			assert.Equal(t, nil, err)

			// Check that resources get created or updated
			key := client.ObjectKey{
				Namespace: host.ObjectMeta.Namespace, Name: host.ObjectMeta.Name}
			actualSettings := &metal3v1alpha1.HostFirmwareSettings{}
			err = r.Client.Get(ctx, key, actualSettings)
			assert.Equal(t, nil, err)
			assert.Equal(t, tc.ExpectedSettings, actualSettings)

			if tc.ExpectedSchema != nil {
				key = client.ObjectKey{
					Namespace: host.ObjectMeta.Namespace, Name: schemaName}
				actualSchema := &metal3v1alpha1.FirmwareSchema{}
				err = r.Client.Get(ctx, key, actualSchema)
				assert.Equal(t, nil, err)
				assert.Equal(t, tc.ExpectedSchema, actualSchema)
			}
		})
	}
}

func TestGetHostFirmwareSettings(t *testing.T) {

	testCases := []struct {
		Scenario string
		// the existing resources
		CurrentHFSResource   *metal3v1alpha1.HostFirmwareSettings
		CreateSchemaResource bool
		// the expected updated resource
		ExpectedStatusSettings metal3v1alpha1.SettingsMap
		ExpectedSpecSettings   metal3v1alpha1.DesiredSettingsMap
		ExpectedError          error
	}{
		{
			Scenario: "valid spec changes no schema",
			CurrentHFSResource: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"L2Cache":               intstr.FromString("10x512 KB"),
						"ProcVirtualization":    intstr.FromString("Disabled"),
						"NetworkBootRetryCount": intstr.FromString("20"),
					},
				},
			},
			CreateSchemaResource: false,
			ExpectedStatusSettings: metal3v1alpha1.SettingsMap{
				"CustomPostMessage":     "All tests passed",
				"L2Cache":               "10x256 KB",
				"NetworkBootRetryCount": "10",
				"ProcVirtualization":    "Enabled",
				"SecureBoot":            "Enabled",
				"AssetTag":              "X45672917",
			},
			ExpectedSpecSettings: metal3v1alpha1.DesiredSettingsMap{
				"L2Cache":               intstr.FromString("10x512 KB"),
				"ProcVirtualization":    intstr.FromString("Disabled"),
				"NetworkBootRetryCount": intstr.FromString("20"),
			},
			ExpectedError: nil,
		},
		{
			Scenario: "valid spec changes with schema",
			CurrentHFSResource: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"CustomPostMessage":     intstr.FromString("All tests passed"),
						"ProcVirtualization":    intstr.FromString("Disabled"),
						"NetworkBootRetryCount": intstr.FromString("20"),
					},
				},
			},
			CreateSchemaResource: true,
			ExpectedStatusSettings: metal3v1alpha1.SettingsMap{
				"CustomPostMessage":     "All tests passed",
				"L2Cache":               "10x256 KB",
				"NetworkBootRetryCount": "10",
				"ProcVirtualization":    "Enabled",
				"SecureBoot":            "Enabled",
				"AssetTag":              "X45672917",
			},
			ExpectedSpecSettings: metal3v1alpha1.DesiredSettingsMap{
				"CustomPostMessage":     intstr.FromString("All tests passed"),
				"ProcVirtualization":    intstr.FromString("Disabled"),
				"NetworkBootRetryCount": intstr.FromString("20"),
			},
			ExpectedError: nil,
		},
		{
			Scenario: "invalid string",
			CurrentHFSResource: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"CustomPostMessage":     intstr.FromString("A really long POST message"),
						"ProcVirtualization":    intstr.FromString("Disabled"),
						"NetworkBootRetryCount": intstr.FromString("20"),
					},
				},
			},
			CreateSchemaResource:   true,
			ExpectedStatusSettings: nil,
			ExpectedSpecSettings:   nil,
			ExpectedError:          InvalidHostFirmwareValueError(InvalidHostFirmwareValueError{name: "CustomPostMessage", value: "A really long POST message"}),
		},
		{
			Scenario: "invalid int",
			CurrentHFSResource: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"CustomPostMessage":     intstr.FromString("All tests passed"),
						"ProcVirtualization":    intstr.FromString("Disabled"),
						"NetworkBootRetryCount": intstr.FromString("2000"),
					},
				},
			},
			CreateSchemaResource:   true,
			ExpectedStatusSettings: nil,
			ExpectedSpecSettings:   nil,
			ExpectedError:          InvalidHostFirmwareValueError(InvalidHostFirmwareValueError{name: "NetworkBootRetryCount", value: "2000"}),
		},
		{
			Scenario: "invalid enum",
			CurrentHFSResource: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"CustomPostMessage":     intstr.FromString("All tests passed"),
						"ProcVirtualization":    intstr.FromString("Not enabled"),
						"NetworkBootRetryCount": intstr.FromString("20"),
					},
				},
			},
			CreateSchemaResource:   true,
			ExpectedStatusSettings: nil,
			ExpectedSpecSettings:   nil,
			ExpectedError:          InvalidHostFirmwareValueError(InvalidHostFirmwareValueError{name: "ProcVirtualization", value: "Not enabled"}),
		},
		{
			Scenario: "invalid name",
			CurrentHFSResource: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"SomeNewSetting": intstr.FromString("foo"),
					},
				},
			},
			CreateSchemaResource:   true,
			ExpectedStatusSettings: nil,
			ExpectedSpecSettings:   nil,
			ExpectedError:          InvalidHostFirmwareNameError(InvalidHostFirmwareNameError{name: "SomeNewSetting"}),
		},
		{
			Scenario: "invalid password in spec",
			CurrentHFSResource: &metal3v1alpha1.HostFirmwareSettings{
				Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
					Settings: metal3v1alpha1.DesiredSettingsMap{
						"CustomPostMessage":     intstr.FromString("All tests passed"),
						"ProcVirtualization":    intstr.FromString("Disabled"),
						"NetworkBootRetryCount": intstr.FromString("20"),
						"SysPassword":           intstr.FromString("Pa%$word"),
					},
				},
			},
			CreateSchemaResource:   true,
			ExpectedStatusSettings: nil,
			ExpectedSpecSettings:   nil,
			ExpectedError:          InvalidHostFirmwareNameError(InvalidHostFirmwareNameError{name: "SysPassword"}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Scenario, func(t *testing.T) {

			ctx := context.TODO()
			host := &metal3v1alpha1.BareMetalHost{}
			host.ObjectMeta.Name = hostName
			host.ObjectMeta.Namespace = hostNamespace

			// This test does not require data from the provisioner (no pivot is done)
			settings := metal3v1alpha1.SettingsMap{}
			schema := map[string]metal3v1alpha1.SettingSchema{}
			prov := getMockProvisioner(settings, schema)

			r := getTestHostReconciler(host)
			info := getDefaultHostReconcileInfo(host, hostName, hostNamespace)

			// Create the resources using fakeclient
			if tc.CurrentHFSResource != nil {

				createHFSResource(ctx, r, tc.CurrentHFSResource, tc.CreateSchemaResource)
			}
			if tc.CreateSchemaResource {
				createSchemaResource(ctx, r)
			}

			hfs, err := r.getHostFirmwareSettings(prov, info)
			assert.Equal(t, tc.ExpectedError, err)
			if tc.ExpectedStatusSettings != nil {
				assert.Equal(t, tc.ExpectedStatusSettings, hfs.Status.Settings)
				assert.Equal(t, tc.ExpectedSpecSettings, hfs.Spec.Settings)
			}
		})
	}
}

func TestGetHostFirmwareSettingsAfterPivot(t *testing.T) {

	// Clear the Status as would happen during a pivot and ensure settings are reinstated
	ctx := context.TODO()
	host := &metal3v1alpha1.BareMetalHost{}
	host.ObjectMeta.Name = hostName
	host.ObjectMeta.Namespace = hostNamespace

	settings := metal3v1alpha1.SettingsMap{
		"CustomPostMessage":     "All tests passed",
		"L2Cache":               "10x256 KB",
		"NetworkBootRetryCount": "10",
		"ProcVirtualization":    "Enabled",
		"SecureBoot":            "Enabled",
		"AssetTag":              "X45672917",
	}

	schema := map[string]metal3v1alpha1.SettingSchema{}
	prov := getMockProvisioner(settings, schema)
	r := getTestHostReconciler(host)
	info := getDefaultHostReconcileInfo(host, hostName, hostNamespace)

	// Create the resources using fakeclient
	hfs := &metal3v1alpha1.HostFirmwareSettings{
		Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
			Settings: metal3v1alpha1.DesiredSettingsMap{
				"L2Cache":               intstr.FromString("10x512 KB"),
				"ProcVirtualization":    intstr.FromString("Disabled"),
				"NetworkBootRetryCount": intstr.FromString("20"),
			},
		},
	}
	createHFSResource(ctx, r, hfs, false)
	createSchemaResource(ctx, r)
	origStatus := hfs.Status.DeepCopy()

	// clear settings
	hfs.Status.Settings = metal3v1alpha1.SettingsMap(nil)
	r.Client.Update(ctx, hfs)

	hfsAfter, err := r.getHostFirmwareSettings(prov, info)
	assert.Equal(t, nil, err)
	assert.Equal(t, origStatus.Settings, hfsAfter.Status.Settings)
}

func TestReconcilerAfterPivot(t *testing.T) {

	// Clear the Status as would happen during a pivot and ensure firmwareSchema is reinstated
	ctx := context.TODO()
	host := &metal3v1alpha1.BareMetalHost{}
	host.ObjectMeta.Name = hostName
	host.ObjectMeta.Namespace = hostNamespace

	// Create the resources using fakeclient
	hfs := &metal3v1alpha1.HostFirmwareSettings{
		Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
			Settings: metal3v1alpha1.DesiredSettingsMap{
				"L2Cache":               intstr.FromString("10x512 KB"),
				"ProcVirtualization":    intstr.FromString("Disabled"),
				"NetworkBootRetryCount": intstr.FromString("20"),
			},
		},
	}
	r := getTestHFSReconciler(hfs)

	hfs.TypeMeta = metav1.TypeMeta{
		Kind:       "HostFirmwareSettings",
		APIVersion: "metal3.io/v1alpha1"}
	hfs.ObjectMeta = metav1.ObjectMeta{
		Name:      hostName,
		Namespace: hostNamespace}
	hfs.Status.Settings = metal3v1alpha1.SettingsMap(nil)
	hfs.Status.FirmwareSchema = nil
	hfs.Annotations = make(map[string]string)
	hfs.Annotations[metal3v1alpha1.FirmwareSchemaNameAnnotation] = schemaName
	hfs.Annotations[metal3v1alpha1.FirmwareSchemaNamespaceAnnotation] = hostNamespace

	r.Client.Create(ctx, hfs)

	// Do the reconcile
	namespacedName := types.NamespacedName{
		Namespace: hostNamespace,
		Name:      hostName,
	}
	request := ctrl.Request{NamespacedName: namespacedName}
	_, err := r.Reconcile(ctx, request)
	assert.Equal(t, nil, err)

	// Check if FirmwareSchema was updated
	key := client.ObjectKey{Namespace: hostNamespace, Name: hostName}
	hfsAfter := &metal3v1alpha1.HostFirmwareSettings{}
	err = r.Client.Get(ctx, key, hfsAfter)
	assert.Equal(t, nil, err)
	expectedSchema := &metal3v1alpha1.SchemaReference{
		Name:      schemaName,
		Namespace: hostNamespace}
	assert.Equal(t, expectedSchema, hfsAfter.Status.FirmwareSchema)
}
