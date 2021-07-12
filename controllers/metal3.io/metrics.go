package controllers

import (
	"github.com/prometheus/client_golang/prometheus"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
)

const (
	labelHostNamespace = "namespace"
	labelHostName      = "host"
	labelErrorType     = "error_type"
	labelPowerOnOff    = "on_off"
	labelPrevState     = "prev_state"
	labelNewState      = "new_state"
	labelHostDataType  = "host_data_type"
)

var reconcileCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "metal3_reconcile_total",
	Help: "The number of times hosts have been reconciled",
}, []string{labelHostNamespace, labelHostName})
var reconcileErrorCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_reconcile_error_total",
	Help: "The number of times the operator has failed to reconcile a host",
})
var actionFailureCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "metal3_host_error_total",
	Help: "The number of times hosts have entered an error state",
}, []string{labelErrorType})

var powerChangeAttempts = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "metal3_operation_power_change_total",
	Help: "Number of times a host has been powered on or off",
}, []string{labelHostNamespace, labelHostName, labelPowerOnOff})

var credentialsMissing = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_credentials_missing_total",
	Help: "Number of times a host's credentials are found to be missing",
})
var credentialsInvalid = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_credentials_invalid_total",
	Help: "Number of times a host's credentials are found to be invalid",
})
var unhandledCredentialsError = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_credentials_unhandled_error_total",
	Help: "Number of times getting a host's credentials fails in an unexpected way",
})
var updatedCredentials = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_credentials_updated_total",
	Help: "Number of times a host's credentials change",
})
var waitingForPreprovImage = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_waiting_for_preprov_image_total",
	Help: "Number of times the preprovisioning image is required but not available",
})
var noManagementAccess = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_credentials_no_management_access_total",
	Help: "Number of times a host management interface is unavailable",
})
var hostConfigDataError = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "metal3_host_config_data_error_total",
	Help: "Number of times the operator has failed to retrieve host configuration data",
}, []string{labelHostDataType})
var delayedProvisioningHostCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "metal3_delayed__provisioning_total",
	Help: "The number of times hosts have been delayed while provisioning due a busy provisioner",
}, []string{labelHostNamespace, labelHostName})
var delayedDeprovisioningHostCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "metal3_delayed__deprovisioning_total",
	Help: "The number of times hosts have been delayed while deprovisioning due a busy provisioner",
}, []string{labelHostNamespace, labelHostName})

var slowOperationBuckets = []float64{30, 90, 180, 360, 720, 1440}

var stateTime = map[metal3v1alpha1.ProvisioningState]*prometheus.HistogramVec{
	metal3v1alpha1.StateRegistering: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "metal3_operation_register_duration_seconds",
		Help: "Length of time per registration per host",
	}, []string{labelHostNamespace, labelHostName}),
	metal3v1alpha1.StateInspecting: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "metal3_operation_inspect_duration_seconds",
		Help:    "Length of time per hardware inspection per host",
		Buckets: slowOperationBuckets,
	}, []string{labelHostNamespace, labelHostName}),
	metal3v1alpha1.StateProvisioning: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "metal3_operation_provision_duration_seconds",
		Help:    "Length of time per hardware provision operation per host",
		Buckets: slowOperationBuckets,
	}, []string{labelHostNamespace, labelHostName}),
	metal3v1alpha1.StateDeprovisioning: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "metal3_operation_deprovision_duration_seconds",
		Help:    "Length of time per hardware deprovision operation per host",
		Buckets: slowOperationBuckets,
	}, []string{labelHostNamespace, labelHostName}),
}

var stateChanges = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "metal3_provisioning_state_change_total",
	Help: "Number of times a state transition has occurred",
}, []string{labelPrevState, labelNewState})

var hostRegistrationRequired = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_host_registration_required_total",
	Help: "Number of times a host is found to be unregistered",
})

var hostUnmanaged = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_host_unmanaged_total",
	Help: "Number of times a host is found to be unmanaged",
})

var deleteWithoutDeprov = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "metal3_delete_without_deprovisioning_total",
	Help: "Number of times a host is deleted despite deprovisioning failing",
})

func init() {
	metrics.Registry.MustRegister(
		reconcileCounters,
		reconcileErrorCounter,
		actionFailureCounters,
		powerChangeAttempts,
		delayedProvisioningHostCounters,
		delayedDeprovisioningHostCounters)

	for _, collector := range stateTime {
		metrics.Registry.MustRegister(collector)
	}

	metrics.Registry.MustRegister(
		credentialsMissing,
		credentialsInvalid,
		unhandledCredentialsError,
		updatedCredentials,
		noManagementAccess,
		hostConfigDataError)

	metrics.Registry.MustRegister(
		stateChanges,
		hostRegistrationRequired,
		hostUnmanaged,
		deleteWithoutDeprov)
}

func hostMetricLabels(request ctrl.Request) prometheus.Labels {
	return prometheus.Labels{
		labelHostNamespace: request.Namespace,
		labelHostName:      request.Name,
	}
}

func stateChangeMetricLabels(prevState, newState metal3v1alpha1.ProvisioningState) prometheus.Labels {
	return prometheus.Labels{
		labelPrevState: string(prevState),
		labelNewState:  string(newState),
	}
}
