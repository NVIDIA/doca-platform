# Copyright (C) 2024 Nvidia Corporation, Soule BA <souleb@nvidia.com>

# This file is part of the sos project: https://github.com/sosreport/sos
#
# This copyrighted material is made available to anyone wishing to use,
# modify, copy, or redistribute it subject to the terms and conditions of
# version 2 of the GNU General Public License.
#
# See the LICENSE file in the source distribution for further information.

from fnmatch import translate
import re
import json
import os
from sos.report.plugins import (Plugin, RedHatPlugin, DebianPlugin,
                                UbuntuPlugin, PluginOpt)

KUBE_PACKAGES = (
    'kubelet',
    'kubernetes',
)

KUBE_SVCS = (
    'kubelet',
    'kube-apiserver',
    'kube-proxy',
    'kube-scheduler',
    'kube-controller-manager',
)

KUBECONFIGS = (
    '/etc/kubernetes/admin.conf',
)

class DocaDpf(Plugin):
    """
    This plugin will capture information related to the DOCA Platform Framework
    resources and configurations in the system.
    """

    short_desc = 'DOCA Platform Framework resources and configurations'

    plugin_name = "doca-dpf"
    profiles = ('doca',)
    plugin_timeout = 900

    # DOCA Platform Framework related configuration files to collect
    config_files = []
    resources = [
        'bfb'
        'dpfoperatorconfig',
        'dpu',
        'dpudeployment',
        'dpuflavor',
        'dpuservice',
        'dpuservicechain',
        'dpuserviceconfiguration',
        'dpuservicecredentialrequest',
        'dpuserviceinterface',
        'dpuserviceipam',
        'dpuservicetemplate',
        'dpuset',
        'servicechain',
        'servicechainset',
        'serviceinterface',
        'serviceinterfaceset',
        # nv-ipam custom resources
        'cidrpool',
        'ippool',
        # argocd custom resources
        'application',
        'appproject',
        # kamaji custom resources
        'tenantcontrolplane',
    ]

    option_list = [
        PluginOpt('all', default=False,
                  desc='collect all namespace output separately'),
        PluginOpt('describe', default=False,
                  desc='collect describe output of all resources')
    ]

    kube_cmd = "kubectl"

    def set_kubeconfig(self):
        if os.environ.get('KUBECONFIG'):
            return
        for _kconf in self.files:
            if self.path_exists(_kconf):
                self.kube_cmd += f" --kubeconfig={_kconf}"
                break

    def check_is_master(self):
        """ Check if this is the master node """
        return any(self.path_exists(f) for f in self.files)

    def setup(self):
        # Copy the specified configuration files
        self.add_copy_spec(self.config_files)

        # We can only grab kubectl output from the master
        if not self.check_is_master():
            return

        self.collect_per_resource_details()

    def collect_per_resource_details(self):
        """ Collect details about each resource in all namespaces """
        # get all namespaces in use
        kns = self.collect_cmd_output(f'{self.kube_cmd} get namespaces',
                                      subdir='cluster-info')
        # namespace is the 1st word on line, until the line has spaces only
        kn_output = kns['output'].splitlines()[1:]
        knsps = [n.split()[0] for n in kn_output if n and len(n.split())]

        for nspace in knsps:
            knsp = f'--namespace={nspace}'
            if self.get_option('all'):
                k_cmd = f'{self.kube_cmd} get -o json {knsp}'

                for res in self.resources:
                    self.add_cmd_output(
                        f'{k_cmd} {res}',
                        subdir=f'cluster-info/{nspace}'
                    )

            if self.get_option('describe'):
                # need to drop json formatting for this
                k_cmd = f'{self.kube_cmd} {knsp}'
                for res in self.resources:
                    ret = self.exec_cmd(f'{k_cmd} get {res}')
                    if ret['status'] == 0:
                        k_list = [k.split()[0] for k in
                                  ret['output'].splitlines()[1:]]
                        for item in k_list:
                            k_cmd = f'{self.kube_cmd} {knsp}'
                            self.add_cmd_output(
                                f'{k_cmd} describe {res} {item}',
                                subdir=f'cluster-info/{nspace}/{res}'
                            )


class RedHatKubernetes(DocaDpf, RedHatPlugin):

    packages = KUBE_PACKAGES

    files = KUBECONFIGS

    services = KUBE_SVCS

    def check_enabled(self):
        # do not run at the same time as the openshift plugin
        if self.is_installed("openshift-hyperkube"):
            return False
        return super().check_enabled()

    def setup(self):
        self.set_kubeconfig()
        super().setup()


class UbuntuKubernetes(DocaDpf, UbuntuPlugin, DebianPlugin):

    packages = KUBE_PACKAGES

    files = KUBECONFIGS + (
        '/root/cdk/cdk_addons_kubectl_config',
        '/var/snap/microk8s/current/credentials/client.config',
    )

    services = KUBE_SVCS + (
        'snap.kubelet.daemon',
        'snap.kube-apiserver.daemon',
        'snap.kube-proxy.daemon',
        'snap.kube-scheduler.daemon',
        'snap.kube-controller-manager.daemon',
        # CDK
        'cdk.master.auth-webhook',
    )

    def setup(self):
        self.set_kubeconfig()

        if self.is_installed('microk8s'):
            self.kube_cmd = 'microk8s kubectl'

        self.config_files.extend([
            '/root/cdk/kubelet/config.yaml',
            '/root/cdk/audit/audit-policy.yaml',
        ])
        super().setup()


# vim: et ts=5 sw=4
