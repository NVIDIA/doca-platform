---
title: "E2E Testing quick start guide"
---

[TOC]

# E2E Testing quick start guide
General information about the E2E testing framework structure, patterns, and best practices.

* **Client**: controller-runtime client + kubernetes clientset


## File Organization
### Core files
* `test/e2e/e2e_suite_test.go` - entry point, suite lifecycle (`BeforeSuite`/`AfterSuite`)
* `test/e2e/config.go` - configuration structure and parsing
* `test/e2e/utils.go` - shared utilities, domain labels, cleanup scopes

### Infrastructure setup
* `test/e2e/system_setup.go` - DPF system deployment, node setup, cluster provisioning
* `test/e2e/system_test.go` - system-level test configuration (SetInput, SystemSetupBeforeSuite)

### Test suites
* `test/e2e/*_test.go`

> For high-level testing structure overview (Suite, Describe, Context, It), see [End-to-End Testing Architecture](../../../docs/public/developer-guides/system/testing/end-to-end-testing.md).


## Configuration
Configures test environment setup: which DPF infrastructure/application resources to deploy, how many DPU nodes to provision, and test behavior settings.

### Usage
Pass config file via `-e2e.config` flag:
```bash
E2E_TEST_ARGS="-e2e.config=./config-provisioning-multinode.yaml" ... make test-e2e
```

### Config files
Location: `test/e2e/config-*.yaml`

Each config targets different test scenarios:
* **`config-quick.yaml`** - fast tests without provisioning (`numberOfDPUNodes: 0`, fake BFB)
* **`config-provisioning.yaml`** - single-node provisioning tests with real DPU, external reboot
* **`config-provisioning-multinode.yaml`** - multi-node (2 nodes) provisioning, external reboot
* **`config-provisioning-physical.yaml`** - physical hardware setup
* **`config-provisioning-zt-physical.yaml`** - Zero-Trust physical setup; sets `nodeRebootConfigMap`/`nodeRebootConfigMapPath` for in-cluster Redfish reboots
* **`config-provisioning-upgrade-v26.04.yaml` / `config-provisioning-upgrade-current.yaml`** - upgrade scenario tests (see [upgrade testing workflow](../../../docs/do_not_publish/tests/upgrade.md))
* **`config-scale.yaml`** - scale tests (10 nodes, mock DPUServices for performance, see [scale testing methodology](../../../docs/public/developer-guides/system/testing/scale-testing.md))

Example structure:
```yaml
bfb: "../objects/infrastructure/bfb.yaml"                                               # BFB manifest
dpuCluster: "../objects/infrastructure/dpucluster.yaml"                                 # DPUCluster spec
dpuService: "../objects/application/dpuservice.yaml"                                    # DPUService template
numberOfDPUNodes: 2                                                                     # node count
# Reboot driver: pick ONE of the two paths below (never both).
# cloud env:
useExternalNodeReboot: true                                                             # labels host nodes with reboot-method=external (cloud only)
# Zero-Trust (in-cluster Job drives Redfish/BMC):
nodeRebootConfigMap: "dpunode-reboot-redfish"                                           # custom reboot script ConfigMap name (ZeroTrust only)
nodeRebootConfigMapPath: "../objects/infrastructure/dpunode-reboot-redfish.yaml"        # custom reboot script ConfigMap path (ZeroTrust only)
```

The reboot driver is split into two independent paths; a given config picks
at most one:

* **`useExternalNodeReboot: true`** (cloud) — labels every
  host-cluster Node with `provisioning.dpu.nvidia.com/reboot-method=external`
  so a systemd unit can reboot the node.
* **`nodeRebootConfigMap` + `nodeRebootConfigMapPath`** (Zero-Trust only) —
  the suite applies the named ConfigMap (with a Job pod-template that drives
  Redfish/BMC) and patches each DPUNode to
  `spec.nodeRebootMethod.script.name=<configmap>`. The DPUNode controller
  spawns the Job per reboot. Requires the `E2E_ZT_BMC_PASSWORD` and
  `E2E_ZT_BMC_INVENTORY_PATH` env vars.

Configs reference YAML manifests in `test/objects/`

### Flow
1. Config YAML specifies paths to manifest files in `test/objects/`
2. Framework loads these manifests into K8s objects
3. Tests use these objects to create/verify resources

### Related files
* `test/e2e/config.go` - config struct definition
* `test/e2e/system_setup.go` - `systemTestInput` (holds loaded objects), `applyConfig()` (loads manifests)

### CI workflows
For automated test execution workflows, see:
* [CI e2e workflow](../../../docs/do_not_publish/tests/e2e.md) - standard e2e test execution
* [CI e2e-mock-dms workflow](../../../docs/do_not_publish/tests/e2e-mock-dms.md) - scale testing with mock DMS



## Test Labels
Categorize and filter tests via Ginkgo labels. Defined in `Domain` struct (`test/e2e/utils.go`).

