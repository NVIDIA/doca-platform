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

package bmcdump

import (
	"fmt"
	"net/http"

	redfishclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
)

// bmcGeneration holds everything that differs between BlueField generations.
// Supporting a new generation means adding an entry here, and a BMC that renames
// a Redfish resource will fail with the expected path named in the error.
type bmcGeneration struct {
	name        string
	username    string
	managerDump string
	// systemDump is empty on generations that expose no CPU diagnostics dump.
	systemDump string
}

var (
	bf3Generation = bmcGeneration{
		name:        "BlueField-3",
		username:    redfishclient.BF3BMCUser,
		managerDump: "/redfish/v1/Managers/Bluefield_BMC" + dumpLogServicePath,
		// BF3's System Dump offers only root-of-trust and firmware-attribute
		// types. None is an analog of CPU diagnostics, and the CPU-side
		// evidence is in the Manager dump instead, so there is nothing to collect.
		systemDump: "",
	}

	bf4Generation = bmcGeneration{
		name:        "BlueField-4",
		username:    redfishclient.BF4BMCUser,
		managerDump: "/redfish/v1/Managers/BlueField_BMC_0" + dumpLogServicePath,
		systemDump:  "/redfish/v1/Systems/BlueField_0" + dumpLogServicePath,
	}
)

// dumpUnit is one CollectDiagnosticData job: a Dump log service plus the body it
// accepts. A BMC yields at most two, one under Managers and one under Systems.
type dumpUnit struct {
	// name is the artifact subdirectory, managerUnitName or systemUnitName.
	name        string
	servicePath string
	requestBody string
}

func (u dumpUnit) entriesPath() string {
	return u.servicePath + "/Entries"
}

func (u dumpUnit) collectTarget() string {
	return u.servicePath + collectDiagnosticDataPath
}

func (u dumpUnit) clearTarget() string {
	return u.servicePath + clearLogPath
}

// discovery is what the collector learns about a BMC before it starts
// collecting. It is recorded in metadata.txt so a CI artifact explains itself.
type discovery struct {
	product  string
	username string
	units    []dumpUnit
}

// discover identifies the BMC generation and derives the credentials and dump
// services from it. The only request it makes is to the root service, which both
// generations serve unauthenticated; asking before authenticating is what lets us
// pick the right username without probing, since a wrong guess costs a 600s
// account lockout.
func (c *collector) discover() (*discovery, error) {
	product, err := c.rootServiceProduct()
	if err != nil {
		return nil, err
	}
	generation := generationFor(product)
	username := c.resolveUsername(product)
	c.client.SetBasicAuth(username, c.target.Password)

	c.note("BMC product: %s (treated as %s)", product, generation.name)
	c.note("Redfish user: %s", username)

	d := &discovery{
		product:  product,
		username: username,
		units: []dumpUnit{{
			name:        managerUnitName,
			servicePath: generation.managerDump,
			requestBody: managerDumpRequestBody,
		}},
	}
	if generation.systemDump == "" {
		c.note("Skipped system dump: %s exposes no %s", generation.name, cpuDiagnosticsType)
	} else {
		d.units = append(d.units, dumpUnit{
			name:        systemUnitName,
			servicePath: generation.systemDump,
			requestBody: systemDumpRequestBody,
		})
	}

	for _, unit := range d.units {
		c.note("Collecting %s dump from %s with %s", unit.name, unit.servicePath, unit.requestBody)
	}
	return d, nil
}

func (c *collector) rootServiceProduct() (string, error) {
	response, err := c.requestJSON(http.MethodGet, rootServicePath, nil)
	if err != nil {
		return "", fmt.Errorf("reading redfish root service: %w", err)
	}
	return redfishString(response["Product"]), nil
}

func (c *collector) resolveUsername(product string) string {
	if c.opts.Username != "" {
		return c.opts.Username
	}
	return generationFor(product).username
}

// generationFor defaults to BF3, matching how the provisioning client treats an
// unrecognized product.
func generationFor(product string) bmcGeneration {
	root := redfishclient.RootServiceInfo{Product: product}
	if root.IsBF4() {
		return bf4Generation
	}
	return bf3Generation
}
