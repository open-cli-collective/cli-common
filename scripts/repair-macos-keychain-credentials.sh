#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Repair macOS Keychain metadata/ACLs for Open CLI Collective credentials.

Dry-run by default:
  scripts/repair-macos-keychain-credentials.sh

Apply changes:
  scripts/repair-macos-keychain-credentials.sh --apply

What apply does for each present Keychain item:
  1. reads the existing secret value with `security find-generic-password -w`
  2. deletes the old item
  3. recreates it with label "<service> <profile>/<key>"
  4. adds explicit trusted application paths for the relevant installed CLI(s)

Secret values are never printed and are not passed as process arguments.
USAGE
}

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This repair script is macOS-only." >&2
  exit 1
fi

apply=0
while (($#)); do
  case "$1" in
    --apply)
      apply=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if ! command -v security >/dev/null 2>&1; then
  echo "missing required command: security" >&2
  exit 1
fi

to_hex() {
  if command -v xxd >/dev/null 2>&1; then
    xxd -p -c 1000000
  else
    od -An -v -tx1 | tr -d ' \n'
  fi
}

security_quote() {
  local s=$1
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}

extract_credential_ref() {
  local path=$1
  if [[ ! -f "$path" ]]; then
    return 1
  fi
  awk -F: '
    $1 ~ /^[[:space:]]*credential_ref[[:space:]]*$/ {
      value=$2
      sub(/^[[:space:]]*/, "", value)
      sub(/[[:space:]]*$/, "", value)
      gsub(/^"|"$/, "", value)
      if (value != "") {
        print value
        found=1
        exit
      }
    }
    END {
      if (!found) exit 1
    }
  ' "$path" 2>/dev/null
}

read_credential_ref() {
  local tool=$1 default_ref=$2
  shift 2

  local path ref seen="" refs=() sources=()
  for path in "$@"; do
    if ref=$(extract_credential_ref "$path"); then
      sources+=("$path")
      case "|$seen|" in
        *"|$ref|"*) ;;
        *)
          seen+="${seen:+|}${ref}"
          refs+=("$ref")
          ;;
      esac
    fi
  done

  case "${#refs[@]}" in
    0)
      printf '%s\n' "$default_ref"
      ;;
    1)
      printf '%s\n' "${refs[0]}"
      ;;
    *)
      skip_lines+=("$tool: skipped; conflicting credential_ref values across ${sources[*]}")
      return 1
      ;;
  esac
}

split_ref() {
  local ref=$1
  if [[ "$ref" != */* || "$ref" == */*/* || "$ref" == /* || "$ref" == */ ]]; then
    return 1
  fi
}

target_lines=()
skip_lines=()

add_target() {
  local name=$1 ref=$2 key=$3 apps=$4
  if ! split_ref "$ref"; then
    skip_lines+=("$name: skipped invalid ref $ref")
    return
  fi
  local service=${ref%%/*}
  local profile=${ref#*/}
  local account="${profile}/${key}"
  target_lines+=("${name}|${service}|${account}|${apps}")
}

add_static_targets() {
  local slck_ref nrq_ref gro_ref
  if slck_ref=$(read_credential_ref "slck" "slack-chat-api/default" \
    "$HOME/Library/Application Support/slack-chat-api/config.yml" \
    "${XDG_CONFIG_HOME:-$HOME/.config}/slack-chat-api/config.yml"); then
    add_target "slck bot_token" "$slck_ref" "bot_token" "slck"
    add_target "slck user_token" "$slck_ref" "user_token" "slck"
  fi
  if nrq_ref=$(read_credential_ref "nrq" "newrelic-cli/default" \
    "$HOME/Library/Application Support/newrelic-cli/config.yml" \
    "${XDG_CONFIG_HOME:-$HOME/.config}/newrelic-cli/config.yml"); then
    add_target "nrq api_key" "$nrq_ref" "api_key" "nrq"
  fi
  if gro_ref=$(read_credential_ref "gro" "google-readonly/default" \
    "$HOME/Library/Application Support/google-readonly/config.yml" \
    "${XDG_CONFIG_HOME:-$HOME/.config}/google-readonly/config.yml"); then
    add_target "gro oauth_token" "$gro_ref" "oauth_token" "gro"
  fi
  add_target "jtk/cfl api_token" "atlassian-cli/default" "api_token" "jtk cfl"

  # salesforce-cli predates cli-common and stores account "oauth_token" directly.
  target_lines+=("sfdc oauth_token|salesforce-cli|oauth_token|sfdc")

  if [[ -d "$HOME/dev/hubspot-cli" ]]; then
    skip_lines+=("hspt: skipped; hubspot-cli currently stores access_token in config/env, not macOS Keychain")
  fi
}