Labels serve different purposes:
* **Test categorization** - group tests by feature area (SDN, SNAP, VPC, provisioning, upgrade, scale)
* **Prerequisites** - indicate test requirements (provisioned nodes, L2 connectivity)
* **Behavior triggers** - automatically invoke domain-specific setup in `BeforeSuite` (e.g., `Domain.SDN` triggers `SDNBeforeSuite()`)

Usage:
```go
// Apply label to test
Label(Domain.SDN)

// Command-line filtering
--label-filter="SDN"                                // run SDN tests (triggers SDNBeforeSuite)
--label-filter="!SDN"                               // exclude SDN (skips SDNBeforeSuite)
--label-filter="(SDN || SNAP) && RequiresNodes"     // complex filter
```

**Note:** there is some associated logic implemented to some labels (e.g. multiple labels could trigger the same suite, ...).


## Cleanup Scopes
Automatic resource cleanup via labels. Defined in `test/utils/utils.go`:

### Default Ginkgo Scopes
* `utils.AfterEachCleanupLabels`
    * Per-test resources
* `utils.AfterAllCleanupLabels`
    * Global, long living resources (mostly infrastructure)

Usage:
```go
// Directly with ObjectMeta
dpuService := &dpuservicev1.DPUService{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "my-service",
        Namespace: "dpu-system",
        Labels:    utils.AfterEachCleanupLabels,
    },
}

// or

obj.SetLabels(utils.AfterAllCleanupLabels)
```


## Resource Collection
Artifacts collected on test failures and at suite teardown.

### Per failed test
Path: `artifacts/failed_tests/<test-name>/`
* Pod logs from all namespaces
* Resource YAMLs (dump of all DPF CRs)
* Kubernetes events
* Node status

### Pre-DPF operator config cleanup
Path: `artifacts/pre-dpf-operator-config-cleanup/`
* Cluster state captured after all tests but before DPFOperatorConfig deletion

### Post-DPF operator config cleanup
Path: `artifacts/post-dpf-operator-config-cleanup/`
* Cluster state captured after DPFOperatorConfig deletion (final state)


## Troubleshooting
For common test failures, error messages, and solutions, see:
* [Common Issues & Solutions](../../../docs/do_not_publish/tests/common-issues.md) - troubleshooting guide for e2e test failures


## Anatomy of an e2e test
Main test function: Exported, called from `*_test.go` (e.g. `system_test.go`):

Test-specific guidance (with general engineering best practices worth noting). The following shows annotated examples of common test patterns and highlights best practices.
```go
// sometest_test.go
var _ = Describe("DPF <some> tests ...", Labels{Domain.DPFSystem}, func() {

	BeforeEach(func() {
        // If required: Check if we have DPU nodes
		if !input.hasDpuNodes() {
			return
		}
    })
    
    // Note the casing:
    // * All strings in Context, Describe, By, Skip, ... start with a majuscule
    // * Only It starts with a minuscule
	Context("Validate my fancy feature", Labels{dpfSystemLabel, requiresNodesLabel}, func() {
		It("create a pod consuming a DPUServiceNAD with all dependencies and check that it is created successfully", func() {
			ValidateMyFeature(ctx, input)
		})
	})
})
```

