#!/usr/bin/env bash
set -euo pipefail

EXPECTED_LEAF_SHA="42e1afd02aae8666c09c15f171e1639550f301c2"

usage() {
  cat <<'USAGE'
Usage: scripts/repair-macos-keychain-credentials.sh [--heal|--cleanup|--rebuild] [--apply] [--keychain PATH] [--tool NAME]
       scripts/repair-macos-keychain-credentials.sh --self-test

Find Open CLI Collective macOS Keychain generic-password items whose ACLs still
trust ad-hoc or per-build cdhash identities instead of the current stable-signed
CLI binaries.

Default mode is inspect-only. Mutating runs require --apply and exactly one
action:

  --heal     append missing stable-signed trusted app grants; does not read,
             delete, print, or recreate secret values
  --cleanup  rebuild already-healed stable+stale-cdhash items to remove stale
             cdhash ACL/partition metadata
  --rebuild  rebuild discovered items into canonical metadata and stable app
             ACLs; heavy escape hatch

Without --apply, action modes are dry-runs.

Use --tool to limit discovery. Repeat it for more than one tool. Supported
values: slck, nrq, gro, jtk, cfl, atlassian, cr, sfdc.

Run this as your normal macOS user, not with sudo. Only --cleanup and --rebuild
read existing secret values, and they never print secrets or pass them as
process arguments.
USAGE
}

lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

expected_dr_for() {
  printf 'identifier "org.open-cli-collective.%s" and certificate leaf = H"%s"' "$1" "$EXPECTED_LEAF_SHA"
}

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

cleanup() {
  if [ -n "${work_tmp:-}" ]; then
    rm -rf "$work_tmp"
  fi
}

ensure_work_tmp() {
  if [ -z "${work_tmp:-}" ]; then
    work_tmp="$(mktemp -d)"
    targets_file="$work_tmp/targets.tsv"
    notes_file="$work_tmp/notes.txt"
    repairs_file="$work_tmp/repairs.tsv"
    rebuilds_file="$work_tmp/rebuilds.tsv"
    : >"$targets_file"
    : >"$notes_file"
    : >"$repairs_file"
    : >"$rebuilds_file"
  fi
}

set_action() {
  local requested=$1
  if [ "$action" != "inspect" ] && [ "$action" != "$requested" ]; then
    echo "choose only one action: --heal, --cleanup, or --rebuild" >&2
    exit 2
  fi
  action=$requested
}

add_note() {
  ensure_work_tmp
  printf '%s\n' "$1" >>"$notes_file"
}

tool_selected() {
  local tool=$1
  if [ "$tool_filter" = " " ]; then
    return 0
  fi
  case "$tool_filter" in
    *" $tool "*) return 0 ;;
    *) return 1 ;;
  esac
}

any_tool_selected() {
  local tool
  for tool in "$@"; do
    if tool_selected "$tool"; then
      return 0
    fi
  done
  return 1
}

extract_credential_ref() {
  local path=$1
  if [ ! -f "$path" ]; then
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

  local path ref seen refs sources
  seen=""
  refs=""
  sources=""
  for path in "$@"; do
    if ref=$(extract_credential_ref "$path"); then
      sources="${sources}${sources:+ }$path"
      case "|$seen|" in
        *"|$ref|"*) ;;
        *)
          seen="${seen}${seen:+|}$ref"
          refs="${refs}${refs:+
}$ref"
          ;;
      esac
    fi
  done

  case "$(printf '%s\n' "$refs" | sed '/^$/d' | wc -l | tr -d ' ')" in
    0)
      printf '%s\n' "$default_ref"
      ;;
    1)
      printf '%s\n' "$refs"
      ;;
    *)
      add_note "$tool: skipped; conflicting credential_ref values across $sources"
      return 1
      ;;
  esac
}

split_ref() {
  local ref=$1
  case "$ref" in
    */*/*|/*|*/|*" "*|"")
      return 1
      ;;
    */*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

add_target() {
  local display=$1 ref=$2 key=$3 apps=$4
  ensure_work_tmp
  if ! split_ref "$ref"; then
    add_note "$display: skipped invalid credential_ref $ref"
    return
  fi
  local service profile account
  service=${ref%%/*}
  profile=${ref#*/}
  account="$profile/$key"
  printf '%s\t%s\t%s\t%s\n' "$display" "$service" "$account" "$apps" >>"$targets_file"
}

