---
title: "DPF E2E Test Cleanup Tracker System"
---

[TOC]

# DPF E2E Test Cleanup Tracker System

## Overview
The tracker system provides flexible, fine-grained cleanup control for DPF e2e tests, addressing limitations in resource lifecycle management across complex test scenarios.


## Label System
Resources are tracked using cleanup labels:
* **Suite-level**: `CleanupScope.Suite` - Cleaned up at end of test suite
* **It-level**: `CleanupScope.It` - Cleaned up after each test (`It` block)

`CleanupScope` is an alias to `core.CleanupLabels` available within the `e2e` package.


## CLI Flags
Extended skip cleanup control:

```bash
-e2e.skip-cleanup                    # Skip all cleanup operations (master switch)
-e2e.skip-cleanup.on-failure         # Keep resources on failure for debugging

-e2e.skip-cleanup.suite              # Skip all Suite-scoped cleanup (both before and after)
-e2e.skip-cleanup.suite-before       # Skip Suite-scoped cleanup before suite
-e2e.skip-cleanup.suite-after        # Skip Suite-scoped cleanup after suite

-e2e.skip-cleanup.it                 # Skip all It-scoped cleanup (both before and after)
-e2e.skip-cleanup.it-before          # Skip It-scoped cleanup before each It block
-e2e.skip-cleanup.it-after           # Skip It-scoped cleanup after each It block

-e2e.cleanup.stale                   # Force cleanup of stale labeled resources
```
