# Scripts

Shared support scripts for Open CLI Collective repositories.

## `repair-macos-keychain-credentials.sh`

Repairs macOS Keychain generic-password ACLs for Collective CLI credentials
that still trust ad-hoc or per-build `cdhash` identities instead of the current
stable-signed CLI binaries.

Default mode is inspect-only:

```bash
scripts/repair-macos-keychain-credentials.sh
```

Preview an additive heal:

```bash
scripts/repair-macos-keychain-credentials.sh --heal
```

Apply an additive heal:

```bash
scripts/repair-macos-keychain-credentials.sh --heal --apply
```

Clean up already-healed `stable+stale-cdhash` items by rebuilding them into
canonical metadata and stable app ACLs:

```bash
scripts/repair-macos-keychain-credentials.sh --cleanup
scripts/repair-macos-keychain-credentials.sh --cleanup --apply
```

Use the heavy rebuild path when an item should be recreated canonically from its
current secret value:

```bash
scripts/repair-macos-keychain-credentials.sh --rebuild --tool nrq
scripts/repair-macos-keychain-credentials.sh --rebuild --tool nrq --apply
```

Limit discovery to one or more tools:

```bash
scripts/repair-macos-keychain-credentials.sh --tool cr --tool nrq
```

Run this as the normal macOS user who owns the login Keychain, not with `sudo`.
Real mutation requires `--apply` plus exactly one action: `--heal`, `--cleanup`,
or `--rebuild`. `--apply` alone exits without scanning or mutating.

`--heal --apply` does not read, print, delete, or recreate secret values. It is
intentionally additive: it appends missing stable-signed trusted application
grants to explicit decrypt ACL app lists and preserves existing trusted
applications. It skips `NULL` or non-explicit app-list ACLs instead of narrowing
their meaning.

`--cleanup --apply` and `--rebuild --apply` read the existing secret value,
delete the old Keychain item, and recreate it with canonical label,
description, and stable-signed app ACLs. They never print secrets or pass them
as process arguments.

`stable+stale-cdhash` means the current stable-signed app is already trusted.
macOS may still report older cdhash grants or partition metadata for that item;
the script treats that state as repaired.