add_static_targets() {
  local slck_ref nrq_ref gro_ref

  if tool_selected slck; then
    if slck_ref=$(read_credential_ref "slck" "slack-chat-api/default" \
      "$HOME/Library/Application Support/slack-chat-api/config.yml" \
      "${XDG_CONFIG_HOME:-$HOME/.config}/slack-chat-api/config.yml"); then
      add_target "slck bot_token" "$slck_ref" "bot_token" "slck"
      add_target "slck user_token" "$slck_ref" "user_token" "slck"
    fi
  fi

  if tool_selected nrq; then
    if nrq_ref=$(read_credential_ref "nrq" "newrelic-cli/default" \
      "$HOME/Library/Application Support/newrelic-cli/config.yml" \
      "${XDG_CONFIG_HOME:-$HOME/.config}/newrelic-cli/config.yml"); then
      add_target "nrq api_key" "$nrq_ref" "api_key" "nrq"
    fi
  fi

  if tool_selected gro; then
    if gro_ref=$(read_credential_ref "gro" "google-readonly/default" \
      "$HOME/Library/Application Support/google-readonly/config.yml" \
      "${XDG_CONFIG_HOME:-$HOME/.config}/google-readonly/config.yml"); then
      add_target "gro oauth_token" "$gro_ref" "oauth_token" "gro"
    fi
  fi

  if any_tool_selected atlassian jtk cfl; then
    add_target "jtk/cfl api_token" "atlassian-cli/default" "api_token" "jtk cfl"
  fi

  if tool_selected sfdc; then
    add_target "sfdc oauth_token" "salesforce-cli/default" "oauth_token" "sfdc"
  fi
}

add_cr_targets() {
  if ! tool_selected cr; then
    return
  fi
  ensure_work_tmp
  printf '%s\t%s\t%s\t%s\n' "cr credentials" "codereview" "*" "cr" >>"$targets_file"
}

resolve_apps_for_target() {
  local apps=$1 app path dr expected apps_file
  apps_file=$2
  : >"$apps_file"

  for app in $apps; do
    if ! path=$(command -v "$app" 2>/dev/null); then
      add_note "$app: skipped; binary not found on PATH"
      continue
    fi
    if [ ! -x "$path" ]; then
      add_note "$app: skipped; binary is not executable: $path"
      continue
    fi
    if ! dr=$(codesign -d -r- "$path" 2>&1 | sed -n 's/^designated => //p' | head -n 1); then
      add_note "$app: skipped; could not inspect codesign requirement for $path"
      continue
    fi
    if [ -z "$dr" ]; then
      add_note "$app: skipped; no designated requirement found for $path"
      continue
    fi
    expected="$(expected_dr_for "$app")"
    if [ "$(lower "$dr")" != "$(lower "$expected")" ]; then
      add_note "$app: skipped; unexpected designated requirement for $path"
      add_note "  expected: $expected"
      add_note "  actual:   $dr"
      continue
    fi
    printf '%s\t%s\t%s\n' "$app" "$path" "$dr" >>"$apps_file"
  done
}

