# Bundled Runtime

Packaged `sao` releases are expected to place the bundled `agentapi` binary in this directory.

Expected paths:

- `libexec/agentapi`
- `libexec/agentapi.exe` on Windows

Development checkouts may not include the binary. `sao validate` warns about that case instead of requiring a separate manual install.
