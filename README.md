# sao

`sao` is a config-driven local orchestrator for running coding agents across registered repositories on an idle worker machine.

Current execution backend:

- `acpx` (preferred if installed)
- fallback to `npx -y acpx@latest`

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
- single-task dispatch through `acpx`
- local state tracking to avoid redispatching unchanged issues