classify_item() {
  local dump_file=$1 service=$2 account=$3 apps_file=$4
  perl -Mstrict -Mwarnings -e '
    my ($dump_file, $service, $account, $apps_file) = @ARGV;
    open my $afh, "<", $apps_file or die "open apps file: $!";
    my @apps;
    while (my $line = <$afh>) {
      chomp $line;
      next if $line eq "";
      my ($name, $path, $dr) = split /\t/, $line, 3;
      push @apps, { name => $name, path => $path, dr => lc($dr // "") };
    }
    close $afh;

    open my $dfh, "<", $dump_file or die "open dump file: $!";
    my $block = "";
    my @matching_blocks;

    sub maybe_collect {
      my ($block, $service, $account, $matching_blocks) = @_;
      return if $block eq "";
      return if $block !~ /^class: "genp"$/m;
      return if $block !~ /\Q"svce"<blob>="$service"\E/;
      return if $block !~ /\Q"acct"<blob>="$account"\E/;
      push @$matching_blocks, $block;
    }

    while (my $line = <$dfh>) {
      if ($line =~ /^keychain:/) {
        maybe_collect($block, $service, $account, \@matching_blocks);
        $block = "";
        next;
      }
      $block .= $line;
    }
    maybe_collect($block, $service, $account, \@matching_blocks);
    close $dfh;

    if (@matching_blocks == 0) {
      print "no-item\t\t\n";
      exit 0;
    }
    if (@matching_blocks > 1) {
      print "duplicate-items\t\t\n";
      exit 0;
    }

    my @reqs = map { lc($_) } ($matching_blocks[0] =~ /requirement: ([^\n]*)/g);
    my $has_cdhash = grep { /cdhash h"/ } @reqs;
    my (@present, @missing);
    for my $app (@apps) {
      my $found = grep { $_ eq $app->{dr} } @reqs;
      push @{ $found ? \@present : \@missing }, $app->{name};
    }

    my $state;
    if (@missing == 0) {
      $state = $has_cdhash ? "stable+stale-cdhash" : "stable";
    } elsif ($has_cdhash) {
      $state = @present ? "partial-stable+stale-cdhash" : "cdhash-only";
    } else {
      $state = @present ? "partial-stable" : "missing-current-binary";
    }

    print $state, "\t", join(",", @present), "\t", join(",", @missing), "\n";
  ' "$dump_file" "$service" "$account" "$apps_file"
}

list_accounts_for_service() {
  local dump_file=$1 service=$2
  perl -Mstrict -Mwarnings -e '
    my ($dump_file, $service) = @ARGV;
    open my $dfh, "<", $dump_file or die "open dump file: $!";
    my $block = "";
    my %seen;

    sub maybe_print {
      my ($block, $service, $seen) = @_;
      return if $block eq "";
      return if $block !~ /^class: "genp"$/m;
      return if $block !~ /\Q"svce"<blob>="$service"\E/;
      my ($account) = $block =~ /"acct"<blob>="([^"]*)"/;
      return if !defined $account || $account eq "";
      return if $seen->{$account}++;
      print "$account\n";
    }

    while (my $line = <$dfh>) {
      if ($line =~ /^keychain:/) {
        maybe_print($block, $service, \%seen);
        $block = "";
        next;
      }
      $block .= $line;
    }
    maybe_print($block, $service, \%seen);
  ' "$dump_file" "$service" | sort
}

run_self_test() {
  command -v perl >/dev/null || { echo "perl not found" >&2; exit 2; }

  local tmp dump apps actual expected
  tmp="$(mktemp -d)"
  dump="$tmp/keychain.dump"
  apps="$tmp/apps.tsv"

  {
    printf 'jtk\t/tmp/jtk\t%s\n' "$(expected_dr_for jtk)"
    printf 'cfl\t/tmp/cfl\t%s\n' "$(expected_dr_for cfl)"
  } >"$apps"

  cat >"$dump" <<EOF
keychain: "/tmp/login.keychain-db"
class: "genp"
    "svce"<blob>="atlassian-cli"
    "acct"<blob>="stable/api_token"
        requirement: $(expected_dr_for jtk)
        requirement: $(expected_dr_for cfl)
keychain: "/tmp/login.keychain-db"
class: "genp"
    "svce"<blob>="atlassian-cli"
    "acct"<blob>="mixed/api_token"
        requirement: $(expected_dr_for jtk)
        requirement: $(expected_dr_for cfl)
        requirement: cdhash H"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
keychain: "/tmp/login.keychain-db"
class: "genp"
    "svce"<blob>="atlassian-cli"
    "acct"<blob>="partial/api_token"
        requirement: $(expected_dr_for jtk)
        requirement: cdhash H"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
keychain: "/tmp/login.keychain-db"
class: "genp"
    "svce"<blob>="atlassian-cli"
    "acct"<blob>="old/api_token"
        requirement: cdhash H"cccccccccccccccccccccccccccccccccccccccc"
keychain: "/tmp/login.keychain-db"
class: "genp"
    "svce"<blob>="atlassian-cli"
    "acct"<blob>="missing/api_token"
        requirement: identifier "other.tool" and certificate leaf = H"dddddddddddddddddddddddddddddddddddddddd"
keychain: "/tmp/login.keychain-db"
class: 0x0000000F
    "svce"<blob>="atlassian-cli"
    "acct"<blob>="ignored/api_token"
        requirement: cdhash H"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
keychain: "/tmp/login.keychain-db"
class: "genp"
    "svce"<blob>="other-service"
    "acct"<blob>="old/api_token"
        requirement: cdhash H"ffffffffffffffffffffffffffffffffffffffff"
EOF

  expected=$(printf '%s\n' \
    $'stable\tjtk,cfl\t' \
    $'stable+stale-cdhash\tjtk,cfl\t' \
    $'partial-stable+stale-cdhash\tjtk\tcfl' \
    $'cdhash-only\t\tjtk,cfl' \
    $'missing-current-binary\t\tjtk,cfl' \
    $'no-item\t\t')

  actual=$(cat <<EOF
$(classify_item "$dump" "atlassian-cli" "stable/api_token" "$apps")
$(classify_item "$dump" "atlassian-cli" "mixed/api_token" "$apps")
$(classify_item "$dump" "atlassian-cli" "partial/api_token" "$apps")
$(classify_item "$dump" "atlassian-cli" "old/api_token" "$apps")
$(classify_item "$dump" "atlassian-cli" "missing/api_token" "$apps")
$(classify_item "$dump" "atlassian-cli" "absent/api_token" "$apps")
EOF
)

  if [ "$actual" != "$expected" ]; then
    echo "self-test failed: classifier output mismatch" >&2
    printf 'expected:\n%s\n' "$expected" >&2
    printf 'actual:\n%s\n' "$actual" >&2
    exit 1
  fi

  echo "self-test OK"
  rm -rf "$tmp"
}

build_helper() {
  command -v cc >/dev/null || { echo "cc not found; install Xcode command line tools" >&2; exit 2; }
  ensure_work_tmp
  helper="$work_tmp/kc_acl_add_app"

  cat >"$work_tmp/kc_acl_add_app.c" <<'C'
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdio.h>
#include <string.h>

static void print_status(const char *what, OSStatus st) {
  fprintf(stderr, "%s: %d\n", what, (int)st);
}

static int authorizations_contain(CFArrayRef auths, CFStringRef target) {
  if (auths == NULL) {
    return 0;
  }
  CFRange range = CFRangeMake(0, CFArrayGetCount(auths));
  return CFArrayContainsValue(auths, range, target);
}

int main(int argc, char **argv) {
  if (argc != 5) {
    fprintf(stderr, "usage: kc_acl_add_app <keychain> <service> <account> <app-path>\n");
    return 2;
  }

  const char *kc_path = argv[1];
  const char *service = argv[2];
  const char *account = argv[3];
  const char *app_path = argv[4];

  SecKeychainRef kc = NULL;
  OSStatus st = SecKeychainOpen(kc_path, &kc);
  if (st != errSecSuccess) {
    print_status("SecKeychainOpen", st);
    return 3;
  }

  SecKeychainItemRef item = NULL;
  st = SecKeychainFindGenericPassword(
      kc, (UInt32)strlen(service), service, (UInt32)strlen(account), account,
      NULL, NULL, &item);
  if (st != errSecSuccess) {
    print_status("SecKeychainFindGenericPassword", st);
    CFRelease(kc);
    return 4;
  }

  SecTrustedApplicationRef app = NULL;
  st = SecTrustedApplicationCreateFromPath(app_path, &app);
  if (st != errSecSuccess) {
    print_status("SecTrustedApplicationCreateFromPath", st);
    CFRelease(item);
    CFRelease(kc);
    return 5;
  }

  CFStringRef fallback_label =
      CFStringCreateWithCString(kCFAllocatorDefault, service, kCFStringEncodingUTF8);

  SecAccessRef access = NULL;
  st = SecKeychainItemCopyAccess(item, &access);
  if (st != errSecSuccess) {
    print_status("SecKeychainItemCopyAccess", st);
    CFRelease(fallback_label);
    CFRelease(app);
    CFRelease(item);
    CFRelease(kc);
    return 6;
  }

  CFArrayRef acl_list = NULL;
  st = SecAccessCopyACLList(access, &acl_list);
  if (st != errSecSuccess) {
    print_status("SecAccessCopyACLList", st);
    CFRelease(access);
    CFRelease(fallback_label);
    CFRelease(app);
    CFRelease(item);
    CFRelease(kc);
    return 7;
  }

  int changed = 0;
  CFIndex acl_count = CFArrayGetCount(acl_list);
  for (CFIndex i = 0; i < acl_count; i++) {
    SecACLRef acl = (SecACLRef)CFArrayGetValueAtIndex(acl_list, i);
    CFArrayRef auths = SecACLCopyAuthorizations(acl);
    if (!authorizations_contain(auths, kSecACLAuthorizationDecrypt)) {
      if (auths != NULL) {
        CFRelease(auths);
      }
      continue;
    }

    CFArrayRef existing_apps = NULL;
    CFStringRef description = NULL;
    SecKeychainPromptSelector prompt_selector = 0;
    st = SecACLCopyContents(acl, &existing_apps, &description, &prompt_selector);
    if (st != errSecSuccess) {
      print_status("SecACLCopyContents", st);
      if (auths != NULL) {
        CFRelease(auths);
      }
      CFRelease(acl_list);
      CFRelease(access);
      CFRelease(fallback_label);
      CFRelease(app);
      CFRelease(item);
      CFRelease(kc);
      return 8;
    }

    if (existing_apps == NULL) {
      if (description != NULL) {
        CFRelease(description);
      }
      if (auths != NULL) {
        CFRelease(auths);
      }
      continue;
    }

    CFMutableArrayRef new_apps =
        CFArrayCreateMutableCopy(kCFAllocatorDefault, 0, existing_apps);
    CFArrayAppendValue(new_apps, app);
    st = SecACLSetContents(acl, new_apps,
                           description == NULL ? fallback_label : description,
                           prompt_selector);
    if (st != errSecSuccess) {
      print_status("SecACLSetContents", st);
      CFRelease(new_apps);
      if (description != NULL) {
        CFRelease(description);
      }
      if (existing_apps != NULL) {
        CFRelease(existing_apps);
      }
      if (auths != NULL) {
        CFRelease(auths);
      }
      CFRelease(acl_list);
      CFRelease(access);
      CFRelease(fallback_label);
      CFRelease(app);
      CFRelease(item);
      CFRelease(kc);
      return 9;
    }
    changed = 1;

    CFRelease(new_apps);
    if (description != NULL) {
      CFRelease(description);
    }
    if (existing_apps != NULL) {
      CFRelease(existing_apps);
    }
    if (auths != NULL) {
      CFRelease(auths);
    }
  }

  if (!changed) {
    fprintf(stderr, "no decrypt-capable explicit app-list ACL found for item\n");
    CFRelease(acl_list);
    CFRelease(access);
    CFRelease(fallback_label);
    CFRelease(app);
    CFRelease(item);
    CFRelease(kc);
    return 10;
  }

  st = SecKeychainItemSetAccess(item, access);
  if (st != errSecSuccess) {
    print_status("SecKeychainItemSetAccess", st);
    CFRelease(acl_list);
    CFRelease(access);
    CFRelease(fallback_label);
    CFRelease(app);
    CFRelease(item);
    CFRelease(kc);
    return 11;
  }

  CFRelease(acl_list);
  CFRelease(access);
  CFRelease(fallback_label);
  CFRelease(app);
  CFRelease(item);
  CFRelease(kc);
  return 0;
}
C

  echo "Building temporary ACL helper..." >&2
  cc "$work_tmp/kc_acl_add_app.c" -Wno-deprecated-declarations \
    -framework Security -framework CoreFoundation -o "$helper" >/dev/null
}

queue_repairs_for_missing_apps() {
  local display=$1 service=$2 account=$3 apps_file=$4 missing_csv=$5
  local app path dr missing
  while IFS="$(printf '\t')" read -r app path dr; do
    [ -n "$app" ] || continue
    case ",$missing_csv," in
      *",$app,"*)
        printf '%s\t%s\t%s\t%s\t%s\n' "$display" "$service" "$account" "$app" "$path" >>"$repairs_file"
        ;;
    esac
  done <"$apps_file"
}

