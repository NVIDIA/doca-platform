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

package airgap

import (
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/utils/ptr"
)

const (
	// AptSourcesFile is the apt sources file of the apt mirror the DPUFlavor writes.
	AptSourcesFile = "/etc/apt/airgap-mirror.list"
	// QuietUnit is the systemd unit that stops the DPU OS's own internet traffic.
	QuietUnit = "dpf-airgap-quiet.service"
)

// quietUnit masks the units of the DPU OS that reach the internet on their own (snapd, Ubuntu Pro,
// MOTD news, the apt daily jobs, NTP, the firmware metadata refresh), which an air-gapped e2e run
// must not count as leaks. The DPUFlavor can enable a unit but not mask one. The '-' prefix ignores
// a unit the OS lacks.
const quietUnit = `[Unit]
Description=DPF air-gap e2e: mask the Ubuntu units that reach the internet

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=-/usr/bin/systemctl mask --now snapd.service snapd.socket snapd.seeded.service snapd.refresh.timer snapd.snap-repair.timer
ExecStart=-/usr/bin/systemctl mask --now apt-news.service esm-cache.service ua-timer.timer ua-reboot-cmds.service
ExecStart=-/usr/bin/systemctl mask --now motd-news.service motd-news.timer
ExecStart=-/usr/bin/systemctl mask --now apt-daily.timer apt-daily-upgrade.timer apt-daily.service apt-daily-upgrade.service
ExecStart=-/usr/bin/systemctl mask --now systemd-timesyncd.service
ExecStart=-/usr/bin/systemctl mask --now fwupd-refresh.timer fwupd-refresh.service
ExecStart=-/usr/bin/systemctl try-restart systemd-resolved.service

[Install]
WantedBy=multi-user.target
`

// resolvedConf drops the global upstream DNS servers of systemd-resolved: the DPU reaches the
// mirror by IP address.
const resolvedConf = `[Resolve]
DNS=
FallbackDNS=
DNSStubListener=yes
`

// The DPU OS reaches the internet in the first minutes of each OS install, before the quiet unit
// runs, from the two sources below. cloud-init writes the config files of the DPUFlavor before the
// network is up, so these files stop both from the first boot on.
const (
	// pollinateSeeded marks the entropy seed of pollinate as fetched, so pollinate.service skips
	// its fetch from entropy.ubuntu.com, which it otherwise makes at every boot. pollinate checks
	// only that the file exists.
	pollinateSeeded = "/var/cache/pollinate/seeded"
	// esmAptHook is the apt hook of Ubuntu Pro, which starts esm-cache.service and
	// apt-news.service at every apt update. The DPUFlavor replaces it with a comment.
	esmAptHook = "/etc/apt/apt.conf.d/20apt-esm-hook.conf"
)

// ApplyToDPUFlavor configures the DPU of flavor for the mirror, as the air-gap user guide does:
//   - the containerd sandbox image is the mirror copy of the one of the BOM;
//   - containerd pulls from the mirror host over plain HTTP (a hosts.toml for the mirror host
//     only, no upstream registry is redirected);
//   - the packages of the flavor install from the apt mirror, when one is configured;
//   - the DPU OS's own internet traffic is stopped, so the egress block of the e2e run drops
//     nothing.
func (m *Mirror) ApplyToDPUFlavor(flavor *provisioningv1.DPUFlavor) {
	m.ApplyToDPUFlavorSpec(&flavor.Spec)
}

// ApplyToDPUFlavorSpec is ApplyToDPUFlavor on a DPUFlavor spec.
func (m *Mirror) ApplyToDPUFlavorSpec(spec *provisioningv1.DPUFlavorSpec) {
	spec.ContainerdConfig.SandboxImage = m.sandbox
	hostsToml := fmt.Sprintf("server = \"http://%[1]s\"\n\n[host.\"http://%[1]s\"]\n  capabilities = [\"pull\", \"resolve\"]\n  skip_verify = true\n", m.host)
	spec.ConfigFiles = append(spec.ConfigFiles,
		configFile("/etc/containerd/certs.d/"+m.host+"/hosts.toml", hostsToml),
		configFile("/etc/systemd/system/"+QuietUnit, quietUnit),
		configFile("/etc/default/motd-news", "ENABLED=0\n"),
		configFile("/etc/systemd/resolved.conf.d/airgap.conf", resolvedConf),
		configFile(pollinateSeeded, "seeded by the DPF air-gap e2e\n"),
		configFile(esmAptHook, "// The DPF air-gap e2e removes the Ubuntu Pro apt hooks.\n"),
	)
	spec.SystemdServices = append(spec.SystemdServices, provisioningv1.SystemdServiceSpec{
		Name:      QuietUnit,
		Operation: provisioningv1.SystemdServiceEnableAndStart,
	})
	if m.cfg.AptMirror != "" && len(spec.Packages) > 0 {
		spec.ConfigFiles = append(spec.ConfigFiles,
			configFile(AptSourcesFile, fmt.Sprintf("deb [trusted=yes] http://%s/ ./\n", m.cfg.AptMirror)))
		for i := range spec.Packages {
			spec.Packages[i].RepoFileRef = AptSourcesFile
		}
	}
}

// configFile returns a DPUFlavor config file that writes content to path with mode 0644,
// overwriting any file at path.
func configFile(path, content string) provisioningv1.ConfigFile {
	return provisioningv1.ConfigFile{
		Path:        path,
		Operation:   provisioningv1.FileOverride,
		Raw:         ptr.To(content),
		Permissions: "0644",
	}
}
