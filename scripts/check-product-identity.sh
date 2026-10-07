#!/usr/bin/env bash
# Fail on unreviewed uses of the old product name in tracked files.
#
#   scripts/check-product-identity.sh --runtime-only
#   scripts/check-product-identity.sh
#
# The product is Kwakore (D-08), with no compatibility aliases. The old name
# may still appear only where it means something other than this product,
# and each such place is reviewed here, file by file, with a reason:
#
#   font        the typeface the napp ui kit embeds and names in CSS (the
#               old name is also a font family, and that font stays)
#   fixture     test napplet content under backend/testdata, and the ids the
#               dev fixture tests read back from it
#   historical  notes on dropped vendored patches, regression tests that
#               assert the old identity is refused, never written and never
#               migrated, and user docs stating that nothing is migrated
#
# --runtime-only scans the tracked runtime sources: backend/, desktop/,
# scripts/, packaging/ and the justfile. Without it the scan covers every
# tracked file outside .planning/ (archived planning history is never
# scanned or rewritten): the runtime sources plus Nix, CI, the user and
# contributor docs and the spec audit. Both modes must pass. The full list of
# reviewed matches and renamed paths is recorded in
# .planning/phases/09-linux-packaging-rename-and-cleanup/09-RENAME-INVENTORY.md.
#
# Each match is checked case-insensitively, in file contents and in tracked
# path names. A line passes only when its file has an allowlist entry whose
# pattern covers every occurrence on that line: the covered text is cut out
# and the rest must be clean, so an allowed line cannot carry an extra,
# unreviewed use. Binary files pass only through an explicit binary entry.
# An entry that matches nothing in the scanned files fails the scan too, so
# the allowlist cannot outlive what it reviewed.
#
# Exit status: 0 when every match is reviewed, 1 on any unreviewed match or
# stale entry, 2 on usage or git errors.

set -euo pipefail

usage() {
	sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'
}

mode=full
case "${1-}" in
--runtime-only) mode=runtime ;;
"") ;;
-h | --help)
	usage
	exit 0
	;;
*)
	usage >&2
	exit 2
	;;
esac
if [ "$#" -gt 1 ]; then
	usage >&2
	exit 2
fi

root=$(git rev-parse --show-toplevel) || exit 2
cd "$root"

# Spelled in two pieces, and the allowlist patterns bracket their first
# letter, so this file never matches its own scan.
old='verd''ana'

runtime_paths=(backend desktop scripts packaging justfile)
if [ "$mode" = runtime ]; then
	pathspecs=("${runtime_paths[@]}")
else
	pathspecs=(. ':(exclude).planning')
fi

# ─── allowlist ──────────────────────────────────────────────────
# path|kind|pattern|reason
#
# pattern is a case-sensitive POSIX ERE that must cover the old name where
# it appears on a matching line; BINARY allows a binary file as a whole.
allow=(
	# the typeface: the launcher's font, which napp-ui.css sets and embed.go
	# inlines as @font-face
	'backend/webview/embed.go|font|own face — [V]erdana, in the three faces|comment naming the embedded typeface'
	"backend/webview/embed.go|font|the launcher's [V]erdana\$|comment naming the embedded typeface"
	'backend/webview/embed.go|font|@font-face\{font-family:[V]erdana;|the @font-face family name the kit CSS selects'
	'backend/webview/napp-ui.css|font|said loudly: [V]erdana \(the very face|comment naming the kit typeface'
	'backend/webview/napp-ui.css|font|falls back on\. [V]erdana is wide|comment on the typeface metrics'
	'backend/webview/napp-ui.css|font|font-family: [V]erdana, "DejaVu Sans"|the kit font stack'
	'backend/webview/napp-ui.css|font|Type: [V]erdana runs large|comment on the typeface metrics'
	'justfile|font|# [V]erdana, in three faces\) as woff2|comment on the fonts recipe sources'
	'env.d.ts|font|own face — [V]erdana, in three faces, inlined|ui kit contract comment naming the embedded typeface'
	'desktop/assets/v.TTF|font|BINARY|the typeface file, regular; its name table carries the family name'
	'desktop/assets/vb.ttf|font|BINARY|the typeface file, bold; its name table carries the family name'
	'desktop/assets/vi.ttf|font|BINARY|the typeface file, italic; its name table carries the family name'

	# test napplets: their content is data the launcher loads, not product
	# identity, and the dev tests assert the ids read from metadata.json
	'backend/testdata/probe-napplet/metadata.json|fixture|"id": "[v]erdana-probe"|probe fixture id'
	'backend/testdata/probe-napplet/metadata.json|fixture|"title": "[V]erdana probe"|probe fixture title'
	'backend/testdata/probe-napplet/index.html|fixture|<title>[V]erdana probe</title>|probe fixture page title'
	'backend/testdata/probe-napplet/index.html|fixture|"[v]erdana-probe/ping"|probe fixture inter-napplet topic'
	'backend/testdata/probe-napplet/index.html|fixture|title: "[V]erdana probe"|probe fixture notification title'
	'backend/testdata/adversarial-napplet/metadata.json|fixture|"id": "[v]erdana-adversarial"|adversarial fixture id'
	'backend/testdata/adversarial-napplet/metadata.json|fixture|"title": "[V]erdana adversarial"|adversarial fixture title'
	'backend/testdata/adversarial-napplet/index.html|fixture|<title>[V]erdana adversarial</title>|adversarial fixture page title'
	'backend/dev_probe_test.go|fixture|dev~[v]erdana-probe|the dev id derived from the probe fixture metadata.json'
	'backend/dev_adversarial_test.go|fixture|dev~[v]erdana-adversarial|the dev id derived from the adversarial fixture metadata.json'

	# history and old-identity guards
	'backend/webview/shim/README.md|historical|with no [V]erdana patches|vendoring note: the shim carries none of the old patches'
	'backend/webview/shim/README.md|historical|The six [V]erdana patches of the former `0\.30\.0\+[v]erdana\.2` build|vendoring note naming the dropped patched build'
	'backend/webview/shim/README.md|historical|NAP-CONFIG `schemaError` correlation, are dropped\. Where [V]erdana still needs|vendoring note on where the dropped patch behavior moved'
	'backend/linuxhost/host_linux_test.go|historical|[V]ERDANA_|asserts the host writes no old child environment key (no aliases)'
	'backend/desktopentry/entry_linux_test.go|historical|"com\.[v]erdana\.napp\.0123456789abcdef\.desktop"|asserts an old product desktop entry is left alone (no migration)'
	'backend/netguard/link_test.go|historical|"[v]erdana://x"|asserts the old launcher scheme stays rejected'
	'spec/CONFORMANCE.md|historical|patched shim build \(`0\.30\.0\+[v]erdana\.2`\)|audit evidence: the version string of the dropped patched shim build'
	'spec/CONFORMANCE.md|historical|marked `// [v]erdana:` in the old build|audit evidence: the comment marker the dropped patch carried'
	'docs/service.md|historical|anything from older [V]erdana installations|user doc: removal never touches the old product (no migration, D-08)'
	'docs/service.md|historical|Nothing is migrated from [V]erdana:|user doc: no compatibility names or migration (D-08)'
)
# ────────────────────────────────────────────────────────────────