queue_rebuild() {
  local display=$1 service=$2 account=$3 apps_file=$4 state=$5
  printf '%s\t%s\t%s\t%s\t%s\n' "$display" "$service" "$account" "$apps_file" "$state" >>"$rebuilds_file"
}

rebuild_item() {
  local display=$1 service=$2 account=$3 apps_file=$4 state=$5
  local app path dr secret hex label comment cmd i
  local app_args=()

  while IFS="$(printf '\t')" read -r app path dr; do
    [ -n "$app" ] || continue
    app_args+=("-T" "$path")
  done <"$apps_file"

  if [ "${#app_args[@]}" -eq 0 ]; then
    echo "ERROR $display ($service/$account): no stable app paths available" >&2
    return 1
  fi

  if ! secret=$(security find-generic-password -s "$service" -a "$account" -w); then
    echo "ERROR $display ($service/$account): failed to read existing secret" >&2
    return 1
  fi
  hex=$(printf '%s' "$secret" | to_hex)
  unset secret
  if [ -z "$hex" ]; then
    echo "ERROR $display ($service/$account): existing secret is empty; refusing to rebuild" >&2
    return 1
  fi

  label="${service} ${account}"
  comment="Credential for ${service} ${account}"

  if ! security delete-generic-password -s "$service" -a "$account" >/dev/null 2>&1; then
    echo "ERROR $display ($service/$account): failed to delete old item" >&2
    return 1
  fi

  cmd="add-generic-password -s $(security_quote "$service") -a $(security_quote "$account")"
  cmd+=" -l $(security_quote "$label") -j $(security_quote "$comment") -D \"application password\" -U"
  i=0
  while [ "$i" -lt "${#app_args[@]}" ]; do
    cmd+=" ${app_args[$i]} $(security_quote "${app_args[$((i + 1))]}")"
    i=$((i + 2))
  done
  cmd+=" -X $hex"

  if ! printf '%s\n' "$cmd" | security -i >/dev/null; then
    echo "ERROR $display ($service/$account): canonical rebuild failed; attempting value restore without app ACL cleanup" >&2
    printf 'add-generic-password -s %s -a %s -l %s -j %s -D "application password" -U -X %s\n' \
      "$(security_quote "$service")" \
      "$(security_quote "$account")" \
      "$(security_quote "$label")" \
      "$(security_quote "$comment")" \
      "$hex" | security -i >/dev/null || true
    unset hex
    return 1
  fi

  unset hex
  printf 'ok (%s)\n' "$state"
  return 0
}

