#!/usr/bin/env bash
# Подпись выпуска cream — та же схема, что у релиза форка Teleport (ключ
# stvor-platform, cosign своим ключом ECDSA P-256, без Rekor и без сети).
# Запускается на раннере CI (job release-sign) и в самопроверке на PR.
#
#   buildinfo DIR TAG COMMIT GO EPOCH
#       DIR/BUILDINFO.json (тег, коммит, версия Go, время сборки = время коммита
#       тега, суммы бинарей внутри тарболов) и DIR/SHA256SUMS заново — по всем
#       .deb, .tar.gz и BUILDINFO.json. Детерминировано: повторная сборка тега
#       даёт те же байты, подпись не перевыпускается.
#   sign DIR
#       DIR/SHA256SUMS.sig (base64 ASN.1 ECDSA над sha256 — проверяется stdlib и
#       openssl dgst -verify) и DIR/SHA256SUMS.bundle.json (cosign verify-blob).
#       Окружение: COSIGN (путь к бинарю), COSIGN_PRIVATE_KEY, COSIGN_PASSWORD.
#   verify DIR PUB
#       Обе подписи — cosign verify-blob без сети и DNS (своё сетевое
#       пространство имён) и openssl dgst -verify.
#   source DIR TAG
#       Тег подписывается, только если его коммит — предок origin/main, а
#       DIR/BUILDINFO.json собран из этого коммита и для этого тега (окружение
#       release проверяет ref запуска, а не тег). Запуск — в клоне с полной
#       историей (origin/main и теги).
#   selftest
#       Одноразовый ключ: buildinfo → sign → verify, подделка SHA256SUMS и
#       отсутствие секрета дают отказ; ключи release/keys/*.pub — ECDSA P-256.
set -euo pipefail

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

die() {
	echo "release-sign: $*" >&2
	exit 1
}

need_cosign() {
	[ -n "${COSIGN:-}" ] && [ -x "$COSIGN" ] || die "нет бинаря cosign (COSIGN='${COSIGN:-}')"
}

# Без сети: unshare -rn (непривилегированное пространство имён); где его
# запрещает AppArmor (ubuntu-24.04) — sudo unshare -n. Иначе отказ: проверка
# «без сети» не подменяется проверкой с сетью.
offline() {
	if unshare -rn true 2>/dev/null; then
		unshare -rn "$@"
	elif sudo -n unshare -n true 2>/dev/null; then
		sudo -n unshare -n "$@"
	else
		die "не создать сетевое пространство имён (unshare -rn / sudo unshare -n) — проверка без сети невозможна"
	fi
}

