# Cloud dev setup

This guide uses the NVIDIA NIC cloud to set up a single node DPF enviornment.

# 1. Create an NVIDIA NIC Cloud setup

DPF requires either a Bluefield 2 or 3 for deploying a test cluster. To get a setup with the correct configuration from the NVIDIA NIC Cloud go to [this link](http://linux-cloud.mellanox.com/order?branch=master&description=b2b%20x86-64%20bf2&fw_version=/auto/host_fw_release/fw-41686/fw-41686-rel-24_41_1000-build-001/dist&hca_protocol=ETH&image=linux/inbox-ubuntu24.04-x86_64&is_extendable=True&mlxconfig=SRIOV_EN=1%20NUM_OF_VFS=46&time=6&version=20240508.0) and order the setup. 

# 2. Deploy DPF Standalone

Once the setup has been created - there should be a confirmation email - ssh into either of the nodes using the default credentials.

Follow the DPF Standalone install guide: https://gitlab-master.nvidia.com/doca-platform-foundation/dpf-standalone

Once the installation has succeeded the DPF system is ready to use and test. Further information can be found at the DPF Standalone repo.