apply=0
action="inspect"
self_test=0
tool_filter=" "
keychain="$HOME/Library/Keychains/login.keychain-db"
work_tmp=""
targets_file=""
notes_file=""
repairs_file=""
rebuilds_file=""
helper=""
trap cleanup EXIT

while [ "$#" -gt 0 ]; do
  case "$1" in
    --heal)
      set_action heal
      shift
      ;;
    --cleanup)
      set_action cleanup
      shift
      ;;
    --rebuild)
      set_action rebuild
      shift
      ;;
    --apply)
      apply=1
      shift
      ;;
    --self-test)
      self_test=1
      shift
      ;;
    --keychain)
      keychain="${2:?--keychain requires a value}"
      shift 2
      ;;
    --tool)
      case "${2:?--tool requires a value}" in
        slck|nrq|gro|jtk|cfl|atlassian|cr|sfdc)
          tool_filter="$tool_filter$2 "
          ;;
        *)
          echo "unknown tool: $2" >&2
          usage >&2
          exit 2
          ;;
      esac
      shift 2
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
done

if [ "$self_test" -eq 1 ]; then
  run_self_test
  exit 0
fi

if [ "$apply" -eq 1 ] && [ "$action" = "inspect" ]; then
  echo "--apply requires exactly one action: --heal, --cleanup, or --rebuild" >&2
  usage >&2
  exit 2
