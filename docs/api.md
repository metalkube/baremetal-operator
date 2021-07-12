# API and Resource Definitions

## BareMetalHost

**Metal³** introduces the concept of **BareMetalHost** resource, which
defines a physical host and its properties. The **BareMetalHost** embeds
two well differentiated sections, the bare metal host specification
and its current status.

### BareMetalHost spec

The *BareMetalHost's* *spec* defines the desire state of the host. It contains
mainly, but not only, provisioning details.

#### bmc

The `bmc` fields contain the connection information for the BMC
(Baseboard Management Controller) on the host.

The sub-fields are

* *address* -- The URL for communicating with the BMC controller, based
  on the provider being used. See below for more details.
* *credentialsName* -- A reference to a *secret* containing the
  username and password for the BMC.
* *disableCertificateVerification* -- A boolean to skip certificate
    validation when true.

BMC URLs vary based on the type of BMC and the protocol used to
communicate with them.

* IPMI
  * `ipmi://<host>:<port>`, an unadorned `<host>:<port>` is also accepted
    and the port is optional, if using the default one (623).
  * The ipmi privilege level can be set from the default(`ADMINISTRATOR`)
    to `OPERATOR` with an option URL parameter `privilegelevel`.
    `ipmi://<host>:<port>?privilegelevel=OPERATOR`
* Dell iDRAC
  * `idrac://` (or `idrac+http://` to disable TLS).
  * `idrac-virtualmedia://` to use virtual media instead of PXE
    for attaching the provisioning image to the host.
  * `idrac-redfish://` may be used to manage iDRAC controller with the
    Redfish protocol over HTTPS. The URL must also contain a path to
    the Redfish API system endpoint.
    `idrac-redfish://myhost.example/redfish/v1/Systems/System.Embedded.1`
* Fujitsu iRMC
  * `irmc://<host>:<port>`, where `<port>` is optional if using the default.
* HUAWEI ibmc
  * `ibmc://<host>:<port>` (or `ibmc+http://<host>:<port>` to disable TLS)
* HPE iLO 4
  * `ilo4://<host>:<port>` for iLO 4 based systems and the port is optional,
    if using the default one (443).
* HPE iLO 5
  * `ilo5://<host>:<port>` for iLO 5 based systems and the port is optional,
    if using the default one (443).
* iLO 5 Redfish
  * `ilo5-redfish://` (or `ilo5-redfish+http://` to disable TLS), the hostname
    or IP address, and the path to the system ID are required,
    for example `ilo5-redfish://myhost.example/redfish/v1/Systems/MySystemExample`
* Redfish
  * `redfish://` (or `redfish+http://` to disable TLS)
  * `redfish-virtualmedia://` to use virtual media instead of PXE
    for attaching the provisioning image to the host.
  * The hostname or IP address, and the path to the system ID are
    required for all variants.  For example
    `redfish://myhost.example/redfish/v1/Systems/System.Embedded.1`
    or `redfish://myhost.example/redfish/v1/Systems/1`

#### online

A boolean indicating whether the host should be powered on (true) or
off (false). Changing this value will trigger a change in power state
on the physical host.

#### consumerRef

