---
title: "End-To-End Testing"
---

[TOC]

DPF end-to-end tests are written in go and use the `ginkgo` and `gomega` frameworks.

The tests use a configuration file which is unique per test run.

### Prerequisites

All tests require:

* a working Kubernetes cluster
* DPF Operator controller manager deployed and running

Individual tests may also have their own prerequisites which should be defined in their CI definition. For example different tests may require different sets of container images and helm charts to run.

### Structure

The tests have the following structure:

* `Suite`: Reads configuration and sets up the system for a test run. There is one test configuration per `Suite`
* `Describe`: The core of a test - contains the full set of tests to be run. There is one DPFOperatorConfig per `Describe`. `Describe` nodes never run in parallel with each other
* `Context`: A set of tests. These are categorised and configured as a group representing a subsystem or a feature
* `It`: An individual test. Each `It` block is independent and leaves a clean state once finished. `It` blocks are able to run in parallel and continue on failure
* `By` is used to describe a step either inside or outside a test
