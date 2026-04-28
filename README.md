# sao

`sao` is a config-driven local orchestrator for running coding agents across registered repositories on an idle worker machine.

## Current status

This repository now contains a working MVP implementation for the machine and repo orchestration flow:

- `sao init-machine`
- `sao init-repo`
- `sao add-repo /path/to/repo`
- `sao validate`
- `sao plan`
- `sao once`
- `sao`

What works today:

- machine config and repo config creation
- registered repo management
- git remote detection per repo
- GitHub issue discovery through `gh`
- deterministic candidate ranking
- single-task dispatch through bundled `agentapi`
- local state tracking to avoid redispatching unchanged issues

What still needs real runtime packaging:

- shipping the bundled `agentapi` binary under `libexec/`
- local compilation and smoke testing once a Go toolchain is available
