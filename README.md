# cli-common

Shared libraries for the Open CLI Collective CLIs.

## `credstore`

`github.com/open-cli-collective/cli-common/credstore` is the shared
credential-store library. It implements the **Open CLI Collective
Secret-Handling Standard** ([`docs/working-with-secrets.md`](docs/working-with-secrets.md)),
the source of truth for how Collective CLIs handle secrets and credentials.

## Standards

The Collective's cross-CLI standards are versioned here, alongside the code
they govern, so consumers pin them with the same module version. See
[`docs/README.md`](docs/README.md) for a one-line "use this when…" index:

- [`docs/working-with-secrets.md`](docs/working-with-secrets.md) — secret
  state (OS keyring; implemented by `credstore`).
- [`docs/working-with-state.md`](docs/working-with-state.md) — **non-secret**
  on-disk state (config + cache) and its rollout plan; companion pillar to
  the secrets standard.
- [`docs/command-surface.md`](docs/command-surface.md) — command-tree shape:
  verbs, positional-vs-flag, mutation safety, prompt classes, async, flag
  conventions.
- [`docs/output-and-rendering.md`](docs/output-and-rendering.md) — what a
  command prints: text-first, `--id`/`--extended`/`--fulltext`/`--fields`,
  tables, key:value blocks, ISO-8601, stream discipline, JSON scope, the
  data ↔ presentation seam.
- [`docs/scriptability.md`](docs/scriptability.md) — installer-script
  ergonomics: `init` wizard parity, `--non-interactive`, exit codes,
  browser-open, `--profile` reservation.

Tracking: epic **INT-310** (Get Claude desktop working for people).

### Status

This is an in-progress build (standard §2.1). Implemented so far:

- **Credential-ref grammar** (§1.3): `ParseRef`, `FormatRef`,
  `EscapeRefSegment`, and the §2.1 default-ref codification (`DefaultProfile`,
  `DefaultRef`). A ref is `"<service>/<profile>"` — two non-empty segments
  drawn from `[A-Za-z0-9_-]`, joined by a single `/`. Errors are the typed
  `*RefError`, matchable via `errors.Is` against `ErrRefEmpty`,
  `ErrRefSegmentCount`, `ErrRefInvalidChar`.

Not yet implemented (separate units of work under INT-310): the OS-keyring
backends (Keychain / Credential Manager / Secret Service / encrypted file),
the `Store`/`Open` lifecycle, single-key and bundle operations, `SetBundle`
atomicity, Linux fail-closed backend classification, redaction helpers, and
legacy-migration helpers.

## Development

```sh
make check   # tidy + lint + test
```

Requires Go 1.24+ and `golangci-lint` (v2). The module is standard-library
only; there is no `go.sum`.

## License

MIT — see [LICENSE](LICENSE).
