---
title: "DPF E2E Test Cleanup Tracker System"
---

[TOC]

# DPF E2E Test Cleanup Tracker System

## Overview
The tracker system provides flexible, fine-grained cleanup control for DPF e2e tests, addressing limitations in resource lifecycle management across complex test scenarios.


## Core Concepts
### Named Cleanup Scopes
The tracker supports named scopes with independent lifecycles. Though the Scopes concept adds a powerful tool to manage resources we must stay aware that we still are bound by Ginkgo's rules - specifically the two phase approach of tree building (basically everything outside of `It`, `Before*`/`After*`) and actual test execution. Keep that in mind when setting up your scope as this influences the lifetime of your resources and when functions/Scopes are called/constructed.

#### Manual Scope
* Explicit control via `CleanupBefore()` and `CleanupAfter()` calls (both optional)
* Can be called multiple times
* Potential use cases: Resources shared across multiple contexts (e.g., VPC prerequisites)

### Lifecycle Methodology
The tracker follows Ginkgo's Before/After hook methodology:
* `BeforeSuite` / `AfterSuite`
* `BeforeAll` / `AfterAll`
* `BeforeEach` / `AfterEach`
* Custom named scope hooks integrate seamlessly


## CLI Flags
Extended skip cleanup control:

```bash
-e2e.skip-cleanup{ /, .suite, .it, .named-scopes }{ /, -before, -after}
    e.g.
    -e2e.skip-cleanup.suite
    -e2e.skip-cleanup.suite-before
    -e2e.skip-cleanup.suite-after
-e2e.skip-cleanup.on-failure      # Keep resources on failure for debugging
-e2e.cleanup.stale                # Force cleanup of stale labeled resources
```

### Named Scope Syntax
* e.g. `-e2e.skip-cleanup.named-scopes="some_scope"`
    * Fully supports Ginkgo label syntax (Regex, Ginkgo-styled boolean expressions)
* Allows precise targeting of cleanup scopes


## Helper Functions
### Scope Inspection
* `someScope.ResourcesExist()` - Check if scope has any resources
* `someScope.CountResources()` - Count resources in scope
* `someScope.HasAlternatives(scopeB, ...)` - Check if any of the provided alternative scopes have resources, automatically excluding the receiver scope from the check. Useful for conditional cleanup decisions, e.g. "skip creating resources if an alternative scope already provides them".


## Label System
Resources are tracked using cleanup labels:
* **Suite-level**: `CleanupScope.Suite` - Cleaned up at end of test suite
* **It-level**: `CleanupScope.It` - Cleaned up after each test (`It` block)
* **Scope-level**: `scope.CleanupLabels` - Cleaned up when scope emits cleanup

`CleanupScope` is an alias to `cleanup.CleanupLabels` available within the `e2e` package.


## Setup Skeleton
Blueprint for integrating the tracker into a Ginkgo test suite.
It covers flag registration, tracker creation, and hooking into the test lifecycle so scope transitions and cleanup are handled automatically.

```go
// init runs before Ginkgo parses arguments
// Register all other CLI flags here so they are available when Ginkgo starts
func init() {
	testing.Init() // Initialize Go test flags (required for Go 1.24+)
	cleanupFlags = cleanup.NewCleanupFlagsFromCLI()
}

// Finalise flag parsing (Init), create a configured tracker instance
// Emit the lifecycle event with cleanup.GinkgoHook.BeforeSuite
var _ = BeforeSuite(func() {
	cleanupFlags.Init()
	cleanupTracker = cleanup.NewTracker(myCleanupFunc, cleanupFlags, ctx, testClient, resourcesToDelete)
	cleanupTracker.HandleScopeLifecycle(nil, cleanup.GinkgoHook.BeforeSuite)
})

// Emit the lifecycle event with cleanup.GinkgoHook.Before/AfterEach and forward the SpecReport
var _ = ReportBeforeEach(func(spec SpecReport) {
	cleanupTracker.HandleScopeLifecycle(&spec, cleanup.GinkgoHook.BeforeEach)
})
var _ = ReportAfterEach(func(spec SpecReport) {
	cleanupTracker.HandleScopeLifecycle(&spec, cleanup.GinkgoHook.AfterEach)
})

// Emits the lifecycle event with cleanup.GinkgoHook.AfterSuite
var _ = AfterSuite(func() {.GinkgoHook.AfterSuite)
	cleanupTracker.HandleScopeLifecycle(nil, cleanup.GinkgoHook.AfterSuite)
})
```


## Examples
Below are illustrative examples demonstrating how Named Scopes can be used. These are simplified for clarity and intended for demonstration purposes only.

Sleeps are added so one can better follow each step. Some debug prints were added to make all actions visible.

### Helper functions used in the examples
```go
var (
	resourceFQNAsStr = func(namespace, name string) string {
		return fmt.Sprintf("%s::ConfigMap(%s)", namespace, name)
	}
	configMapExists = func(namespace, name string) bool {
		return testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &corev1.ConfigMap{}) == nil
	}
	namespaceExists = func(namespace string) bool {
		return testClient.Get(ctx, client.ObjectKey{Name: namespace}, &corev1.Namespace{}) == nil
	}
	checkState = func(namespace string, names ...string) {
		for _, name := range names {
			if configMapExists(namespace, name) {
				By(fmt.Sprintf("****Hello %s", resourceFQNAsStr(namespace, name)))
			} else {
				By(fmt.Sprintf("%s not found", resourceFQNAsStr(namespace, name)))
			}
		}
	}
	createConfigMap = func(namespace, name string, labels map[string]string) {
		Expect(testClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		})).To(Succeed())
		Expect(configMapExists(namespace, name)).To(BeTrue())
		checkState(namespace, name)
	}
)
```

