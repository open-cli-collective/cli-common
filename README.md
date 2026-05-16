# cli-common

Shared libraries for the Open CLI Collective CLIs.

## `credstore`

`github.com/open-cli-collective/cli-common/credstore` is the shared
credential-store library. It implements the **Open CLI Collective
Secret-Handling Standard** (`working-with-secrets.md`), the source of truth
for how Collective CLIs handle secrets and credentials.

The standard is maintained outside this repository and is not published;
this README references it by name and section number rather than by link.
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