fi

if [ "$(uname -s)" != "Darwin" ]; then
  echo "This helper only supports macOS Keychain on Darwin." >&2
  exit 2
fi

if [ "${EUID:-$(id -u)}" -eq 0 ]; then
  echo "Do not run this with sudo; run it as the user who owns the login Keychain." >&2
  exit 2
fi

command -v codesign >/dev/null || { echo "codesign not found" >&2; exit 2; }
command -v security >/dev/null || { echo "security not found" >&2; exit 2; }
command -v perl >/dev/null || { echo "perl not found" >&2; exit 2; }
[ -f "$keychain" ] || { echo "keychain not found: $keychain" >&2; exit 2; }

ensure_work_tmp
add_static_targets
add_cr_targets

if [ ! -s "$targets_file" ]; then
  echo "No credential targets discovered."
  if [ -s "$notes_file" ]; then
    echo
    echo "Notes:"
    sed 's/^/- /' "$notes_file"
  fi
  exit 0
fi

dump_file="$work_tmp/keychain.dump"
dump_err="$work_tmp/keychain.err"
echo "Scanning Keychain ACL metadata in $keychain ..." >&2
echo "This can take 10-30 seconds on a large login Keychain." >&2
if ! security dump-keychain -a "$keychain" >"$dump_file" 2>"$dump_err"; then
  echo "failed to scan Keychain ACL metadata: $keychain" >&2
  if [ -s "$dump_err" ]; then
    sed 's/^/  /' "$dump_err" >&2
  fi
  exit 1
