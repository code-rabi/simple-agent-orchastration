# Tech Design: Simple Agent Orchestration

## Recommendation

Build the deliverable as a **Go CLI binary**.

Do **not** make the primary deliverable Rust or `npx` for v1.

Why Go:

- The product is a deterministic orchestration CLI, not a heavy application server.
- It needs strong subprocess management for `gh`, `git`, and `agentapi`.
- A single static binary is a better operator experience than requiring Node.js.
- `coder/agentapi` is itself a Go project and already exposes a clean HTTP boundary, which makes Go a natural fit for a bundled execution runtime.
- Go is fast enough, simple enough, and easier to maintain than Rust for this scope.

## Decision Summary

### Chosen

- Language: **Go**
- Deliverable: **single CLI binary**
- Primary integrations:
  - `gh` CLI for GitHub issue and PR operations
  - `git` CLI for repo state checks and branch/worktree operations if needed later
  - bundled `agentapi` for unified control of coding agents

### Not Chosen

#### Rust

Rust would be a good fit for a long-term systems tool, but it is the wrong optimization for v1:

- more implementation overhead
- slower iteration for a small team
- less leverage from the existing Go-based `agentapi` ecosystem

#### `npx` / Node CLI

Node would speed up prototyping, but it weakens the product shape:

- requires users to have a working Node runtime
- less ideal for a deterministic local ops tool
- weaker default story for shipping one self-contained executable
- no direct ecosystem advantage from the `agentapi` reference implementation

`npx` can still be a **future wrapper** around the Go binary if distribution convenience matters.

## Product Framing

This tool is a **config-driven local orchestrator for an idle worker machine**:

- deterministic orchestration
- agentic task execution
- zero LLM tokens spent on orchestration decisions

The orchestrator should support a machine that can work across multiple repos over time.

That means the product has two layers:

- **machine config**
  - what this machine can run and which repos it should work on
- **repo config**
  - how a given repo should be worked

The orchestrator reads machine config, enumerates registered repos, loads each repo's committed policy config, inspects GitHub work, chooses the next eligible task, picks the best available coding agent according to policy, and launches execution through `agentapi`.

`agentapi` should be bundled and managed by `sao`, not installed separately by the user.

## Goals

- Read a machine-root config file.
- Read a committed repo policy config for each registered repo.
- Discover GitHub issues and PR work via deterministic filters.
- Prioritize work based on explicit rules.
- Route tasks to an available coding agent with fallback.
- Support onboarding new repos onto an existing worker machine.
- Respect concurrency limits and agent availability.
- Keep the orchestration layer simple, inspectable, and debuggable.

## Non-Goals

- No planning agent deciding what to do next.
- No autonomous long-horizon orchestration logic driven by LLMs.
- No custom abstraction replacing `agentapi`.
- No deep GitHub App integration in v1 if `gh` can cover the use case.

## Proposed CLI Shape

Binary name suggestion: `sao`

Commands:

- `sao`
  - runs the orchestration loop in the foreground with live logs across registered repos
- `sao init-machine`
  - creates the machine-root config
- `sao add-repo /path/to/repo`
  - registers a repo with the machine and optionally scaffolds repo config
- `sao init-repo`
  - creates repo-local policy config in the current repo
- `sao plan`
  - print candidate tasks and routing decisions without executing
- `sao agents`
  - show configured agents and current availability
- `sao validate`
  - validate machine config, repo config, and local prerequisites

For v1, `sao`, `sao init-machine`, `sao add-repo`, `sao init-repo`, and `sao validate` are enough.

Future CLI evolution:

- add `-d` or `--detach` later for background execution
- add service manager support later if we want long-running daemon installs

The default operating mode should stay simple:

- foreground process
- continuous loop
- readable running logs

## High-Level Architecture

```text
+----------------------+
| machine config       |
+----------+-----------+
           |
           v
+----------------------+
| project registry     |
| - registered repos   |
| - enabled/disabled   |
| - local repo paths   |
+----------+-----------+
           |
           v
+----------------------+
| repo config loader   |
| - load per-repo      |
| - infer repo context |
+----------+-----------+
           |
           v
+----------------------+
| selector engine      |
| - query GH items     |
| - apply filters      |
| - score priority     |
+----------+-----------+
           |
           v
+----------------------+
| scheduler            |
| - machine concurrency|
| - per-agent limits   |
| - choose agent       |
+----------+-----------+
           |
           v
+----------------------+
| executor             |
| - ensure agentapi    |
| - spawn session      |
| - submit task        |
| - stream status      |
+----------+-----------+
           |
           v
+----------------------+
| state/logging        |
| - machine run state  |
| - structured logs    |
| - retry markers      |
+----------------------+
```

## Core Modules

### 1. Config Loader

Responsibilities:

- load machine config
- load repo config
- apply defaults
- validate schema

Suggested files:

