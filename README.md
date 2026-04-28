# Simple Agent Orchestration

`sao` is a CLI that watches GitHub repositories for eligible issues, ranks them, and dispatches work to supported coding agents like Codex and Claude.

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

2. In a repo you want `sao` to watch, create the repo config:

```bash
sao init-repo
```

3. Register that repo in the machine config:

```bash
sao add-repo /path/to/repo
```

4. Validate the setup:

```bash
sao validate
```

5. Preview what would run:

```bash
sao plan
```

6. Run one cycle:

```bash
sao once
```

7. Or run the foreground loop:

```bash
sao
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
- `sao init-repo`
- `sao add-repo /path/to/repo`
- `sao validate`
- `sao agents`
- `sao plan`
- `sao once`
- `sao`

## Notes

- This is currently an MVP.
- Dispatch is single-task oriented even though the machine config includes concurrency fields.
- Direct execution is currently implemented for `claude` and `codex`.