### Manual mode - simple
```go
// E2E_TEST_ARGS='-v -ginkgo.v -ginkgo.focus="" -ginkgo.label-filter="manual-mode-simple" -e2e.skip-cleanup.suite-after=true -e2e.config=./config.yaml' make test-e2e
var _ = Describe("manual-mode-simple", Label("manual-mode-simple"), Ordered, func() {
	const testNS = "test-ns1"
	var scope *cleanup.Scope
	const sleepTime time.Duration = time.Second * 4

	BeforeAll(func() {
		By(">>BeforeAll")
		time.Sleep(sleepTime)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   testNS,
				// Namespace cleaned up automatically when suite completes
				Labels: CleanupScope.Suite,
			},
		}

		if !namespaceExists(testNS) {
			Expect(testClient.Create(ctx, ns)).To(Succeed())
		}

		// Register a manual scope - cleanup only happens when we explicitly call EmitCleanup*
		scope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("scope"))
		By(fmt.Sprintf("Pre-cleanup for scope: %s", scope.Name))
		// Clean up any leftover resources from previous runs before starting fresh
		scope.CleanupBefore()
		By("<<BeforeAll")
	})

	AfterAll(func() {
		time.Sleep(sleepTime)
		By(">>AfterAll")
		By(fmt.Sprintf("Post-cleanup for scope: %s", scope.Name))
		// Clean up all resources created with scope.CleanupLabels during this test run
		scope.CleanupAfter()
		By("<<AfterAll")
	})

	Context("Context 1", func() {
		fmt.Println("Context level (start)")
		It("It1", func() {
			// This ConfigMap will be cleaned up when the suite completes (AfterSuite)
			createConfigMap(testNS, "configmap1-suite", CleanupScope.Suite)
			// This ConfigMap will be cleaned up when we call scope.CleanupAfter()
			createConfigMap(testNS, "configmap1-scope", scope.CleanupLabels)
			time.Sleep(sleepTime)
		})
		It("It2", func() {
			time.Sleep(sleepTime)
			// This ConfigMap will be cleaned up automatically after this It block (AfterEach)
			createConfigMap(testNS, "configmap2-it", CleanupScope.It)
			time.Sleep(time.Second * 4)
		})
		fmt.Println("Context level (middle)")
		It("It3", func() { time.Sleep(sleepTime) })
		It("It4", func() { time.Sleep(sleepTime) })
		fmt.Println("Context level (end)")
	})
})
```

### Manual mode - advanced
```go
// E2E_TEST_ARGS='-v -ginkgo.v -ginkgo.focus="" -ginkgo.label-filter="manual-mode-advanced" -e2e.cleanup.stale -e2e.skip-cleanup.suite=true -e2e.config=./config.yaml' make test-e2e
var _ = Describe("manual-mode-advanced", Label("manual-mode-advanced"), Ordered, func() {
	const testNS = "test-ns1"
	var scope *cleanup.Scope
	const sleepTime time.Duration = time.Second * 4

	BeforeAll(func() {
		By(">>BeforeAll")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   testNS,
				// Namespace cleaned up automatically when suite completes
				Labels: CleanupScope.Suite,
			},
		}
		if !namespaceExists(testNS) {
			Expect(testClient.Create(ctx, ns)).To(Succeed())
		}

		// Register scope but don't emit cleanup yet - we'll control it manually within tests
		scope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("scope"))
		By("<<BeforeAll")
	})

	AfterAll(func() {
		time.Sleep(sleepTime)
		By(">>AfterAll")
		// Note: No CleanupAfter() here - cleanup was handled explicitly in tests
		By("<<AfterAll")
	})

	Context("Context 1", func() {
		fmt.Println("Context level (start)")
		It("It1", func() {
			By(fmt.Sprintf(">>>>Pre-cleanup for scope: %s", scope.Name))
			// Clean up any stale resources before creating new ones for this test phase
			scope.CleanupBefore()
			// This ConfigMap will persist until we explicitly call scope.CleanupAfter()
			createConfigMap(testNS, "configmap1-scope", scope.CleanupLabels)
			By(fmt.Sprintf(">>>>Pre-cleanup again for scope: %s", scope.Name))
			time.Sleep(sleepTime)
		})
		It("It2", func() {
			// Add more resources to the same scope - they accumulate until cleanup
			createConfigMap(testNS, "configmap2-scope", scope.CleanupLabels)
			// This one uses It-level cleanup - gone after this test regardless of scope
			createConfigMap(testNS, "configmap2-it", CleanupScope.It)
			time.Sleep(time.Second * 4)
			By(fmt.Sprintf("<<<<Post-cleanup for scope: %s", scope.Name))
			// Explicitly clean up all scope resources now (configmap1-scope + configmap2-scope)
			scope.CleanupAfter()
		})
		fmt.Println("Context level (middle)")
		It("It3", func() {
			time.Sleep(sleepTime)
			By(fmt.Sprintf("<<<<Post-cleanup for scope: %s", scope.Name))
			// Safe to call even when scope is empty - it's a no-op
			scope.CleanupAfter()
			// This ConfigMap uses It-level cleanup - cleaned up after this test
			createConfigMap(testNS, "configmap3-it", CleanupScope.It)
		})
		It("It4", func() { time.Sleep(sleepTime) })
		fmt.Println("Context level (end)")
	})
})
```


