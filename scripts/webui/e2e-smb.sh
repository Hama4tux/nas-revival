#!/usr/bin/env bash
# End-to-end check of SMB (W2.5) on the deployed NAS with a real SMB client: builds backend/cmd/e2e-smb for
# ARMv5, runs it on the NAS as nas-revival (ssh host alias, default nsa310) to create throw-away accounts
# nrt-ma/mu/mr/mn and shares nrt-smb/nrt-smbh and enable SMB, then uses smbclient from this workstation
# (read/write/deny, guest, wrong password, SMB1 and SMB2 refused, hidden and read-only share, recycle bin,
# session list) and finally restores the previous SMB settings and removes everything.
# Passwords are random, passed over ssh stdin and smbclient auth files (0600), never printed.
# Needs smbclient on the workstation. Exit code 0 = all checks passed.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }
command -v smbclient >/dev/null || { echo "smbclient not found" >&2; exit 1; }
addr="$(ssh -G "$host" | awk '$1 == "hostname" { print $2 }')"

tmp="$(mktemp -d)"
chmod 0700 "$tmp"
fails=0
nas() { ssh "$host" "runuser -u nas-revival -- /tmp/nr-e2e-smb $1"; }
cleanup() {
	nas cleanup </dev/null || true
	ssh "$host" 'rm -f /tmp/nr-e2e-smb'
	rm -rf "$tmp"
}
trap cleanup EXIT

(cd "$repo/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -trimpath -o "$tmp/e2e-smb" ./cmd/e2e-smb)
scp -q "$tmp/e2e-smb" "$host:/tmp/nr-e2e-smb"
ssh "$host" 'chmod 0755 /tmp/nr-e2e-smb'

declare -A pw
for u in nrt-ma nrt-mu nrt-mr nrt-mn; do
	pw[$u]="$(head -c 18 /dev/urandom | base64 | tr '+/' '-_')"
	printf 'username = %s\npassword = %s\n' "$u" "${pw[$u]}" >"$tmp/auth-$u"
done
printf 'username = nrt-mu\npassword = wrong-password-123\n' >"$tmp/auth-wrong"
chmod 0600 "$tmp"/auth-*

printf '%s\n' "${pw[nrt-ma]}" "${pw[nrt-mu]}" "${pw[nrt-mr]}" "${pw[nrt-mn]}" | nas setup || fails=$((fails + 1))

# smbclient: $1 auth, $2 share, $3 commands, rest extra options. Prints the NT_STATUS or "OK".
smb() {
	local auth="$1" sh="$2" cmd="$3"
	shift 3
	local out
	if out="$(cd "$tmp" && smbclient "//$addr/$sh" -A "$tmp/auth-$auth" -m SMB3 "$@" -c "$cmd" 2>&1)"; then
		if grep -q NT_STATUS <<<"$out"; then grep -o 'NT_STATUS_[A-Z_]*' <<<"$out" | head -1; else echo OK; fi
	else
		grep -o 'NT_STATUS_[A-Z_]*' <<<"$out" | head -1 || echo FAILED
	fi
}
refused() {
	if [ "$2" != OK ]; then echo "ok   $1 $2"; else echo "FAIL $1: connected"; fails=$((fails + 1)); fi
}
expect() {
	if [ "$2" = "$3" ]; then echo "ok   $1 $2"; else echo "FAIL $1: got $2, want $3"; fails=$((fails + 1)); fi
}

echo "upload" >"$tmp/e2e-upload.txt"
echo "delete me" >"$tmp/e2e-delete.txt"
expect "rw user uploads" "$(smb nrt-mu nrt-smb 'put e2e-upload.txt; put e2e-delete.txt; mkdir e2e-dir; rmdir e2e-dir')" OK
expect "rw user deletes (to recycle bin)" "$(smb nrt-mu nrt-smb 'del e2e-delete.txt')" OK
expect "ro user lists" "$(smb nrt-mr nrt-smb 'ls')" OK
expect "ro user cannot write" "$(smb nrt-mr nrt-smb 'put e2e-upload.txt ro.txt')" NT_STATUS_ACCESS_DENIED
expect "denied user cannot connect" "$(smb nrt-mn nrt-smb 'ls')" NT_STATUS_ACCESS_DENIED
expect "wrong password" "$(smb wrong nrt-smb 'ls')" NT_STATUS_LOGON_FAILURE
guest="$(smbclient "//$addr/nrt-smb" -N -m SMB3 -c ls 2>&1 | grep -o 'NT_STATUS_[A-Z_]*' | head -1 || true)"
expect "guest refused" "${guest:-OK}" NT_STATUS_ACCESS_DENIED
refused "SMB1 refused" "$(smb nrt-mu nrt-smb 'ls' -m NT1 --option='client min protocol=NT1' --option='client max protocol=NT1')"
refused "SMB 2.1 refused (minimum 3.0)" "$(smb nrt-mu nrt-smb 'ls' -m SMB2 --option='client max protocol=SMB2_10')"
expect "hidden share readable" "$(smb nrt-mu nrt-smbh 'ls')" OK
expect "read-only share refuses write" "$(smb nrt-mu nrt-smbh 'put e2e-upload.txt')" NT_STATUS_ACCESS_DENIED
list="$(smbclient -L "//$addr" -A "$tmp/auth-nrt-mu" -m SMB3 2>/dev/null || true)"
expect "share listed" "$(awk '$1 == "nrt-smb"' <<<"$list" | wc -l)" 1
expect "hidden share not listed" "$(awk '$1 == "nrt-smbh"' <<<"$list" | wc -l)" 0

# Keep a session and an open file while the web API lists them.
(cd "$tmp" && { echo 'open e2e-upload.txt'; sleep 15; echo exit; } | smbclient "//$addr/nrt-smb" -A "$tmp/auth-nrt-mu" -m SMB3 >/dev/null 2>&1) &
sleep 4
printf '%s\n' "${pw[nrt-ma]}" | nas status || fails=$((fails + 1))
wait
ssh "$host" '/tmp/nr-e2e-smb verify' </dev/null || fails=$((fails + 1))

if [ "$fails" -eq 0 ]; then echo "all checks passed"; else echo "$fails check(s) failed"; exit 1; fi
