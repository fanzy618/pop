#!/usr/bin/env bash
# Delete all rules whose pattern cannot be used by the current domain-suffix
# matcher (internal/rules). Such rules are leftover "regex / wildcard" entries
# from earlier versions and are silently ignored at runtime.
#
# A pattern is kept iff, after lowercasing + trimming + stripping a single
# leading "*." (legacy) and trailing ".", it:
#   - is non-empty
#   - contains only [a-z0-9.-]  (matcher refuses anything with '*' after the
#     leading "*." strip, and real hostnames live in this char class)
# A single-label pattern like "svc" IS valid: the matcher uses it as a suffix
# rule and will match "foo.svc", "x.y.svc", etc.
# Everything else is considered a regex/garbage rule and deleted.
#
# With --wildcards, also delete legacy "*.foo.com" rules whose stripped form
# duplicates an existing "foo.com" rule with identical enabled/action/upstream
# (such pairs are functional duplicates: the matcher already treats "*.foo.com"
# the same as "foo.com").
#
# Usage:
#   scripts/cleanup-regex-rules.sh                       # dry-run, regex/invalid only
#   scripts/cleanup-regex-rules.sh --wildcards           # dry-run, also list dup wildcards
#   scripts/cleanup-regex-rules.sh --wildcards --apply   # actually delete
#   POP_CONSOLE_BASE=http://host:5080 scripts/cleanup-regex-rules.sh --apply
set -euo pipefail

BASE="${POP_CONSOLE_BASE:-http://127.0.0.1:5080}"
APPLY=0
WILDCARDS=0
for arg in "$@"; do
  case "$arg" in
    --apply|-y)    APPLY=1 ;;
    --wildcards|-w) WILDCARDS=1 ;;
    -h|--help)
      sed -n '2,26p' "$0"; exit 0 ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
command -v jq   >/dev/null || { echo "jq is required"   >&2; exit 1; }

is_domain_pattern() {
  # returns 0 if the pattern is something the matcher can use, 1 otherwise
  local p="$1"
  p="$(printf '%s' "$p" | tr '[:upper:]' '[:lower:]')"
  p="${p#"${p%%[![:space:]]*}"}"   # ltrim
  p="${p%"${p##*[![:space:]]}"}"   # rtrim
  p="${p#\*.}"                     # strip legacy leading *.
  p="${p%.}"                       # strip trailing dot
  [[ -n "$p" ]] || return 1
  [[ "$p" =~ ^[a-z0-9.-]+$ ]] || return 1
  return 0
}

fetch_all_rules() {
  local page=1 size=100 total fetched=0
  : > /tmp/pop-rules.$$.jsonl
  trap 'rm -f /tmp/pop-rules.$$.jsonl' EXIT
  while :; do
    local body
    body="$(curl -fsS "$BASE/api/rules?page=$page&page_size=$size")"
    total="$(jq -r '.total' <<<"$body")"
    jq -c '.items[]' <<<"$body" >> /tmp/pop-rules.$$.jsonl
    fetched=$((fetched + $(jq -r '.items | length' <<<"$body")))
    (( fetched >= total )) && break
    page=$((page + 1))
  done
  cat /tmp/pop-rules.$$.jsonl
}

echo "POP console: $BASE  (mode: $([[ $APPLY -eq 1 ]] && echo APPLY || echo dry-run), wildcards: $([[ $WILDCARDS -eq 1 ]] && echo on || echo off))"

# First pass: collect all rules into a tsv "id\tpattern\tenabled\taction\tupstream_id"
rules_tsv=$(mktemp); plain_tsv=$(mktemp); bad_tsv=$(mktemp)
trap 'rm -f "$rules_tsv" "$plain_tsv" "$bad_tsv"' EXIT
fetch_all_rules \
  | jq -r '[.id, .pattern, (.enabled|tostring), .action, (.upstream_id // 0 | tostring)] | @tsv' \
  > "$rules_tsv"

# Build twin index sorted by pattern: "<pattern>\t<enabled>|<action>|<upstream_id>"
awk -F'\t' 'substr($2,1,2)!="*." { print $2"\t"$3"|"$4"|"$5 }' "$rules_tsv" \
  | sort -k1,1 > "$plain_tsv"

lookup_plain_key() {
  # binary-friendly lookup: awk single-file scan; tiny enough at ~4k rows
  awk -F'\t' -v k="$1" '$1==k {print $2; exit}' "$plain_tsv"
}

: > "$bad_tsv"
while IFS=$'\t' read -r id pattern enabled action upstream; do
  if ! is_domain_pattern "$pattern"; then
    printf '%s\t%s\tregex/invalid\n' "$id" "$pattern" >> "$bad_tsv"
    continue
  fi
  if [[ $WILDCARDS -eq 1 && "$pattern" == \*.* ]]; then
    stripped="${pattern#\*.}"
    twin=$(lookup_plain_key "$stripped")
    if [[ -n "$twin" && "$twin" == "$enabled|$action|$upstream" ]]; then
      printf '%s\t%s\tdup of %s\n' "$id" "$pattern" "$stripped" >> "$bad_tsv"
    fi
  fi
done < "$rules_tsv"

bad_count=$(wc -l < "$bad_tsv" | tr -d ' ')
# show first 30 then a summary
head -30 "$bad_tsv" | awk -F'\t' '{ printf "  candidate id=%s  pattern=%-30s (%s)\n", $1, $2, $3 }'
[[ "$bad_count" -gt 30 ]] && echo "  ... and $((bad_count - 30)) more"
echo "found $bad_count rule(s) to delete"

bad_ids=()
while IFS=$'\t' read -r id _; do
  bad_ids+=("$id")
done < "$bad_tsv"

if [[ $APPLY -ne 1 || ${#bad_ids[@]} -eq 0 ]]; then
  [[ $APPLY -ne 1 && ${#bad_ids[@]} -gt 0 ]] && echo "re-run with --apply to delete."
  exit 0
fi

deleted=0 failed=0
for id in "${bad_ids[@]}"; do
  if curl -fsS -X DELETE "$BASE/api/rules/$id" >/dev/null; then
    deleted=$((deleted + 1))
  else
    failed=$((failed + 1))
    echo "  failed to delete id=$id" >&2
  fi
done
echo "deleted=$deleted failed=$failed"
exit $(( failed > 0 ? 1 : 0 ))
