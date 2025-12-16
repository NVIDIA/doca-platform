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