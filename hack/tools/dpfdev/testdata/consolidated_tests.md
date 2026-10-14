# Consolidated Test Cases

## Test Case 1: Valid Commands
This section contains valid bash commands that should execute successfully.

```bash
echo "Hello World"
ls -la
```
## Test Case 2: Environment Variable Persistence
This section tests that environment variables set in one command are available to subsequent commands.

```bash
# Set an environment variable
export TEST_VAR="Hello from environment"
```

```bash
echo $TEST_VAR
: ${TEST_VAR:?env not set}
```

## Test Case 2: Invalid Command
This section contains an invalid command that should fail.

```bash   
invalid-command-that-doesnt-exist
```

## Test Case 3: No Commands
This section contains no bash commands, just regular markdown content.
No code blocks here.

## Test Case 4: Multiple Valid Commands with Output
This section contains multiple valid commands that produce output.

```bash
echo "Testing multiple commands"
pwd
whoami
```

## Test Case 5: Empty Command
This section contains an empty command block that should be skipped.

```bash

```

## Test Case 6: Mixed Content
This section contains a mix of text and commands.

Some text before the command.
```bash
echo "Command in the middle"
```
Some text after the command. 

## Test Case 7: Different Syntax Highlighting
This section demonstrates different syntax highlighting options that should all work.

```bash
echo "This is a bash block"
```

```shell
echo "This is a shell block"
```

```sh
echo "This is a sh block"
```