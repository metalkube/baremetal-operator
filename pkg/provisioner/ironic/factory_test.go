package ironic

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

type EnvFixture struct {
	ironicEndpoint    string
	inspectorEndpoint string
	kernelURL         string
	ramdiskURL        string
	isoURL            string

	origEnv map[string]string
}

func (f *EnvFixture) TearDown() {
	for e, v := range f.origEnv {
		if v == "" {
			os.Unsetenv(e)
		} else {
			os.Setenv(e, v)
		}
	}
}

func (f *EnvFixture) replace(env, value string) {
	f.origEnv[env] = os.Getenv(env)
	if value == "" {
		os.Unsetenv(env)
	} else {
		os.Setenv(env, value)
	}
}

func (f *EnvFixture) SetUp() {
	f.origEnv = map[string]string{}
	f.replace("IRONIC_ENDPOINT", f.ironicEndpoint)
	f.replace("IRONIC_INSPECTOR_ENDPOINT", f.inspectorEndpoint)
	f.replace("DEPLOY_KERNEL_URL", f.kernelURL)
	f.replace("DEPLOY_RAMDISK_URL", f.ramdiskURL)
	f.replace("DEPLOY_ISO_URL", f.isoURL)
}

func (f EnvFixture) VerifyConfig(t *testing.T, c ironicConfig) {
	assert.Equal(t, f.kernelURL, c.deployKernelURL)
	assert.Equal(t, f.ramdiskURL, c.deployRamdiskURL)
	assert.Equal(t, f.isoURL, c.deployISOURL)
}

func (f EnvFixture) VerifyEndpoints(t *testing.T, ironic, inspector string) {
	assert.Equal(t, f.ironicEndpoint, ironic)
	assert.Equal(t, f.inspectorEndpoint, inspector)
}

func TestLoadConfigFromEnv(t *testing.T) {
	cases := []struct {
		name                  string
		env                   EnvFixture
		expectedError         string
		expectedImgBuildError string
	}{
		{
			name: "kernel and ramdisk",
			env: EnvFixture{
				kernelURL:  "http://kernel",
				ramdiskURL: "http://ramdisk",
			},
		},
		{
			name: "ISO only",
			env: EnvFixture{
				isoURL: "http://iso",
			},
		},
		{
			name: "ISO and kernel/ramdisk",
			env: EnvFixture{
				isoURL:     "http://iso",
				kernelURL:  "http://kernel",
				ramdiskURL: "http://ramdisk",
			},
		},
		{
			name:          "no deploy info",
			env:           EnvFixture{},
			expectedError: "Either DEPLOY_KERNEL_URL and DEPLOY_RAMDISK_URL or DEPLOY_ISO_URL must be set",
		},
		{
			name: "only kernel",
			env: EnvFixture{
				kernelURL: "http://kernel",
			},
			expectedError: "Either DEPLOY_KERNEL_URL and DEPLOY_RAMDISK_URL or DEPLOY_ISO_URL must be set",
		},
		{
			name: "only ramdisk",
			env: EnvFixture{
				ramdiskURL: "http://ramdisk",
			},
			expectedError:         "Either DEPLOY_KERNEL_URL and DEPLOY_RAMDISK_URL or DEPLOY_ISO_URL must be set",
			expectedImgBuildError: "DEPLOY_RAMDISK_URL requires DEPLOY_KERNEL_URL to be set also",
		},
		{
			name: "ISO and kernel",
			env: EnvFixture{
				kernelURL: "http://kernel",
				isoURL:    "http://iso",
			},
			expectedError: "DEPLOY_KERNEL_URL and DEPLOY_RAMDISK_URL can only be set together",
		},
		{
			name: "ISO and ramdisk",
			env: EnvFixture{
				ramdiskURL: "http://ramdisk",
				isoURL:     "http://iso",
			},
			expectedError:         "DEPLOY_KERNEL_URL and DEPLOY_RAMDISK_URL can only be set together",
			expectedImgBuildError: "DEPLOY_RAMDISK_URL requires DEPLOY_KERNEL_URL to be set also",
		},
	}

	for _, tt := range []string{"", " (with img builder)"} {
		for _, tc := range cases {
			t.Run(tc.name+tt, func(t *testing.T) {
				defer tc.env.TearDown()
				tc.env.SetUp()
				imgBuild := tt == ""
				config, err := loadConfigFromEnv(imgBuild)
				expectedError := tc.expectedError
				if imgBuild {
					expectedError = tc.expectedImgBuildError
				}
				if expectedError != "" {
					assert.Regexp(t, expectedError, err)
				} else {
					assert.Nil(t, err)
					tc.env.VerifyConfig(t, config)
				}
			})
		}
	}
}

func TestLoadEndpointsFromEnv(t *testing.T) {
	cases := []struct {
		name        string
		env         EnvFixture
		expectError bool
	}{
		{
			name: "both",
			env: EnvFixture{
				ironicEndpoint:    "http://ironic.test",
				inspectorEndpoint: "http://ironic-inspector.test",
			},
		}, {
			name: "ironic-only",
			env: EnvFixture{
				ironicEndpoint: "http://ironic.test",
			},
			expectError: true,
		}, {
			name: "inspector-only",
			env: EnvFixture{
				inspectorEndpoint: "http://ironic-inspector.test",
			},
			expectError: true,
		}, {
			name:        "neither",
			env:         EnvFixture{},
			expectError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.env.TearDown()
			tc.env.SetUp()
			i, ii, err := loadEndpointsFromEnv()
			if tc.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				tc.env.VerifyEndpoints(t, i, ii)
			}
		})
	}
}