fi
echo "Scan complete." >&2

if [ "$action" = "inspect" ]; then
  mode_label="inspect"
elif [ "$apply" -eq 1 ]; then
  mode_label="$action apply"
else
  mode_label="$action dry-run"
fi
echo "Mode: $mode_label"
echo
echo "Items:"

process_item() {
  local display=$1 service=$2 account=$3 apps_file=$4
  local classification state rest present missing

  classification=$(classify_item "$dump_file" "$service" "$account" "$apps_file")
  state=${classification%%$'\t'*}
  rest=${classification#*$'\t'}
  present=${rest%%$'\t'*}
  missing=${rest#*$'\t'}
  printf '  %-32s %s (%s/%s)' "$state" "$display" "$service" "$account"
  if [ -n "$present" ]; then
    printf ' trusted=%s' "$present"
  fi
  if [ -n "$missing" ]; then
    printf ' missing=%s' "$missing"
  fi
  printf '\n'

  case "$state" in
    duplicate-items)
      add_note "$display ($service/$account): manual inspection required; state=$state"
      ;;
    no-item)
      ;;
    *)
      case "$action" in
        heal)
          case "$state" in
            cdhash-only|partial-stable|partial-stable+stale-cdhash)
              queue_repairs_for_missing_apps "$display" "$service" "$account" "$apps_file" "$missing"
              ;;
            missing-current-binary)
              add_note "$display ($service/$account): manual inspection required before heal; state=$state"
              ;;
          esac
          ;;
        cleanup)
          case "$state" in
            stable+stale-cdhash)
              queue_rebuild "$display" "$service" "$account" "$apps_file" "$state"
              ;;
            cdhash-only|partial-stable|partial-stable+stale-cdhash|missing-current-binary)
              add_note "$display ($service/$account): not queued for cleanup; run --heal or --rebuild first; state=$state"
              ;;
          esac
          ;;
        rebuild)
          queue_rebuild "$display" "$service" "$account" "$apps_file" "$state"
          ;;
      esac
      ;;
  esac
}

