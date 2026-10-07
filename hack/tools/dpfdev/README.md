# DPF Development CLI

A command-line tool for DPF development operations.

## Installation

```bash
make install-dpfdev
```

## Configuration

`dpfdev` accepts a config file with the form:

```json
{
    "gitlab": {
        "endpoint": "https://gitlab-master.XXXX.com/api/v4", // NVIDIA GitLab endpoint
        "projectID": "XXX", // GitLab project ID for the repo
        "jobHistoryFile": "" // Path to the file containing job history.
    }
}
```
By default it will expect this config file to be at `~/.config/dfpdev.json`. The location of the config file can be set with the `--config` flag.

## Commands

### Job Commands

#### Analyze Job History

`dpfdev` can retrieve job information from the GitLab API. This information can be used to understand job success rates, durations and to point users to failed job logs.

On start DPF will download job information from the GitLab API. This step can take some time - on subsequent runs only new job data will be downloaded as long as this file remains in place. By default the tool will download 7 days of job history - but this can be customised with the `--start` flag.


```bash
dpfdev jobs analyze --config $PATH_TO_CONFIG.
```

For more details and options, use the help command:

```bash
dpfdev jobs analyze --help
```

### Config Command

The `config` command guides you through creating or updating the dpfdev configuration file.

```bash
dpfdev config
```

This interactive command will:
1. Prompt for the configuration file path (defaults to `$HOME/.config/dpfdev.json`)
2. Load existing configuration if available
3. Guide you through setting up GitLab configuration:
   - GitLab API endpoint
   - Project ID
   - Job history file location

You can also specify a custom configuration file path using the global `--config` flag:

```bash
dpfdev config --config /path/to/custom/config.json
```

For more details, use the help command:

```bash
dpfdev config --help
```

### Runner Commands

Commands for inspecting GitLab CI runners and the hosts backing E2E environments.

#### DPU Health

Check BlueField DPU PF interface health on physical and NIC Cloud CI runner workers.

```bash
# Check physical hosts and NIC Cloud runners
dpfdev runner dpu-health

# Check physical CI hosts only
dpfdev runner dpu-health physical

# Check NIC Cloud runners discovered from GitLab (cloud is an alias for nic-cloud)
dpfdev runner dpu-health cloud

# Check one explicit NIC Cloud runner without GitLab discovery
dpfdev runner dpu-health cloud --no-gitlab --nic-cloud-host 10.10.20.30

# Show per-host probe progress before the summary
dpfdev runner dpu-health --verbose

# Force colors when piping through a pager
dpfdev runner dpu-health --color always | less -R
```

By default, physical checks SSH to the active physical CI setups (`cloud-dev-ci01..04`, `setup28-dpf`, `setup34-dpf`, `setup35-dpf`), then probe `worker1` and `worker2` from each host. NIC Cloud checks discover active runners with the `managed/nic-cloud` tag, SSH to each runner, source `/workspace/cloud_tools/.setup_info`, and probe `CLOUD_PLAYER_1_IP` and `CLOUD_PLAYER_2_IP`.

Physical checks require DPU PF interfaces to be present and `up` by default. NIC Cloud checks only require PF interfaces to exist, because those links can be `down` when no test is currently using the setup. When a reachable worker has missing DPU PF interfaces, the command also checks `kubectl get dpus -A` from the runner environment and fills the `DPU_PHASE` column with the current phase names (for example `OS Installing`). Rows with bad interfaces and a known DPU phase are treated as passing, because the lifecycle state explains the missing host interfaces. If local `kubectl` is unavailable, it falls back to the master host (`--master-host`, or `MASTER_HOST`, default `master1`).

##### Credentials

Physical and NIC Cloud setups use **different** SSH credentials, resolved independently:

| Environment | SSH user (default, overridable) | Password (required, env only) |
| --- | --- | --- |
| Physical | `depuser` — `--physical-user` or `PHYSICAL_USER` | `PHYSICAL_PASSWORD` |
| NIC Cloud | `root` — `--cloud-user` or `CLOUD_SSH_USER` | `VM_PASSWORD` (or `CLOUD_SSH_PASSWORD`) |

GitLab discovery of NIC Cloud runners uses `GITLAB_TOKEN` (or `GITLAB_API_TOKEN` / `GITLAB_RUNNER_GROUP_MAINTAINER_TOKEN`). Passwords are never taken as command-line flags. If a selected pool has no password set, it is skipped with a warning instead of failing the whole run, so you can check one environment even when only its password is available. Use `--secrets-file <file>` to load any of these variables from an env file before probing.

The default output is summary-only. Use `--verbose` to show per-host probe progress. Output color is enabled automatically for terminals and disabled for redirected logs; use `--color always`/`never` to override.

For all options, use:

```bash
dpfdev runner dpu-health --help
```

### Test Commands

Commands for running tests and validations.

#### Test Documentation

Test markdown documentation by executing bash or shell commands in code blocks.

```bash
dpfdev test docs --file <path-to-markdown>
```

##### Custom Code Block Tags

You can tag code blocks with custom identifiers to selectively execute them. This is useful for testing different scenarios (e.g., OCI vs HTTP registries, dev vs production, etc.).

**Tagging Code Blocks:**

Add space-separated tags after the language identifier (e.g., \`\`\`shell oci\`\`\`). Multiple tags can be specified, and blocks are included if ANY tag matches the filter.

<details>
<summary>Example Markdown with Tagged Code Blocks</summary>

````markdown
```shell oci
# This block only runs with --tags oci
helm upgrade --install -n dpf-operator-system dpf-operator $REGISTRY/dpf-operator --version=$TAG
```

```shell http
# This block only runs with --tags http
helm repo add --force-update dpf-repository ${REGISTRY}
helm repo update
helm upgrade --install -n dpf-operator-system dpf-operator dpf-repository/dpf-operator --version=$TAG
```

```shell no-exec
# This block is skipped unless you explicitly enable it with --tags no-exec
# Useful for examples that shouldn't run by default
rm -rf /
```

```shell
# This block always runs (no tags = always executes)
echo "Hello World"
```
````

</details>

```bash
# Run only blocks tagged with 'oci' (plus all untagged blocks)
dpfdev test docs --file <path-to-markdown> --tags oci
```

**Block Execution Rules:**

1. **Untagged blocks** (e.g., \`\`\`shell\`\`\`) always execute regardless of filters
2. **Tagged blocks** (e.g., \`\`\`shell oci\`\`\`) only execute if at least one tag matches `--tags`
3. When `--tags` is **not specified**: only untagged blocks execute
4. Multiple tags in `--tags` act as OR logic: any match includes the block

You can see more information about the tool with:

```bash
dpfdev --help
```

##### Output Format

The command will:
1. Show each command with its line number
2. Indicate success (✓) or failure (✗) for each command
3. Show command output if:
    - The command failed
    - Verbose mode is enabled
4. Show a summary of failed commands at the end

Example output:
```
L7 > echo "Hello World"
  ✓ Success
L8 > ls -la
  ✓ Success
L12 > invalid-command
  ✗ Failed: error executing command 'invalid-command' (line 12): exit status 127
Output: bash: line 1: invalid-command: command not found

Summary: 1 command(s) failed
```

##### Markdown Format

The tool looks for  or shell code blocks in the markdown file:

````markdown
```bash
echo "Hello bash"
ls -la
```

```shell
echo "Hello shell"
ls -la
```

```sh
echo "Hello sh"
ls -la
```

````

Each line in a bash or shell code block is treated as a separate command to execute. 