- `~/.config/sao/config.yaml`
- `.simple-agent-orchestration.yaml`

Config principle:

- only require values that cannot be inferred
- keep common timing and behavior values as internal defaults unless explicitly overridden

Split of responsibility:

- machine config stores operational facts about the worker box
- repo config stores repo-specific task selection and routing policy

### 2. Project Registry

Responsibilities:

- track which repos this machine should work on
- persist enabled and disabled state
- resolve local repo paths
- support repo onboarding

Suggested shape:

- machine config contains a `projects` list with local paths

Why this matters:

- avoids copying machine-level configuration into every repo
- lets one idle machine work across many repos
- makes onboarding a new repo a first-class workflow

### 3. GitHub Provider

Responsibilities:

- query issues/PRs using `gh`
- normalize output into internal task models
- support filters such as label, assignee, unassigned, and state

Why `gh` first:

- matches the PRD
- avoids premature GitHub API client work
- easy local authentication model
- repository can be inferred per registered repo from the local git remote instead of duplicated in config

Implementation detail:

- use `gh issue list` / `gh pr list` / `gh api`
- require JSON output and parse it in Go

### 4. Selector Engine

Responsibilities:

- convert repo config rules into candidate sets
- score tasks deterministically
- break ties predictably

Example priority inputs:

- explicit priority labels
- age
- unassigned status

### 5. Agent Registry

Responsibilities:

- define machine-available agents and default fallback order
- check whether an agent is usable locally
- represent token/quota/manual-disable status

Example agents:

- Claude Code
- Codex
- Gemini

The orchestrator should treat agent availability as a deterministic local fact, not a model decision.

### 6. Executor

Responsibilities:

- start or reuse bundled `agentapi`
- launch an agent session
- send the work item prompt and metadata
- observe completion or failure

Design choice:

- integrate with `agentapi` over its HTTP API
- treat `agentapi` as the control plane boundary
- ship `agentapi` as an internal managed runtime dependency of `sao`

This keeps the orchestrator lean and avoids re-implementing provider-specific behavior.

## Dependency Model

`sao` should own the execution stack boundary cleanly.

Bundled with `sao`:

- `agentapi`

Required on the machine:

- `gh`
- selected coding-agent CLIs such as `claude`, `codex`, or `gemini`

Not acceptable:

- asking users to manually install `agentapi` as a separate prerequisite

Recommended packaging model:

- release `sao` with the correct `agentapi` binary for the target platform
- `sao` launches the bundled binary internally
- `sao` pins the `agentapi` version it was tested against

Validation model:

- `sao validate` checks machine config, repo config, `gh`, and configured agent CLIs
- `sao validate` should not require users to install `agentapi` separately

### 7. Local State Store

Responsibilities:

- avoid duplicate execution
- track in-flight work
- store last run metadata
- support crash recovery

Suggested format:

- local JSON file under `~/.local/state/sao/state.json`

v1 can use a file lock plus JSON state. No database needed.

## Config Model

### Machine Config

Suggested file:

- `~/.config/sao/config.yaml`

```yaml
runtime:
  max_concurrent_tasks: 2

agents:
  default_order: ["claude", "codex", "gemini"]
  installed:
    - name: claude
      type: claude
      command: ["claude"]
      enabled: true
      max_parallel: 1
      healthcheck: ["which", "claude"]
    - name: codex
      type: codex
      command: ["codex"]
      enabled: true
      max_parallel: 1
      healthcheck: ["which", "codex"]
    - name: gemini
      type: gemini
      command: ["gemini"]
      enabled: true
      max_parallel: 1
      healthcheck: ["which", "gemini"]

projects:
  - path: /work/project-a
    enabled: true
  - path: /work/project-b
    enabled: true
```

Machine config owns:

- registered repo paths
- machine-wide concurrency
- installed agent definitions
- default routing order
- runtime overrides

### Repo Config

Suggested file:

- `.simple-agent-orchestration.yaml`

```yaml
version: 1

selection:
  sources:
    - type: issue
      filters:
        state: open
        labels: ["agent-ready"]
        assignee: unassigned
    - type: pr
      filters:
        state: open
        labels: ["agent-fix"]

priority:
  labels:
    "P0": 100
    "P1": 80
    "P2": 50

routing:
  preferred_order: ["claude", "codex"]
```

Repo config owns:

- GitHub selection rules
- priority rules
- repo-specific routing preference overrides
- any repo guardrails for task execution

Notes:

- repo identity is intentionally omitted because the config is committed inside the repo it applies to
- machine-level agent installation details do not belong in repo config
- polling interval is intentionally omitted from the base example because `sao` should have a reasonable built-in default loop interval
- only overrides should be written when teams need behavior different from the defaults

Optional override example:

```yaml
runtime:
  poll_interval_seconds: 120
```

## Repo Onboarding Flow

The tool should make adding a new project lightweight.