declare -a a_path a_kind a_re a_used
for i in "${!allow[@]}"; do
	IFS='|' read -r p k re _reason <<<"${allow[$i]}"
	a_path[i]=$p
	a_kind[i]=$k
	a_re[i]=$re
	a_used[i]=0
done

in_scope() {
	local p=$1 r
	[ "$mode" = full ] && { [[ $p != .planning/* ]]; return; }
	for r in "${runtime_paths[@]}"; do
		[[ $p == "$r" || $p == "$r"/* ]] && return 0
	done
	return 1
}

grep_tracked() {
	local rc=0 out
	out=$(git grep "$@" -i -e "$old" -- "${pathspecs[@]}") || rc=$?
	if [ "$rc" -gt 1 ]; then
		echo "check-product-identity: git grep failed (exit $rc)" >&2
		exit 2
	fi
	[ -n "$out" ] && printf '%s\n' "$out"
	return 0
}

bad=0
reviewed=0
declare -A per_kind=([font]=0 [fixture]=0 [historical]=0)

report() {
	printf 'unreviewed: %s\n' "$1"
	bad=$((bad + 1))
}

# path names
while IFS= read -r p; do
	[ -n "$p" ] || continue
	shopt -s nocasematch
	if [[ $p == *"$old"* ]]; then
		report "$p: tracked path name"
	fi
	shopt -u nocasematch
done < <(git ls-files -- "${pathspecs[@]}")

# text lines
while IFS= read -r hit; do
	[ -n "$hit" ] || continue
	path=${hit%%:*}
	rest=${hit#*:}
	lineno=${rest%%:*}
	line=${rest#*:}
	covered=
	for i in "${!a_path[@]}"; do
		[ "${a_path[i]}" = "$path" ] || continue
		[ "${a_re[i]}" != BINARY ] || continue
		re=${a_re[i]}
		left=$line
		hit_here=0
		while [[ $left =~ $re ]]; do
			[ -n "${BASH_REMATCH[0]}" ] || break
			left=${left/"${BASH_REMATCH[0]}"/}
			hit_here=1
		done
		if [ "$hit_here" = 1 ]; then
			a_used[i]=1
			line=$left
			covered=${a_kind[i]}
		fi
	done
	shopt -s nocasematch
	if [[ $line == *"$old"* ]] || [ -z "$covered" ]; then
		shopt -u nocasematch
		report "$path:$lineno: ${rest#*:}"
		continue
	fi
	shopt -u nocasematch
	reviewed=$((reviewed + 1))
	per_kind[$covered]=$((per_kind[$covered] + 1))
done < <(grep_tracked -n -I)

# binary files: listed without -I but not with it
declare -A text_files=()
while IFS= read -r p; do
	[ -n "$p" ] && text_files[$p]=1
done < <(grep_tracked -l -I)
while IFS= read -r p; do
	[ -n "$p" ] || continue
	[ -z "${text_files[$p]-}" ] || continue
	ok=0
	for i in "${!a_path[@]}"; do
		if [ "${a_path[i]}" = "$p" ] && [ "${a_re[i]}" = BINARY ]; then
			a_used[i]=1
			ok=1
			per_kind[${a_kind[i]}]=$((per_kind[${a_kind[i]}] + 1))
		fi
	done
	if [ "$ok" = 1 ]; then
		reviewed=$((reviewed + 1))
	else
		report "$p: binary file"
	fi
done < <(grep_tracked -l)

# stale entries, for files inside the scanned set
for i in "${!a_path[@]}"; do
	in_scope "${a_path[i]}" || continue
	if [ "${a_used[i]}" = 0 ]; then
		printf 'stale allowlist entry: %s %s %s\n' "${a_path[i]}" "${a_kind[i]}" "${a_re[i]}"
		bad=$((bad + 1))
	fi
done

if [ "$bad" -gt 0 ]; then
	printf 'FAIL product identity (%s): %d unreviewed or stale, %d reviewed\n' "$mode" "$bad" "$reviewed"
	exit 1
fi
printf 'PASS product identity (%s): %d reviewed (font %d, fixture %d, historical %d), 0 unreviewed\n' \
	"$mode" "$reviewed" "${per_kind[font]}" "${per_kind[fixture]}" "${per_kind[historical]}"
