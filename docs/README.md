# cli-common standards

Five normative documents define the surface every Open CLI Collective CLI ships. New CLIs implement to these.

**Conflict resolution order**, from highest to lowest authority:

1. `working-with-secrets.md` (foundational, predates the rest)
2. `working-with-state.md` (foundational, predates the rest)
3. `command-surface.md` — owns command-tree shape and flag taxonomy
4. `output-and-rendering.md` — owns what a command prints; defers to `command-surface.md` for what flags exist
5. `scriptability.md` — synthesizes the others for installer-script use; defers to all four above for the rules it cross-refs

When two docs appear to conflict, the one higher on the list wins **on the surfaces that doc actually defines.** `working-with-secrets.md` governs credential ingress flags, keyring write behavior, and the `_migration` JSON envelope — not (for example) the verb chosen for a credential-rotation command, which remains `command-surface.md`'s domain. `working-with-state.md` governs config and cache layout, the `refresh` command's signature, hermetic test isolation — not output formatting of a `refresh --status` listing, which is `output-and-rendering.md`'s domain. The hierarchy decides which doc's stance prevails *within its own scope*; out-of-scope claims do not auto-win.

| Doc | Use this when… |
|---|---|
| [`working-with-secrets.md`](working-with-secrets.md) | Working with anything credential-related — keyring backends, `credential_ref`, `init` secret ingress, `set-credential`, `--overwrite`, deployment material vs access secret, file-backend fallback, the `_migration` JSON envelope. |
| [`working-with-state.md`](working-with-state.md) | Working with non-secret on-disk state — config file location, cache layout, atomic writes, hermetic test isolation, the `refresh` command, legacy migration acceptance matrix. |
| [`command-surface.md`](command-surface.md) | Adding or naming commands and flags — verbs (`create` / `delete` / `add` / `remove`), positional-vs-flag, mutation safety (resource `--force` vs credential `--overwrite`), the two prompt classes (setup wizards vs safety confirmations), boolean discipline, async (`--wait`/`--no-wait`), short-alias map, naming hygiene. |
| [`output-and-rendering.md`](output-and-rendering.md) | Shaping what a command prints — text-first principle, the `--id` / `--extended` / `--fulltext` / `--fields` coordinate system, pipe-delimited tables, key:value blocks, ISO-8601 dates, pagination, name/ID resolution, stdout/stderr stream discipline, color stance, JSON scope, the data ↔ presentation seam. |
| [`scriptability.md`](scriptability.md) | Making a CLI deployable — `init` wizard parity, `--non-interactive` + TTY detection, exit codes (the `me` health-check contract), the browser-open pattern, `--profile` reservation, cross-refs to secret-ingress and `refresh`. |

Tool-specific specs (jtk's `internal/cmd/GUARDRAILS.md` and `internal/cmd/OUTPUT_SPEC.md`) instantiate the family-wide layer above and add per-tool decisions on top.

These docs are forward-looking pattern docs. Per-CLI divergences from the standards are catalogued inline in each doc's "Current divergences" section; backporting the standards to existing CLIs is a separate workstream.