Expected flow:

1. run `sao init-machine` once on the worker machine
2. clone or prepare a repo locally
3. run `sao init-repo` inside the repo if repo config does not exist yet
4. run `sao add-repo /path/to/repo`
5. start `sao`

Design principle:

- onboarding a new repo should mean registering its path and, if needed, scaffolding a small repo policy file
- users should not have to duplicate machine-level CLI and agent configuration for every new project

## Task Lifecycle

### 1. Discovery

- load machine config
- enumerate enabled registered repos
- load repo config for each repo
- infer GitHub repo from each local git remote
- verify `gh` auth
- query matching issues/PRs

### 2. Selection

- filter out tasks already running or recently failed beyond retry policy
- compute deterministic score
- sort candidates across all eligible repos

### 3. Scheduling

- respect machine-wide concurrency
- respect per-agent concurrency
- assign each selected task to the first healthy agent in repo-preferred order, falling back to machine defaults

### 4. Execution

- ensure bundled `agentapi` is running for the chosen agent
- create a task prompt from a stable template
- submit the task

### 5. State Update

- mark task as running
- persist session metadata
- record success/failure on completion

## Prompting Boundary

The orchestrator should only construct a **small deterministic task envelope**:

- issue or PR URL
- title
- body excerpt or fetched body
- repository path
- expected outcome
- any guardrails from config

It should not perform planning, summarization, or autonomous reasoning itself.

## Failure Model

Expected failures:

- `gh` not authenticated
- selected agent binary missing
- selected agent has no tokens or fails health check
- bundled `agentapi` process fails to start
- GitHub item becomes ineligible before execution

Fallback rules:

- if agent A fails health check, try next configured agent
- if task dispatch fails, mark attempt and continue
- if a task is already in progress, skip it

## Observability

Use structured logs from day one.

Suggested output:

- human-readable console logs in the foreground by default
- optional JSON logs for automation

Key events:

- machine config loaded
- repo registered
- repo config loaded
- candidates discovered
- task selected
- agent assigned
- execution started
- execution completed
- execution failed

## Security and Trust Model

- rely on local `gh` authentication
- rely on locally installed agent CLIs
- bundle and manage `agentapi` internally
- keep secrets out of committed config
- support environment variable references for tokens if ever needed

The orchestrator should not become a secret manager.

## MVP Scope

Build this first:

- machine config loader
- repo config loader
- `init-machine` command
- `init-repo` command
- `add-repo` command
- `validate` command
- `sao` foreground loop command
- GitHub issue discovery via `gh`
- deterministic prioritization
- single-task execution
- fallback across 2-3 configured agents
- project registry in machine config
- local machine state file

## V2 Scope

- detached/background mode
- PR support
- per-agent rate limits
- richer health checks
- worktree/branch isolation
- retry policy tuning
- service manager integration
- webhook-triggered runs instead of polling
- remote fleet management or shared coordinator

## Why Go Is the Best Fit

From the referenced projects checked on **April 28, 2026**:

- `coder/agentapi` is a Go project and exposes an HTTP API for multiple coding agents.
- `ComposioHQ/agent-orchestrator` is a TypeScript project distributed through npm.

That split reinforces the recommendation:

- take inspiration from the config UX of the TypeScript orchestrator
- build this product in Go because the PRD explicitly wants a very lean, deterministic local orchestrator centered around a bundled `agentapi` runtime

In short:

- choose **Go** for the implementation
- ship a **binary** as the real deliverable
- optionally add a tiny install wrapper later if distribution convenience becomes important

## Suggested Project Layout

```text
/cmd/sao/main.go
/internal/config
/internal/github
/internal/priority
/internal/agents
/internal/executor
/internal/state
/internal/cli
```

## Implementation Notes

- Prefer calling `gh` rather than binding directly to GitHub SDKs in v1.
- Prefer calling `agentapi` via HTTP rather than importing internals.
- Keep provider interfaces small and concrete.
- Avoid plugin systems until there is real pressure for them.

## First Build Order

1. `validate` command
2. config schema and loader
3. `gh` discovery adapter
4. priority selector
5. agent health checks
6. `agentapi` executor
7. state tracking
8. `run` end-to-end flow

## Open Questions

- Should v1 orchestrate only issues, or issues plus PRs?
- Should task claiming write back to GitHub labels or comments, or remain purely local in v1?
- Do we want one shared `agentapi` process per agent type, or one process per task?

My recommendation:

- support issues first
- keep claiming local first
- use one `agentapi` process per running task in v1 for simpler isolation
- make machine config the operational entrypoint and repo config the policy entrypoint

## References

- PRD: [prd.md](/home/nitayr/projects/simple-agent-orchastration/prd.md)
- `coder/agentapi`: https://github.com/coder/agentapi
- `ComposioHQ/agent-orchestrator`: https://github.com/ComposioHQ/agent-orchestrator