```go
// myfeature.go
func ValidateMyFeature(ctx context.Context, input *systemTestInput) {
    // If required: Check if we have DPU nodes
    if !input.hasDpuNodes() {
        Skip("Skip test as there are not multiple nodes")
    }
    
    //////////////////////////////////////////////
    // Constants and variables:
    // * Use constants wherever possible; avoid global variables
    // * Prefer maintainable, reusable code without overengineering
    // * Define generic variables/constants at the beginning of a scope
    // * Declare temporary/local variables as late as possible within the smallest scope
    const (
        serviceName       = "myservice"
        dpuServiceNADName = "mynad"
        mtu               = 1500
        defaultTimeout    = 10 * time.Second
    )

    //////////////////////////////////////////////
    // Namespaces
    
    var testNS *corev1.Namespace
    testNS = &corev1.Namespace{
        ObjectMeta: metav1.ObjectMeta{
            // Kubernetes generates unique non-deterministic name like "my-test-ns-abc123"
            GenerateName: "my-test-ns-",
            // To ease your workflow while testing/debugging you could temporarily overwrite this by specifing the `Name` field
            // `Name` overwrites `GenerateName` if specified
            // Name:         "my-test-ns", // TODO: Remove this after testing/debugging
            Labels:       utils.AfterAllCleanupLabels, // Define appropriate cleanup label
        },
    }
    Expect(input.client.Create(ctx, testNS)).To(Succeed())
    By("Created test namespace: " + testNS.Name)

    //////////////////////////////////////////////
    // Image pull secrets

    By("Copy image pull secret to namespace " + testNS.Name)
    // Re-use generic existing helper functions if possible
    CopySecretToNamespace(ctx, input.client, dpfPullSecretName, dpfOperatorSystemNamespace, testNS.Name, utils.AfterEachCleanupLabels)

    //////////////////////////////////////////////
    // Object creation and validation
    By("Create DPUServiceNAD")
    // Easy to read as factory method abstracts away construction details and programm flow and business logic is more in focus
    // Separation of concerns (object construction separated from creation)
    dpuServiceNAD := constructDPUServiceNAD(dpuServiceNADName, testNS.Name, mtu)
    Expect(input.client.Create(ctx, dpuServiceNAD)).To(Succeed())

    // ...

    By("Verify DPUServiceNAD is ready")
    // Most of our objects have a defined status field structure and can be validated easily using helpers
    EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceNAD, defaultTimeout)

    By("Verify DPUService pods are created in DPU cluster")
    // Check with Eventually for async operations
    // Pattern: Check list existence (not individual pod properties)
    // Eventually polls/retries the function repeatedly until success or timeout
    // All assertions evaluated together - if any fail, the entire function retries
    Eventually(func(g Gomega) {
        const podServiceLabel string = "svc.dpu.nvidia.com/service"
        podList := &corev1.PodList{}
        // Use `dpuClusterClient` for DPU cluster operations, `input.client` for host cluster
        g.Expect(dpuClusterClient.List(ctx, podList,
            client.InNamespace(testNS.Name),
            client.MatchingLabels{podServiceLabel: serviceName},
        )).To(Succeed())
        // Only checking if pods exist (not checking individual pod states)
        g.Expect(podList.Items).ToNot(BeEmpty(), "No Pods found in DPU cluster containing label: "+podServiceLabel)
    }).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
    
    // Alternative pattern: Validate each object individually
    // Use when you need to check properties on every pod/object in the list
    Eventually(func(g Gomega) {
        podList := &corev1.PodList{}
        g.Expect(dpuClusterClient.List(ctx, podList, ...)).To(Succeed())
        g.Expect(podList.Items).ToNot(BeEmpty())
        // Loop through and check each pod's status
        for _, pod := range podList.Items {
            g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
        }
    }).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
}

// Private functions to build objects
// Export only what's needed
func constructDPUServiceNAD(name, namespace string, mtu int) *dpuservicev1.DPUServiceNAD {
    return &dpuservicev1.DPUServiceNAD{
        ObjectMeta: metav1.ObjectMeta{
            Name:      name,
            Namespace: namespace,
            Labels:    utils.AfterEachCleanupLabels,  // automatic cleanup
        },
        Spec: dpuservicev1.DPUServiceNADSpec{
            ResourceType: "sf",
            Bridge:       "br-sfc",
            ServiceMTU:   mtu,
        },
    }
}

// Using image pull secrets during object construction
func constructDummyDPUServiceObject(serviceName, namespace, interfaceName string) *dpuservicev1.DPUService {
    dpuServiceDummy := &dpuservicev1.DPUService{
        ObjectMeta: metav1.ObjectMeta{
            Name:      serviceName,
            Namespace: namespace,
            // Always set cleanup labels appropriately
            Labels:    utils.AfterEachCleanupLabels,
        },
    }

    By("Set HelmChart; tag: " + tag + ", repo: " + helmRegistry)
    dpuServiceDummy.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
        // Chart names may vary depending on service (e.g. vpcOvnChartName, ovnChartName)
        Chart:   serviceName + "-chart",
        // Values set via env vars in test/e2e/e2e_suite_test.go::getEnvVariables() :
        Version: tag,
        RepoURL: helmRegistry,
    }

    if ngcAPIKey != "" {
        dpuServiceDummy.Spec.HelmChart.Values = &machineryruntime.RawExtension{
            Raw: []byte(fmt.Sprintf(
                // Make sure you adapt this to your desired secret structure
                // E.g. for OVNK:
                // helmChart:
                //   values:
                //     global:
                //       imagePullSecretName: dpf-pull-secret
                `{"imagePullSecrets": [{"name": "%s"}]}`, dpfPullSecretName,
            )),
        }
    }

    // Dot-access oftentimes cleaner than accessing defining it within deep structures
    dpuServiceDummy.Spec.ServiceID = ptr.To(serviceName)
    dpuServiceDummy.Spec.Interfaces = []string{interfaceName}
    return dpuServiceDummy
}
```


## Anti-Patterns
* Don't create resources without cleanup labels


## Best practices
* `dpuService := generateDPUObj("test", namespace, dpuServiceTemplate.DeepCopy())`
    * Use generator helper functions if available
    * Copy configuration templates before modifying
* When you use polling (`Eventually`/`Consistently`) always set `.WithPolling(1 * time.Second)`
    * A larger interval only adds idle wait time after the condition is already met
    * Polling every 1 s is basically for free
    * Only deviate from 1s if you have a really good reason
