# Simple Agent Orchestration

`sao` is a CLI that watches GitHub repositories for eligible issues, ranks them, and dispatches work to supported coding agents like Codex and Claude through `acpx`.

It keeps machine-level config in `~/.config/sao/config.yaml`, repo-level config in `.simple-agent-orchestration.yaml`, and local task state under `~/.local/state/sao/`.

## Example Config

Machine config at `~/.config/sao/config.yaml`:

```yaml
runtime:
  max_concurrent_tasks: 2
  poll_interval_seconds: 300

agents:
  default_order:
    - codex
    - claude
  installed:
    - name: codex
      type: codex
      command: [codex]
      enabled: true
      max_parallel: 1
      healthcheck: [which, codex]
    - name: claude
      type: claude
      command: [claude]
      enabled: true
      max_parallel: 1
      healthcheck: [which, claude]

projects:
  - path: /Users/you/work/my-repo
    enabled: true
```

Repo config at `/path/to/repo/.simple-agent-orchestration.yaml`:

```yaml
version: 1

selection:
  sources:
    - type: issue
      filters:
        state: open
        labels: [agent-ready]
        assignee: unassigned

priority:
  labels:
    P0: 100
    P1: 80
    P2: 50

routing:
  preferred_order:
    - codex
    - claude
```

## Requirements

- `gh` installed and authenticated
- `codex` and/or `claude` installed
- At least one supported agent configured in machine config

## Install

Install the latest GitHub release for your platform.

macOS / Linux / Bash on Windows:

```bash
curl -fsSL https://raw.githubusercontent.com/code-rabi/simple-agent-orchastration/main/install.sh | bash
```

PowerShell on Windows:

```powershell
irm https://raw.githubusercontent.com/code-rabi/simple-agent-orchastration/main/install.sh | bash
```

By default the installer uses the latest release, installs to `/usr/local/bin` when writable, and otherwise falls back to `~/.local/bin`.

Useful overrides:

- `SAO_INSTALL_DIR=/custom/bin` to choose the install directory
- `SAO_VERSION_TAG=main-<commit-sha>` to install a specific release

## Build From Source

If you want to build `sao` yourself instead of installing a release:

- Go 1.24+

```bash
go build -o sao ./cmd/sao
```

## Quick Start

1. Create the machine config:

```bash
sao init-machine
```

2. In a repo you want `sao` to watch, create the repo config and register the repo in the machine config:

```bash
sao init-project
```

3. Validate the setup:

```bash
sao validate
```

4. Preview what would run:

```bash
sao plan
```

5. Run one cycle:

```bash
sao once
```

When a dispatched agent produces file changes, `sao` creates an isolated git worktree under `~/.local/state/sao/worktrees/`, commits the diff on a task branch, pushes it, opens a draft pull request, and prints the PR URL. The PR URL, branch, worktree path, commit SHA, and agent summary are also stored in the local state file under `~/.local/state/sao/`.

6. Or run the foreground loop:

```bash
sao
```

You can still run the setup steps separately with `sao init-repo` and `sao add-repo /path/to/repo` when needed.

## Update

Update an installed binary to the latest GitHub release:

```bash
sao update
```

## Default Task Selection

By default, a repo is configured to look for:

- open GitHub issues
- labeled `agent-ready`
- unassigned

Default label priority is:

- `P0` = 100
- `P1` = 80
- `P2` = 50

## Config Files

Machine config:

- `~/.config/sao/config.yaml`

Repo config:

- `/path/to/repo/.simple-agent-orchestration.yaml`

State:

- `~/.local/state/sao/`

## Supported Commands

- `sao init-machine`
- `sao init-project`
- `sao init-repo`
- `sao add-repo /path/to/repo`
- `sao update`
- `sao validate`
- `sao agents`
- `sao plan`
- `sao once`
- `sao`

## Notes

- This is currently an MVP.
- Agent execution is routed through `acpx`.
- Completed tasks run in isolated git worktrees and are delivered as draft pull requests when they produce a git diff.
- Supported agent runtimes today are `codex` and `claude`.
