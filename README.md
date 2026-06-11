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
[`docs/README.md`](docs/README.md) for a one-line "use this when…" index and
the convention for citing GitHub sources of truth with optional local workspace
copies:

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

The `credstore` package implements the standard's runtime surface: credential-ref
grammar (§1.3), the `Store`/`Open` lifecycle, single-key and bundle ops,
OS-keyring backends (Keychain / Credential Manager / Secret Service / encrypted
file) with Linux fail-closed classification (§1.4), `--backend` flag plumbing,
redaction helpers, and legacy-migration helpers (§1.8). The `cache` package
implements the tier-1 universal core from `working-with-state.md` §6b
(envelope + atomic write + freshness `Classify`). The `statedir` package
provides the shared path/dir resolver from `working-with-state.md` §6a.

For component-by-component conformance status and the rollout matrix across
consumer CLIs, see `docs/working-with-state.md` §6 and §7 and
`docs/working-with-secrets.md` §2.1.

## Development

```sh
make check   # tidy + lint + test
```

Requires Go 1.26+ and `golangci-lint` (v2).

## License

MIT — see [LICENSE](LICENSE).