add_cr_targets() {
  if ! command -v cr >/dev/null 2>&1; then
    skip_lines+=("cr: skipped; cr binary not found")
    return
  fi
  if ! command -v jq >/dev/null 2>&1; then
    skip_lines+=("cr: skipped; jq is needed to read cr config show --json safely")
    return
  fi

  local json
  if ! json=$(cr config show --json 2>/dev/null); then
    skip_lines+=("cr: skipped; cr config show --json failed (probably not configured)")
    return
  fi

  while IFS='|' read -r ref key store; do
    [[ -n "$ref" && -n "$key" ]] || continue
    # This script repairs macOS Keychain only. Non-local stores may be 1Password.
    if [[ -n "$store" && "$store" != "local-os" ]]; then
      skip_lines+=("cr ${ref}/${key}: skipped non-local credential store $store")
      continue
    fi
    add_target "cr ${ref}/${key}" "$ref" "$key" "cr"
  done < <(
    jq -r '
      .credential_refs[]? as $ref
      | ($ref.keys[]? // empty)
      | "\($ref.ref)|\(.key)|\($ref.store // "")"
    ' <<<"$json"
  )
}

keychain_item_exists() {
  local service=$1 account=$2
  security find-generic-password -s "$service" -a "$account" >/dev/null 2>&1
}

app_args_for() {
  local apps=$1 app path out=()
  for app in $apps; do
    if path=$(command -v "$app" 2>/dev/null); then
      out+=("-T" "$path")
    fi
  done
  if ((${#out[@]})); then
    printf '%s\0' "${out[@]}"
  fi
}

describe_apps() {
  local apps=$1 app path out=()
  for app in $apps; do
    if path=$(command -v "$app" 2>/dev/null); then
      out+=("$app=$path")
    else
      out+=("$app=<missing>")
    fi
  done
  local joined="" item
  for item in "${out[@]}"; do
    if [[ -n "$joined" ]]; then
      joined+=", "
    fi
    joined+="$item"
  done
  printf '%s' "$joined"
}

repair_item() {
  local name=$1 service=$2 account=$3 apps=$4
  local app_args=() app_arg_count secret hex label comment cmd

  while IFS= read -r -d '' arg; do
    app_args+=("$arg")
  done < <(app_args_for "$apps")
  app_arg_count=${#app_args[@]}

  if ((app_arg_count == 0)); then
    echo "SKIP $name ($service/$account): no installed target app found from: $apps"
    return 0
  fi

  if ! keychain_item_exists "$service" "$account"; then
    echo "SKIP $name ($service/$account): no matching Keychain item"
    return 0
  fi

  echo "FOUND $name ($service/$account): trusted apps: $(describe_apps "$apps")"
  if ((apply == 0)); then
    return 0
  fi

  if ! secret=$(security find-generic-password -s "$service" -a "$account" -w); then
    echo "ERROR $name ($service/$account): failed to read existing secret" >&2
    return 1
  fi
  hex=$(printf '%s' "$secret" | to_hex)
  if [[ -z "$hex" ]]; then
    echo "ERROR $name ($service/$account): existing secret is empty; refusing to rewrite" >&2
    return 1
  fi

  label="${service} ${account}"
  comment="Credential for ${service} ${account}"

  if ! security delete-generic-password -s "$service" -a "$account" >/dev/null 2>&1; then
    echo "ERROR $name ($service/$account): failed to delete old item" >&2
    return 1
  fi

  cmd="add-generic-password -s $(security_quote "$service") -a $(security_quote "$account")"
  cmd+=" -l $(security_quote "$label") -j $(security_quote "$comment") -D \"application password\" -U"
  local i=0
  while ((i < app_arg_count)); do
    cmd+=" ${app_args[$i]} $(security_quote "${app_args[$((i + 1))]}")"
    i=$((i + 2))
  done
  cmd+=" -X ${hex}"

  if ! printf '%s\n' "$cmd" | security -i >/dev/null; then
    echo "ERROR $name ($service/$account): recreate failed; attempting restore without target app ACL" >&2
    printf 'add-generic-password -s %s -a %s -l %s -j %s -D "application password" -U -X %s\n' \
      "$(security_quote "$service")" \
      "$(security_quote "$account")" \
      "$(security_quote "$label")" \
      "$(security_quote "$comment")" \
      "$hex" | security -i >/dev/null || true
    return 1
  fi

  echo "REPAIRED $name ($service/$account)"
}

add_static_targets
add_cr_targets

echo "Mode: $([[ $apply == 1 ]] && echo apply || echo dry-run)"
echo

for line in "${target_lines[@]}"; do
  IFS='|' read -r name service account apps <<<"$line"
  repair_item "$name" "$service" "$account" "$apps"
done

if ((${#skip_lines[@]})); then
  echo
  echo "Notes:"
  for line in "${skip_lines[@]}"; do
    echo "- $line"
  done
fi

if ((apply == 0)); then
  echo
  echo "No changes made. Re-run with --apply to delete/recreate the found items."
fi