A reference to another resource that is using the host, it could be
empty if the host is not being currently used.  For example, a
*Machine* resource when the host is being used by the
[*machine-api*](https://github.com/kubernetes-sigs/cluster-api).

#### externallyProvisioned

A boolean indicating whether the host provisioning and deprovisioning
are managed externally. When set:

* Power status can still be managed using the `online` field.
* Hardware inventory will be monitored, but no provisioning or deprovisioning
  operations are performed on the host.

#### image

Holds details for the image to be deployed on a given host.

The sub-fields are

* *url* -- The URL of an image to deploy to the host.
* *checksum* -- The actual checksum or a URL to a file containing
  the checksum for the image at *image.url*.
* *checksumType* -- Checksum algorithms can be specified. Currently
  only `md5`, `sha256`, `sha512` are recognized. If nothing is specified
  `md5` is assumed.
* *format* -- This is the disk format of the image. It can be one of `raw`,
  `qcow2`, `vdi`, `vmdk`, `live-iso` or be left unset.
  Setting it to raw enables raw image streaming in Ironic agent for that image.
  Setting it to live-iso enables iso images to live boot without deploying
  to disk, in this case the checksum fields are ignored.

Even though the image sub-fields are required by Ironic,
when the host provisioning is managed externally via `externallyProvisioned: true`,
and power control isn't needed, the fields can be left empty.

#### userData

A reference to the Secret containing the cloudinit user data and its
namespace, so it can be attached to the host before it boots for
configuring different aspects of the OS (like networking, storage,
...).

#### networkData

A reference to the Secret containing the network configuration data
(e.g. network\_data.json) and its namespace, so it can be attached to
the host before it boots to set network up

#### description

A human-provided string to help identify the host.

#### hardwareProfile

**This field is deprecated. See rootDeviceHints instead.**

The name of the hardware profile to use. The following are the current
supported `hardwareProfile` settings and their corresponding root
devices.

| **hardwareProfile** | **Root Device** |
|---------------------|-----------------|
| `unknown`           | /dev/sda        |
| `libvirt`           | /dev/vda        |
| `dell`              | HCTL: 0:0:0:0   |
| `dell-raid`         | HCTL: 0:2:0:0   |
| `openstack`         | /dev/vdb        |

**NOTE:** These are subject to change.

#### raid

This field contains the information about the RAID configuration for bare
metal servers.

The sub-fields are:

* *hardwareRAIDVolumes* -- It contains the list of logical disks for hardware
  RAID. If rootDeviceHints isn't used, the first volume is the root volume.
  Furthermore, it defines the desired configuration of volume in hardware RAID.
  * *level* -- RAID level for the logical disk. The following levels are
    supported: `0`,`1`,`2`,`5`,`6`,`1+0`,`5+0`,`6+0`.
  * *name* -- Name of the volume. Should be unique within the server. If not
    specified, volume name will be auto-generated.
  * *numberOfPhysicalDisks* -- Integer, number of physical disks to use for the
    logical disk. Defaults to minimum number of disks required for the
    particular RAID level.
  * *rotational* -- If true, select only rotational disks, if false - only
    solid-state and NVMe. Any disk types are used by default.
  * *sizeGibibytes* -- Size (Integer) of the logical disk to be created in GiB.
    If unspecified or set to 0, the maximum capacity of disk will be used for
    logical disk.
* *softwareRAIDVolumes* -- It contains the list of logical disks for software
  RAID. If rootDeviceHints isn't used, the first volume is the root volume. If
  HardwareRAIDVolumes is set this item will be invalid. The number of created
  Software RAID devices must be 1 or 2. If there is only one Software RAID
  device, it has to be a RAID-1. If there are two, the first one has to be a
  RAID-1, while the RAID level for the second one can be 0, 1, or 1+0. As the
  first RAID device will be the deployment device, enforcing a RAID-1 reduces
  the risk of ending up with a non-booting node in case of a disk failure.
  Furthermore, SoftwareRAIDVolume defines the desired configuration of volume
  in software RAID.
  * *level* -- RAID level for the logical disk. The following levels are
    supported: `0`,`1`,`1+0`.
  * *physicalDisks* -- A list of device hints, the number of items should be
    greater than or equal to 2.
  * *sizeGibibytes* -- Size (Integer) of the logical disk to be created in
    GiB. If unspecified or set to 0, the maximum capacity of disk will be
    used for logical disk.

If you do not set the RAID field, we will keep the current RAID configuration.

You can set the `hardwareRAIDVolume` as an empty slice to clear the hardware
RAID configuration, for example:

```yaml
spec:
   raid:
     hardwareRAIDVolume: []
```

**NOTE:** Currently the 'raid' field is only supported by ilo5/idrac/irmc.

**NOTE:** Software RAID will always be deleted.

**NOTE:** If you got following error message:

1. raid settings are defined, but the node's driver %s does not support RAID.
2. node's driver %s does not support hardware RAID.
3. node's driver %s does not support software RAID.

You can solve it by:

1. Keep raid field is nil.
2. Keep hardwareRAIDVolumes field is nil.
3. Keep softwareRAIDVolumes field is nil.

If the error message you get isn't included the above, you may need to check
whether the BM has a RAID controller and keep the raid field blank.

#### firmware

This field contains the information about the BIOS configuration for bare
metal servers.

The sub-fields are:

* *simultaneousMultithreadingEnabled* -- Allows a single physical processor
  core to appear as several logical processors. This supports following
  options: true, false.
* *sriovEnabled* -- SR-IOV support enables a hypervisor to create virtual
  instances of a PCI-express device, potentially increasing performance.
  This supports following options: true, false.
* *virtualizationEnabled* -- Supports the virtualization of platform
  hardware. This supports following options: true, false.

**NOTE:** Currently the `firmware` field is only supported by ilo4/ilo5/irmc
/idrac.

#### rootDeviceHints

Guidance for how to choose the device to receive the image being
provisioned. The storage devices are examined in the order they are
discovered during inspection and the hint values are compared to the
inspected values. The first discovered device that matches is
used. Hints can be combined, and if multiple hints are provided then a
device must match all hints in order to be selected.

The sub-fields are

* *deviceName* -- A string containing a Linux device name like
  `/dev/vda`. The hint must match the actual value exactly.
* *hctl* -- A string containing a SCSI bus address like
  `0:0:0:0`. The hint must match the actual value exactly.
* *model* -- A string containing a vendor-specific device
  identifier. The hint can be a substring of the actual value.
* *vendor* -- A string containing the name of the vendor or
  manufacturer of the device. The hint can be a substring of the
  actual value.
* *serialNumber* -- A string contianing the device serial
  number. The hint must match the actual value exactly.
* *minSizeGigabytes* -- An integer representing the minimum size of the
  device in Gigabytes.
* *wwn* -- A string containing the unique storage identifier. The
  hint must match the actual value exactly.
* *wwnWithExtension* -- A string containing the unique storage
  identifier with the vendor extension appended. The hint must match
  the actual value exactly.
* *wwnVendorExtension* -- A string containing the unique vendor
  storage indentifier. The hint must match the actual value exactly.
* *rotational* -- A boolean indicating whether the device should be
  a rotating disk (`true`) or not (`false`).

#### automatedCleaningMode

An interface to enable/disable automated cleaning during provisioning
and deprovisioning. When set to `disabled`, automated cleaning will be
skipped, where `metadata`(default value) enables it.

### BareMetalHost status

Moving onto the next block, the *BareMetalHost's* *status* which represents
the host's current state. Including tested credentials, current hardware
details, etc.

#### goodCredentials

A reference to the secret and its namespace holding the last set of
BMC credentials the system was able to validate as working.

#### triedCredentials

A reference to the secret and its namespace holding the last set of
BMC credentials that were sent to the provisioning backend.

#### lastUpdated

The timestamp of the last time the status of the host was updated.

#### operationalStatus

The status of the server. Value is one of the following:

* *OK* -- Indicates all the details for the host are known and working,
  meaning the host is correctly configured and manageable.
* *discovered* -- Implies some of the host's details are either
  not working correctly or missing. For example, the BMC address is known
  but the login credentials are not.
* *error* -- Indicates the system found some sort of irrecuperable error.
  Refer to the *errorMessage* field in the status section for more details.

#### errorMessage

Details of the last error reported by the provisioning backend, if
any.

#### hardware

The details for hardware capabilities discovered on the host. These
are filled in by the provisioning agent when the host is registered.

The sub-fields are

* *nics* -- List of network interfaces for the host.
  * *name* -- A string identifying the network device,
    e.g. *nic-1*.
  * *mac* -- The MAC address of the NIC.
  * *ip* -- The IP address of the NIC, if one was assigned
    when the discovery agent ran.
  * *speedGbps* -- The speed of the device in Gbps.
  * *vlans* -- A list holding all the VLANs available for this NIC.
  * *vlanId* -- The untagged VLAN ID.
  * *pxe* -- Whether the NIC is able to boot using PXE.
* *storage* -- List of storage (disk, SSD, etc.) available to the host.
  * *name* -- A string identifying the storage device,
    e.g. *disk 1 (boot)*.
  * *rotational* -- Either true or false, indicates whether the disk
    is rotational.
  * *sizeBytes* -- Size of the storage device.
  * *serialNumber* -- The device's serial number.
* *cpu* -- Details of the CPU(s) in the system.
  * *arch* -- The architecture of the CPU.
  * *model* -- The model string.
  * *clockMegahertz* -- The speed in MHz of the CPU.
  * *flags* -- List of CPU flags, e.g. 'mmx','sse','sse2','vmx', ...
  * *count* -- Amount of these CPUs available in the system.
* *firmware* -- Contains BIOS information like for instance its *vendor*
  and *version*.
* *systemVendor* -- Contains information about the host's *manufacturer*,
  the *productName* and *serialNumber*.
* *ramMebibytes* -- The host's amount of memory in Mebibytes.

#### hardwareProfile (status)

**This field is deprecated. See rootDeviceHints instead.**

The name of the hardware profile that matches the hardware discovered
on the host based on the details saved to the *Hardware* section. If
the hardware does not match any known profile, the value `unknown`
will be set on this field and is used by default. In practice, this
only affects which device the OS image will be written to. The
following are the current supported `hardwareProfile` settings and
their corresponding root devices.

| **hardwareProfile** | **Root Device** |
|---------------------|-----------------|
| `unknown`           | /dev/sda        |
| `libvirt`           | /dev/vda        |
| `dell`              | HCTL: 0:0:0:0   |
| `dell-raid`         | HCTL: 0:2:0:0   |
| `openstack`         | /dev/vdb        |

**NOTE:** These are subject to change.

#### poweredOn

Boolean indicating whether the host is powered on.

See *online* on the *BareMetalHost's* *Spec*.

#### provisioning

Settings related to deploying an image to the host.

* *state* -- The current state of any ongoing provisioning operation.
  The following are the currently supported ones:
  * *\<empty string\>* -- There is no provisioning happening, at the moment.
  * *registering* -- The host's BMC details are being checked.
  * *match profile* -- The discovered hardware details on the host
    are being compared against known profiles.
  * *ready* -- The host is available to be consumed.
  * *provisioning* -- An image is being written to the host's disk(s).
  * *provisioned* -- An image has been completely written to the host's
    disk(s).
  * *externally provisioned* -- Metal³ does not manage the image on the host.
  * *deprovisioning* -- The image is being wiped from the host's disk(s).
  * *inspecting* -- The hardware details for the host are being collected
    by an agent.
* *id* -- The unique identifier for the service in the underlying
  provisioning tool.
* *image* -- The image most recently provisioned to the host.
* *raid* -- The list of hardware or software RAID volumes recently set.
* *firmware* -- The BIOS configuration for bare metal server.
* *rootDeviceHints* -- The root device selection instructions used
  for the most recent provisioning operation.

### BareMetalHost Example

The following is a complete example from a running cluster of a *BareMetalHost*
resource (in YAML), it includes its specification and status sections:

```yaml
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  creationTimestamp: "2019-09-20T06:33:35Z"
  finalizers:
  - baremetalhost.metal3.io
  generation: 2
  name: bmo-master-0
  namespace: bmo-project
  resourceVersion: "22642"
  selfLink: /apis/metal3.io/v1alpha1/namespaces/bmo-project/baremetalhosts/bmo-master-0
  uid: 92b2f77a-db70-11e9-9db1-525400764849
spec:
  bmc:
    address: ipmi://10.10.57.19
    credentialsName: bmo-master-0-bmc-secret
  bootMACAddress: 98:03:9b:61:80:48
  consumerRef:
    apiVersion: machine.openshift.io/v1beta1
    kind: Machine
    name: bmo-master-0
    namespace: bmo-project
  externallyProvisioned: true
  hardwareProfile: default
  image:
    checksum: http://172.16.1.100/images/myOSv1/myOS.qcow2.md5sum
    url: http://172.16.1.100/images/myOSv1/myOS.qcow2
  online: true
  raid:
    hardwareRAIDVolumes:
    - level: "1"
      sizeGibibytes: 200
      rotational: true
  firmware:
    virtualizationEnabled: true
  userData:
    name: bmo-master-user-data
    namespace: bmo-project
  networkData:
    name: bmo-master-network-data
    namespace: bmo-project
  metaData:
    name: bmo-master-meta-data
    namespace: bmo-project
status:
  errorMessage: ""
  goodCredentials:
    credentials:
      name: bmo-master-0-bmc-secret
      namespace: bmo-project
    credentialsVersion: "5562"
  hardware:
    cpu:
      arch: x86_64
      clockMegahertz: 2000
      count: 40
      flags: []
      model: Intel(R) Xeon(R) Gold 6138 CPU @ 2.00GHz
    firmware:
      bios:
        date: 12/17/2018
        vendor: Dell Inc.
        version: 1.6.13
    hostname: bmo-master-0.localdomain
    nics:
    - ip: 172.22.135.105
      mac: "00:00:00:00:00:00"
      model: unknown
      name: eno1
      pxe: true
      speedGbps: 25
      vlanId: 0
    ramMebibytes: 0
    storage: []
    systemVendor:
      manufacturer: Dell Inc.
      productName: PowerEdge r460
      serialNumber: ""
  hardwareProfile: ""
  lastUpdated: "2019-09-20T07:03:23Z"
  operationalStatus: OK
  poweredOn: true
  provisioning:
    ID: a4438010-3fc6-4c5c-b570-900bbe85da57
    image:
      checksum: ""
      url: ""
    state: externally provisioned
  triedCredentials:
    credentials:
      name: bmo-master-0-bmc-secret
      namespace: bmo-project
    credentialsVersion: "5562"
```

And here is the secret `bmo-master-0-bmc-secret` holding the host's
BMC credentials, base64 encoded:

```console
$echo -n 'admin' | base64

YWRtaW4=

$echo -n 'password' | base64

cGFzc3dvcmQ=
```

Copy the above base64 encoded username and password pair and
paste it into the yaml as mentioned below.

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: bmo-master-0-bmc-secret
type: Opaque
data:
  username: YWRtaW4=
  password: cGFzc3dvcmQ=
```

NOTE: After decoding the secret content, whitespace is stripped from
the beginning and end before the username and password values are
used.

## Triggering Provisioning

Several conditions must be met in order to initiate provisioning.

1. The host `spec.image.url` field must contain a URL for a valid
   image file that is visible from within the cluster and from the
   host receiving the image.
2. The host must have `online` set to `true` so that the operator will
   keep the host powered on.
3. The host must have all of the BMC details.

To initiate deprovisioning, clear the image URL from the host spec.

## Unmanaged Hosts

Hosts created without BMC details will be left in the `unmanaged`
state until the details are provided. Unmanaged hosts cannot be
provisioned and their power state is undefined.

## Pausing reconciliation

It is possible to pause the reconciliation of a BareMetalHost object by adding
an annotation `baremetalhost.metal3.io/paused`. **Metal³**  provider sets the
value of this annotation as `metal3.io/capm3` when the cluster to which the
**BareMetalHost** belongs, is paused and removes it when the cluster is
not paused. If you want to pause the reconciliation of **BareMetalHost** you can
put any value on this annotation **other than `metal3.io/capm3`**. Please make
sure that you remove the annotation  **only if the value of the annotation is
not `metal3.io/capm3`, but another value that you have provided**. Removing the
annotation will enable the reconciliation again.

## Detaching hosts

It is possible to prevent management of a BareMetalHost object by adding
an annotation `baremetalhost.metal3.io/detached`. This removes the host from
the provisioner, which prevents any management of the physical host (e.g
changing power state, or deprovisioning), but still allows the BMH status
to be updated unlike the `paused` anotation. While in this state the
OperationalStatus field will be `detached` but the provisioning state will
be unmodified.  This API only has any effect for BareMetalHost resources
that are in either `Provisioned` or `ExternallyProvisioned` state.

Please note only the existence of the annotation is important to treat the BMH
as detached and the value of the annotation is always ignored.