cmd_buildinfo() {
	local dir="$1" tag="$2" commit="$3" go="$4" epoch="$5"
	local version="${tag#v}" t arch tmp bins='{}'
	[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "тег должен быть vX.Y.Z: '$tag'"
	[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || die "коммит — 40 hex: '$commit'"
	[[ "$go" =~ ^go[0-9]+\.[0-9]+(\.[0-9]+)?$ ]] || die "версия Go вида go1.N.M: '$go'"
	[[ "$epoch" =~ ^[0-9]+$ ]] || die "SOURCE_DATE_EPOCH — число: '$epoch'"
	tmp="$(mktemp -d -p "$WORK")"
	for arch in amd64 arm64; do
		t="$dir/cream_${version}_linux_${arch}.tar.gz"
		[ -s "$t" ] || die "нет $t"
		mkdir -p "$tmp/$arch"
		tar -xzf "$t" -C "$tmp/$arch" cream
		# Бинарь собран той версией Go, что записана в BUILDINFO.
		grep -aqF "$go" "$tmp/$arch/cream" || die "в $arch/cream нет строки $go — собран другой версией Go"
		bins="$(jq -c --arg a "$arch" --arg s "$(sha256sum "$tmp/$arch/cream" | cut -d' ' -f1)" \
			'. + {($a): {cream: $s}}' <<<"$bins")"
	done
	jq -n --arg tag "$tag" --arg commit "$commit" --arg version "$version" --arg go "$go" \
		--argjson epoch "$epoch" --arg time "$(date -u -d "@$epoch" +%Y-%m-%dT%H:%M:%SZ)" \
		--argjson binaries "$bins" \
		'{tag:$tag, commit:$commit, version:$version, go:$go, reproducible:true,
		  build_time:$time, source_date_epoch:$epoch,
		  build:"make dist-release VERSION=\($tag) SOURCE_DATE_EPOCH=\($epoch)",
		  binaries:$binaries}' >"$dir/BUILDINFO.json"
	(cd "$dir" && sha256sum -- *.deb *.tar.gz BUILDINFO.json >SHA256SUMS && sha256sum -c --quiet SHA256SUMS)
}

cmd_sign() {
	local dir="$1" k
	need_cosign
	[ -s "$dir/SHA256SUMS" ] || die "нет $dir/SHA256SUMS"
	[ -n "${COSIGN_PRIVATE_KEY:-}" ] || die "нет секрета COSIGN_PRIVATE_KEY (environment release) — выпуск без подписи не делается"
	[ -n "${COSIGN_PASSWORD:-}" ] || die "нет секрета COSIGN_PASSWORD (environment release) — выпуск без подписи не делается"
	k="$WORK/cosign.key"
	(umask 077 && printf '%s\n' "$COSIGN_PRIVATE_KEY" >"$k")
	rm -f "$dir/SHA256SUMS.sig" "$dir/SHA256SUMS.bundle.json"
	"$COSIGN" sign-blob --yes --key "$k" \
		--use-signing-config=false --tlog-upload=false --new-bundle-format=false \
		--output-signature "$dir/SHA256SUMS.sig" "$dir/SHA256SUMS"
	"$COSIGN" sign-blob --yes --key "$k" \
		--use-signing-config=false --tlog-upload=false \
		--bundle "$dir/SHA256SUMS.bundle.json" "$dir/SHA256SUMS"
	rm -f "$k"
}

cmd_verify() {
	local dir="$1" pub="$2" der
	need_cosign
	[ -s "$pub" ] || die "нет публичного ключа $pub"
	offline "$COSIGN" verify-blob --key "$pub" --insecure-ignore-tlog=true \
		--signature "$dir/SHA256SUMS.sig" "$dir/SHA256SUMS"
	offline "$COSIGN" verify-blob --key "$pub" --insecure-ignore-tlog=true \
		--bundle "$dir/SHA256SUMS.bundle.json" "$dir/SHA256SUMS"
	der="$(mktemp -p "$WORK")"
	base64 -d "$dir/SHA256SUMS.sig" >"$der"
	openssl dgst -sha256 -verify "$pub" -signature "$der" "$dir/SHA256SUMS"
}

cmd_source() {
	local dir="$1" tag="$2" commit
	[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "тег должен быть vX.Y.Z: '$tag'"
	commit="$(git rev-parse --verify -q "refs/tags/$tag^{commit}")" || die "нет тега $tag"
	git rev-parse --verify -q refs/remotes/origin/main >/dev/null || die "нет origin/main — нужен клон с полной историей"
	git merge-base --is-ancestor "$commit" refs/remotes/origin/main ||
		die "коммит тега $tag ($commit) не предок origin/main — не подписывается"
	[ -s "$dir/BUILDINFO.json" ] || die "нет $dir/BUILDINFO.json"
	jq -e --arg c "$commit" --arg t "$tag" '.commit == $c and .tag == $t' "$dir/BUILDINFO.json" >/dev/null ||
		die "BUILDINFO.json не от тега $tag ($commit): $(jq -c '{tag, commit}' "$dir/BUILDINFO.json")"
	(cd "$dir" && sha256sum -c --quiet SHA256SUMS) || die "ассеты не совпали с SHA256SUMS"
	grep -q '  BUILDINFO.json$' "$dir/SHA256SUMS" || die "BUILDINFO.json не покрыт SHA256SUMS"
	echo "release-sign: $tag = $commit, предок origin/main, BUILDINFO.json сходится"
}

cmd_selftest() {
	local root t d go commit
	need_cosign
	root="$(cd "$(dirname "$0")/.." && pwd)"
	for k in "$root"/release/keys/stvor-platform.pub "$root"/release/keys/stvor-platform-next.pub; do
		openssl pkey -pubin -in "$k" -noout -text | grep -q 'NIST CURVE: P-256' ||
			die "$k — не публичный ключ ECDSA P-256"
	done
	t="$(mktemp -d -p "$WORK")"
	d="$t/release" go="go1.0.0" commit="$(printf '%040d' 0)"
	mkdir -p "$d" "$t/tree"
	for a in amd64 arm64; do
		printf 'fake cream %s built with %s\n' "$a" "$go" >"$t/tree/cream"
		tar -czf "$d/cream_9.9.9_linux_${a}.tar.gz" -C "$t/tree" cream
		printf 'deb %s\n' "$a" >"$d/cream_9.9.9_${a}.deb"
	done
	cmd_buildinfo "$d" v9.9.9 "$commit" "$go" 1700000000
	[ "$(wc -l <"$d/SHA256SUMS")" -eq 5 ] || die "selftest: в SHA256SUMS не 5 строк"
	jq -e '.build_time == "2023-11-14T22:13:20Z" and (.binaries.arm64.cream | length == 64)' \
		"$d/BUILDINFO.json" >/dev/null || die "selftest: BUILDINFO.json не тот"
	cp "$d/BUILDINFO.json" "$t/bi1"
	cmd_buildinfo "$d" v9.9.9 "$commit" "$go" 1700000000
	cmp -s "$t/bi1" "$d/BUILDINFO.json" || die "selftest: BUILDINFO.json не детерминирован"

	(cd "$t" && COSIGN_PASSWORD=selftest "$COSIGN" generate-key-pair >/dev/null)
	if (COSIGN_PRIVATE_KEY='' COSIGN_PASSWORD='' cmd_sign "$d") 2>/dev/null; then
		die "selftest: подпись без секрета не отказала"
	fi
	COSIGN_PRIVATE_KEY="$(cat "$t/cosign.key")" COSIGN_PASSWORD=selftest cmd_sign "$d"
	cmd_verify "$d" "$t/cosign.pub"
	# Чужой ключ — отказ.
	if (cmd_verify "$d" "$root/release/keys/stvor-platform.pub") >/dev/null 2>&1; then
		die "selftest: подпись одноразовым ключом прошла проверку ключом stvor-platform"
	fi
	# Подделка SHA256SUMS — отказ.
	echo "0000  extra" >>"$d/SHA256SUMS"
	if (cmd_verify "$d" "$t/cosign.pub") >/dev/null 2>&1; then
		die "selftest: изменённый SHA256SUMS прошёл проверку"
	fi
	# source: тег на main — да; тег вне main и чужой BUILDINFO — отказ.
	cmd_buildinfo "$d" v9.9.9 "$commit" "$go" 1700000000
	local g="$t/git"
	git init -q -b main "$g"
	git -C "$g" -c user.name=t -c user.email=t@t commit -q --allow-empty -m main
	git -C "$g" tag v9.9.9
	git -C "$g" update-ref refs/remotes/origin/main HEAD
	git -C "$g" checkout -q -b side
	git -C "$g" -c user.name=t -c user.email=t@t commit -q --allow-empty -m side
	git -C "$g" tag v9.9.8
	if (cd "$g" && cmd_source "$d" v9.9.9) >/dev/null 2>&1; then
		die "selftest: BUILDINFO.json от другого коммита прошёл source"
	fi
	cmd_buildinfo "$d" v9.9.9 "$(git -C "$g" rev-parse v9.9.9)" "$go" 1700000000
	(cd "$g" && cmd_source "$d" v9.9.9) >/dev/null || die "selftest: тег на main не прошёл source"
	for a in amd64 arm64; do cp "$d/cream_9.9.9_linux_${a}.tar.gz" "$d/cream_9.9.8_linux_${a}.tar.gz"; done
	cmd_buildinfo "$d" v9.9.8 "$(git -C "$g" rev-parse v9.9.8)" "$go" 1700000000
	if (cd "$g" && cmd_source "$d" v9.9.8) >/dev/null 2>&1; then
		die "selftest: тег вне main прошёл source"
	fi
	echo "release-sign selftest: OK"
}

case "${1:-}" in
buildinfo) [ $# -eq 6 ] || die "usage: buildinfo DIR TAG COMMIT GO EPOCH"; cmd_buildinfo "$2" "$3" "$4" "$5" "$6" ;;
sign) [ $# -eq 2 ] || die "usage: sign DIR"; cmd_sign "$2" ;;
verify) [ $# -eq 3 ] || die "usage: verify DIR PUB"; cmd_verify "$2" "$3" ;;
source) [ $# -eq 3 ] || die "usage: source DIR TAG"; cmd_source "$2" "$3" ;;
selftest) cmd_selftest ;;
*) die "usage: release-sign.sh buildinfo|source|sign|verify|selftest …" ;;
esac