while IFS="$(printf '\t')" read -r display service account apps; do
  [ -n "$display" ] || continue
  target_id=$(printf '%s_%s_%s' "$display" "$service" "$account" | tr -c 'A-Za-z0-9_' '_')
  apps_file="$work_tmp/apps.$target_id.tsv"
  resolve_apps_for_target "$apps" "$apps_file"
  if [ ! -s "$apps_file" ]; then
    printf '  %-32s %s (%s/%s)\n' "skip-no-stable-app" "$display" "$service" "$account"
    continue
  fi

  if [ "$account" = "*" ]; then
    accounts_file="$work_tmp/accounts.$target_id.txt"
    list_accounts_for_service "$dump_file" "$service" >"$accounts_file"
    if [ ! -s "$accounts_file" ]; then
      printf '  %-32s %s (%s/*)\n' "no-item" "$display" "$service"
      continue
    fi
    while IFS= read -r discovered_account; do
      process_item "$display $discovered_account" "$service" "$discovered_account" "$apps_file"
    done <"$accounts_file"
  else
    process_item "$display" "$service" "$account" "$apps_file"
  fi
done <"$targets_file"

repair_count=$(sed '/^$/d' "$repairs_file" | wc -l | tr -d ' ')
rebuild_count=$(sed '/^$/d' "$rebuilds_file" | wc -l | tr -d ' ')

if [ -s "$notes_file" ]; then
  echo
  echo "Notes:"
  sed 's/^/- /' "$notes_file"
fi

echo
case "$action" in
  inspect)
    echo "Inspect only. Use --heal, --cleanup, or --rebuild to preview an action."
    echo "Pair an action with --apply to mutate Keychain ACLs or canonicalize items."
    exit 0
    ;;
  heal)
    if [ "$repair_count" -eq 0 ]; then
      echo "No missing stable app grants need healing."
      echo "stable+stale-cdhash means current stable-signed apps are already trusted;"
      echo "macOS is only still reporting older cdhash grants or partition metadata."
      exit 0
    fi
    if [ "$apply" -ne 1 ]; then
      echo "Dry-run only. Re-run with --heal --apply to add $repair_count missing stable app grant(s)."
      exit 0
    fi
    ;;
  cleanup)
    if [ "$rebuild_count" -eq 0 ]; then
      echo "No already-healed stale-cdhash items need cleanup."
      exit 0
    fi
    if [ "$apply" -ne 1 ]; then
      echo "Dry-run only. Re-run with --cleanup --apply to rebuild $rebuild_count already-healed stale-cdhash item(s)."
      exit 0
    fi
    ;;
  rebuild)
    if [ "$rebuild_count" -eq 0 ]; then
      echo "No present target items need rebuild."
      exit 0
    fi
    if [ "$apply" -ne 1 ]; then
      echo "Dry-run only. Re-run with --rebuild --apply to rebuild $rebuild_count present target item(s)."
      exit 0
    fi
    ;;
esac

if [ "$action" = "heal" ]; then
  echo "Healing $repair_count missing stable app grant(s). macOS may prompt for Keychain authorization."
  build_helper
  failures=0
  while IFS="$(printf '\t')" read -r display service account app path; do
    [ -n "$display" ] || continue
    printf '  healing %s (%s/%s) for %s ... ' "$display" "$service" "$account" "$app"
    if "$helper" "$keychain" "$service" "$account" "$path"; then
      echo "ok"
    else
      echo "failed"
      failures=$((failures + 1))
    fi
  done <"$repairs_file"

  if [ "$failures" -gt 0 ]; then
    echo "Heal completed with $failures failure(s)." >&2
    exit 1
  fi

  echo
  echo "Heal complete. Re-run without --apply to verify all installed stable apps are trusted."
  exit 0
fi

echo "$([ "$action" = "cleanup" ] && echo "Cleaning up" || echo "Rebuilding") $rebuild_count item(s). macOS may prompt for Keychain authorization."
failures=0
while IFS="$(printf '\t')" read -r display service account apps_file state; do
  [ -n "$display" ] || continue
  printf '  %s %s (%s/%s) ... ' "$([ "$action" = "cleanup" ] && echo cleanup || echo rebuild)" "$display" "$service" "$account"
  if rebuild_item "$display" "$service" "$account" "$apps_file" "$state"; then
    :
  else
    failures=$((failures + 1))
  fi
done <"$rebuilds_file"

if [ "$failures" -gt 0 ]; then
  echo "$action completed with $failures failure(s)." >&2
  exit 1
fi

echo
echo "$action complete. Re-run without --apply to verify the resulting ACL state."
