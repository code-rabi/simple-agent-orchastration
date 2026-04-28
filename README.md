# sao

`sao` is a single Go binary that watches registered GitHub repositories, ranks eligible issues, and dispatches one task at a time through supported agent CLIs.

## What It Does Today

- Stores machine-wide config in `~/.config/sao/config.yaml`
- Stores per-repo config in `.simple-agent-orchestration.yaml`
- Discovers GitHub issues through `gh`
- Filters and ranks candidate tasks
- Dispatches the top task through a supported agent CLI
- Tracks local task state to avoid redispatching unchanged issues

## Prerequisites

- Go 1.24+
- `gh` installed and authenticated
- `codex` and/or `claude` installed
- At least one supported agent configured in machine config

## Build

```bash
go build -o sao ./cmd/sao
```

## Quick Start

1. Create the machine config:

```bash
./sao init-machine
```

2. In a repo you want `sao` to watch, create the repo config:

```bash
./sao init-repo
```

3. Register that repo in the machine config:

```bash
./sao add-repo /path/to/repo
```

4. Validate the setup:

```bash
./sao validate
```

5. Preview what would run:

```bash
./sao plan
```

6. Run one cycle:

```bash
./sao once
```

7. Or run the foreground loop:

```bash
./sao
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
