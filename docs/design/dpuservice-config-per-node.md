# Generating per-node configuration for DPU Services (Implementation Guidelines)

## Overview
Some DPU Services may require configurations that are **node-specific** (uses unique parameter values per each DPU in the setup). An example for such a DPU Service is the HBN DPU Service.
This document proposes a standard approach for implementing per-node configuration.

The proposed mechanism for creating node-specific configuration is as follows:

1. The DPU Service helm chart should include templates for **creating config maps objects**. There should be one config map for the set of per-node parameters we would want to use as part of the service configuration (JSON dictionary), and one or more config maps for each target confiugration file we wish to generate (containing a jinja template for generating the configuration file)
2. The DPU Service pod should include **an init-container** that has all the config maps and target configuration folders mounted. 
3. The init-container start script should include the relevant commands for **extracting the configuration parameters** for the specific node on which it runs and for **generating the target configuration files** using jinja
4. The values for for the above config maps will be included in the DPU Service object yaml, they shall be placed under "spec -> values"

## A detailed example

1. The helm chart of the DPU Service should create **config maps** for both the per-node parameters JSON and for each jinja template required for generating the target configuration files. This is done by adding config maps templates.
   
    Here's an example for the **per-node config parameters** config map:
    
    ```
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: per-node-conf
    data:
      per_node_config.json: {{ .Values.perNodeConf.perNodeConfigJson | toYaml | indent 1 }}
    ```

    Here's an example for the above **jinja template** config map:
    
    ```
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: j2-config-template
      labels:
    data:
      startup-config.yaml.j2: {{ .Values.j2ConfigTemplate.ConfigYamlJ2 | toYaml | indent 1 }}
    ```

2. The DPU Service pod should include an **init-container** that has mountpoints to the generated config maps and to the target folders to which the final configuration should be injected. The init-container should also receive **the hostname of the node on which it runs** as an environment variable.

    Here's an example of the init-container yaml:
    ```
    volumes:
        - name: config-target-volume
          hostPath:
            path: /var/lib/etc/service-config
            type: DirectoryOrCreate
        - name: config-volume
          configMap:
            name: per-nodes-conf
        - name: j2-template-volume
          configMap:
            name: j2-config-template
    initContainers:
      - name: service-init
        image: harbor.mellanox.com/cloud-orchestration-dev/service-x:latest
        securityContext:
          privileged: true
        imagePullPolicy: Always
        volumeMounts:
        - name: config-target-volume
          mountPath: /etc/service-config
        - name: config-volume
          mountPath: /tmp/config-data
        - name: j2-template-volume
          mountPath: /tmp/j2-template
        env:
        - name: HOSTNAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
    ```

3. The **start script** of the init container should run something similar to this for extracting the config parameters specific to the node on which the pod is running and for generating the target configuration file using the jinja template.

    Example code:

    ```
    local config_dir="/tmp/config-data/"
    local template_dir="/tmp/j2-template/"
    local target_dir="/etc/service-config/"
    local hostname="${HOSTNAME}"

    echo "Extract the local node configuration parameters from the multi-node configuration file"
    local host_config=$(jq -r --arg hostname "${hostname}" '.[$hostname]' "${config_dir}/per_node_config.json")

    if [ "$host_config" != "null" ]; then
        echo "Render startup configuration with local node parameters from Jinja2 template"
        python3 -c "from jinja2 import Environment, FileSystemLoader; \
            env = Environment(loader=FileSystemLoader('${template_dir}'), autoescape=True); \
            template = env.get_template('startup-config.yaml.j2'); \
            print(template.render(config=${host_config}))" > "${target_dir}/startup-config.yaml"
        echo "Save generated target configuration file for the local node"
    else
        echo "No configuration parameters found for local node: ${hostname}"
    fi
    ```
    
  4. The DPU Service yaml should include (under "values") **the set of per-node parameters** for the service (JSON formatted).

      Here's an example:

      ```
      perNodeConf:
        perNodeConfigJson: |-
          {
            "host-1": {
              "sample-param-1": "11.0.0.111/32",
              "sample-param-2": "10.0.121.3/29",
              "sample-param-3": "65111",
            },
            "host-2": {
              "sample-param-1": "11.0.0.112/32",
              "sample-param-2": "10.0.121.4/29",
              "sample-param-3": "65112",
            },
            "host-3": {
              "sample-param-1": "11.0.0.113/32",
              "sample-param-2": "10.0.121.5/29",
              "sample-param-3": "65113",
            },
            "host-4": {
              "sample-param-1": "11.0.0.114/32",
              "sample-param-2": "10.0.121.6/29",
              "sample-param-3": "65114",
            },
          }
      ```
   
5. For each **target configuration file** that needs to be created on the node (DPU), the DPU Service yaml should include (under "values") an attribute containing **a jinja template** that refers to the relevant parameters.

    Here's an example:    

    ```
    j2ConfigTemplate:
      ConfigYamlJ2: |-
        - service-x-config:
            interface:
              lo:
                ip:
                  address:
                    {{ config.sample-param-1 }}: {}
                type: loopback
            interface:
              lo:
                ip:
                  address:
                    {{ config.sample-param-2 }}: {}
                type: loopback
            router:
              bgp:
                autonomous-system: {{ config.sample-param-3 }}
                enable: on
    ```   
