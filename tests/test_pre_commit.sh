#!/usr/bin/env bash
# Tests scripts/git-hooks/pre-commit in a throwaway repository.
# Run: bash tests/test_pre_commit.sh
set -uo pipefail

hook=$(cd "$(dirname "$0")/.." && pwd)/scripts/git-hooks/pre-commit
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

printf '192\\.0\\.2\\.77\nsecret-server-name\n' >"$work/patterns"
export NAS_REVIVAL_PRIVATE_PATTERNS=$work/patterns
fail=0

# expect <refuse|allow> <description> <path> <content>
expect() {
  local want=$1 desc=$2 path=$3 content=$4 repo
  repo=$(mktemp -d "$work/repo.XXXX")
  git -C "$repo" init -q
  mkdir -p "$repo/$(dirname "$path")"
  printf '%b' "$content" >"$repo/$path"
  git -C "$repo" add -f -- "$path"
  if (cd "$repo" && "$hook" 2>/dev/null); then got=allow; else got=refuse; fi
  if [[ $got == "$want" ]]; then echo "ok   $desc"; else echo "FAIL $desc (want $want, got $got)"; fail=1; fi
}

body=$(head -c 300 /dev/zero | tr '\0' 'A')
me=$(id -un)

expect refuse "ssh private key file"        ".ssh/id_ed25519"         "x\n"
expect refuse "id_rsa anywhere"             "conf/id_rsa"             "x\n"
expect refuse "known_hosts"                 "etc/known_hosts"         "host ssh-ed25519 AAAA\n"
expect refuse "authorized_keys"             "root/authorized_keys"    "ssh-ed25519 AAAA\n"
expect refuse ".env file"                   "backend/.env"            "TOKEN=1\n"
expect refuse "secret.env"                  "secret.env"              "TOKEN=1\n"
expect refuse "jobs.conf"                   "etc/nas-pull/jobs.conf"  "srv root@host x\n"
expect refuse "device dump"                 "mtd0.raw"                "x\n"
expect refuse "real private key in code"    "a/b.go"                  "k := \`-----BEGIN OPENSSH PRIVATE KEY-----\n$body\n-----END OPENSSH PRIVATE KEY-----\`\n"
expect refuse "own home path"               "docs/x.md"               "see /home/$me/notes\n"
expect refuse "owner pattern: subnet"       "docs/x.md"               "server at 192.0.2.77\n"
expect refuse "owner pattern: name"         "docs/x.md"               "pull from secret-server-name\n"
expect allow  "private key stub in a test"  "keys_test.go"            "\"-----BEGIN OPENSSH PRIVATE KEY-----\\\\nb3Bl\\\\n-----END OPENSSH PRIVATE KEY-----\"\n"
expect allow  ".env.example"                "backend/.env.example"    "TOKEN=\n"
expect allow  "generic paths and IPs"       "a/nfs_test.go"           "/home/debian 192.168.1.20 nas.home\n"
expect allow  "ordinary code"               "a/main.go"               "package main\n"

exit $fail
