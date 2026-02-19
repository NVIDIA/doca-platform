/*
Copyright 2026 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cleanup

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"strings"

	"github.com/onsi/ginkgo/v2"
	ginkgoTypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ginkgoHookID identifies which lifecycle hook is being processed
// Includes both Ginkgo's built-in hooks
//
// Naming requirement: All hooks follow "before-*" or "after-*" pattern
// This allows generic filtering (e.g., skip-cleanup-on-failure applies to all "after-*" hooks)
type ginkgoHookID string

var GinkgoHook = struct {
	// Built-in Ginkgo hooks
	BeforeEach  ginkgoHookID
	AfterEach   ginkgoHookID
	BeforeSuite ginkgoHookID
	AfterSuite  ginkgoHookID
}{
	BeforeEach:  "before-each",
	AfterEach:   "after-each",
	BeforeSuite: "before-suite",
	AfterSuite:  "after-suite",
}

// MergeMaps combines multiple label maps (later maps override earlier ones)
func MergeMaps(mps ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, mp := range mps {
		maps.Copy(merged, mp)
	}
	return merged
}

// globalE2ETestCleanupLabel is the catch-all label applied to all e2e test resources
var globalE2ETestCleanupLabel = map[string]string{"dpf-operator-e2e-test-cleanup": "true"}

// cleanupScopesStruct defines the scope identifiers and helper methods for cleanup
type cleanupScopesStruct struct {
	ID    string
	It    string
	Suite string
}

// ScopeKey returns the ID-prefixed label key for a given scope name
func (c *cleanupScopesStruct) ScopeKey(scopeName string) string {
	return fmt.Sprintf("%s.%s", c.ID, scopeName)
}

// ScopeSelector returns a label selector map for cleanup of a given scope
func (c *cleanupScopesStruct) ScopeSelector(scopeName string) map[string]string {
	return map[string]string{c.ScopeKey(scopeName): "true"}
}

// cleanupScopes is the global instance for scope management
var cleanupScopes = &cleanupScopesStruct{
	ID:    "cleanup-scope",
	It:    "It",
	Suite: "Suite",
}

// CleanupLabels provides labels for marking Kubernetes resources for automatic cleanup
var CleanupLabels = struct {
	It    map[string]string
	Suite map[string]string
}{
	It:    MergeMaps(cleanupScopes.ScopeSelector(cleanupScopes.It), globalE2ETestCleanupLabel),
	Suite: MergeMaps(cleanupScopes.ScopeSelector(cleanupScopes.Suite), globalE2ETestCleanupLabel),
}

// CleanupFlags controls cleanup behavior at various test scopes
type CleanupFlags struct {
	// Master switches
	SkipCleanup          bool
	SkipCleanupOnFailure bool
	CleanupStale         bool // Clean all stale e2e resources in BeforeSuite

	// Built-in Ginkgo scopes
	SkipSuiteCleanup       bool // Convenience flag: skip both before and after
	SkipSuiteCleanupBefore bool
	SkipSuiteCleanupAfter  bool

	SkipItCleanup       bool // Convenience flag: skip both before and after
	SkipItCleanupBefore bool
	SkipItCleanupAfter  bool

	initialized bool
}

// NewCleanupFlagsFromCLI registers cleanup CLI flags and returns a new CleanupFlags instance
// Call in init() to register flags before Ginkgo parses them
// Note: calling twice will panic (flag package rejects duplicate registrations)
// Note: flag.Parse() is handled by Ginkgo during test initialisation
func NewCleanupFlagsFromCLI() *CleanupFlags {
	cf := &CleanupFlags{}

	flag.BoolVar(&cf.CleanupStale, "e2e.cleanup.stale", false, "Clean all stale e2e resources from previous runs in BeforeSuite (ignores skip flags)")

	flag.BoolVar(&cf.SkipCleanup, "e2e.skip-cleanup", false, "Skip all cleanup operations (master switch)")
	flag.BoolVar(&cf.SkipCleanupOnFailure, "e2e.skip-cleanup.on-failure", false, "Skip cleanup for parent scopes when test fails (preserves resources for debugging)")

	flag.BoolVar(&cf.SkipSuiteCleanup, "e2e.skip-cleanup.suite", false, "Skip all Suite-scoped cleanup (both before and after)")
	flag.BoolVar(&cf.SkipSuiteCleanupBefore, "e2e.skip-cleanup.suite-before", false, "Skip Suite-scoped cleanup before suite")
	flag.BoolVar(&cf.SkipSuiteCleanupAfter, "e2e.skip-cleanup.suite-after", false, "Skip Suite-scoped cleanup after suite")

	flag.BoolVar(&cf.SkipItCleanup, "e2e.skip-cleanup.it", false, "Skip all It-scoped cleanup (both before and after)")
	flag.BoolVar(&cf.SkipItCleanupBefore, "e2e.skip-cleanup.it-before", false, "Skip It-scoped cleanup before each It block")
	flag.BoolVar(&cf.SkipItCleanupAfter, "e2e.skip-cleanup.it-after", false, "Skip It-scoped cleanup after each It block")

	return cf
}

// Init applies convenience flags (e.g., SkipCleanup enables all skip flags) and sets the initialized flag
// Call in BeforeSuite after Ginkgo has parsed CLI flags
func (cf *CleanupFlags) Init() *CleanupFlags {
	if cf.initialized {
		return cf // Already initialized
	}

	// Apply convenience flags
	if cf.SkipSuiteCleanup {
		cf.SkipSuiteCleanupBefore = true
		cf.SkipSuiteCleanupAfter = true
	}
	if cf.SkipItCleanup {
		cf.SkipItCleanupBefore = true
		cf.SkipItCleanupAfter = true
	}

	// Apply master skip flag
	if cf.SkipCleanup {
		cf.SkipItCleanupBefore = true
		cf.SkipItCleanupAfter = true
		cf.SkipSuiteCleanupBefore = true
		cf.SkipSuiteCleanupAfter = true
	}

	cf.initialized = true
	return cf
}

// Tracker tracks test hierarchy and manages scope-based cleanup
// Supports Suite-level and It-level auto-cleanup
//
// Thread-safety: Each Ginkgo parallel process has its own Tracker instance (no shared state)
type Tracker struct {
	cleanupFlags      *CleanupFlags
	cleanupFunc       CleanupFunc
	ctx               context.Context
	client            client.Client
	resourcesToDelete []client.ObjectList

	// Test failure tracking (for skip-cleanup-on-failure)
	anyTestFailed bool
}

// CleanupFunc defines the signature for valid cleanup functions
type CleanupFunc func(ctx context.Context, c client.Client, selector labels.Selector, objectLists ...client.ObjectList) error

// NewTracker creates a new hierarchy tracker instance
// Panics if CleanupFlags are not properly initialized (must call Init() or use NewCleanupFlagsFromCLI())
func NewTracker(cleanupFunc CleanupFunc, flags *CleanupFlags, ctx context.Context, testClient client.Client, resourcesToDelete []client.ObjectList) *Tracker {
	if flags == nil || !flags.initialized {
		panic("CleanupFlags not initialized")
	}

	return &Tracker{
		cleanupFlags:      flags,
		cleanupFunc:       cleanupFunc,
		ctx:               ctx,
		client:            testClient,
		resourcesToDelete: resourcesToDelete,
	}
}

// HandleScopeLifecycle handles scope-based tracking and cleanup operations
// This is the main entry point called from ReportBeforeEach/ReportAfterEach/... hooks
func (t *Tracker) HandleScopeLifecycle(specReport *ginkgoTypes.SpecReport, hook ginkgoHookID) {
	// Clean stale resources if flag is set (bypasses all skip checks)
	// Must run before shouldSkip check to ensure it executes even if suite cleanup is skipped
	if hook == GinkgoHook.BeforeSuite && t.cleanupFlags.CleanupStale {
		ginkgo.By("Cleanup: Stale resources (all e2e resources from previous runs)...")
		t.executeCleanup(globalE2ETestCleanupLabel)
	}

	if t.shouldSkip(hook, specReport) {
		return
	}

	// Execute hook-specific logic
	switch hook {
	case GinkgoHook.BeforeSuite:
		t.executeCleanup(cleanupScopes.ScopeSelector(cleanupScopes.Suite))

	case GinkgoHook.AfterSuite:
		t.executeCleanup(cleanupScopes.ScopeSelector(cleanupScopes.Suite))

	case GinkgoHook.BeforeEach:
		t.executeCleanup(cleanupScopes.ScopeSelector(cleanupScopes.It))

	case GinkgoHook.AfterEach:
		t.executeCleanup(cleanupScopes.ScopeSelector(cleanupScopes.It))
	}
}

// WarnIfStaleResources checks for stale resources from previous test runs
// Only warns if resources found AND no cleanup flags are active
// Never actually cleans up - just warns user to clean manually
func (t *Tracker) WarnIfStaleResources() {
	// Use global label to catch stale resources from any scope
	genericE2ETestSelector := labels.SelectorFromSet(globalE2ETestCleanupLabel)

	// Check each resource type - if any resources exist, warn
	for _, resourceListType := range t.resourcesToDelete {
		// Use Limit: 1 - we only need to know if ANY resources exist, not fetch them all
		err := t.client.List(t.ctx, resourceListType, &client.ListOptions{
			LabelSelector: genericE2ETestSelector,
			Limit:         1,
		})
		if err != nil {
			ginkgo.By(fmt.Sprintf("Warning: Failed to check for stale %T: %v", resourceListType, err))
			continue
		}
		foundResources, _ := meta.ExtractList(resourceListType)
		if len(foundResources) > 0 {
			ginkgo.By(fmt.Sprintf("Found stale %T from previous test run", resourceListType))
		}
	}
}

// executeCleanup performs the actual cleanup operation (inline implementation)
func (t *Tracker) executeCleanup(scopeLabels map[string]string) {
	selector := labels.SelectorFromSet(scopeLabels)
	Expect(t.cleanupFunc(t.ctx, t.client, selector, t.resourcesToDelete...)).To(Succeed())
}

// shouldSkip handles the overall cleanup skip logic
// Returns true if cleanup should be skipped based on (in priority order):
// 1. Master skip flag (-e2e.skip-cleanup)
// 2. Test skipped/pending state (for BeforeEach/AfterEach)
// 3. Test failure + skip-cleanup-on-failure flag (for after-* hooks)
// 4. Hook-specific flags (e.g., -e2e.skip-cleanup.suite)
func (t *Tracker) shouldSkip(hook ginkgoHookID, spec *ginkgoTypes.SpecReport) bool {
	// 1. Master skip flag (highest priority)
	if t.cleanupFlags.SkipCleanup {
		return true
	}

	// 2. Skip processing for skipped/pending tests (BeforeEach/AfterEach only)
	if spec != nil && (hook == GinkgoHook.BeforeEach || hook == GinkgoHook.AfterEach) {
		if spec.State.Is(ginkgoTypes.SpecStateSkipped) || spec.State.Is(ginkgoTypes.SpecStatePending) {
			return true
		}
	}

	// 3. Check test failure (only for after-* hooks)
	// When any test fails with -e2e.skip-cleanup.on-failure:
	// - Automatically enabled FailFast stops subsequent tests from running
	// - We skip all after-* cleanup (AfterEach, AfterSuite)
	// - This preserves all resources for debugging
	if strings.HasPrefix(string(hook), "after-") && t.cleanupFlags.SkipCleanupOnFailure {
		// spec is nil for AfterSuite; for AfterEach we check spec.Failed() and track it
		if spec != nil && spec.Failed() {
			t.anyTestFailed = true
		}
		if t.anyTestFailed {
			return true
		}
	}

	// 4. Hook-specific flag checks
	switch hook {
	case GinkgoHook.BeforeSuite:
		return t.cleanupFlags.SkipSuiteCleanupBefore
	case GinkgoHook.AfterSuite:
		return t.cleanupFlags.SkipSuiteCleanupAfter
	case GinkgoHook.BeforeEach:
		return t.cleanupFlags.SkipItCleanupBefore
	case GinkgoHook.AfterEach:
		return t.cleanupFlags.SkipItCleanupAfter
	default:
		panic(fmt.Sprintf("unhandled hook: %s", hook))
	}
}
