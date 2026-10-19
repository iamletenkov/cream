#!/bin/sh
# Проверка состава выпуска (спека §5, §15). Запускается в контейнере debian
# из корня репозитория: sh scripts/check-deb.sh deb|tarball VERSION ARCH DIR.
#   deb      — DIR/cream_<v>_<arch>.deb: точный список файлов, режимы, владелец
#              root, поля control (Architecture, нет Depends, Suggests),
#              сценарии сопровождающего, юнит = internal/systemd, гостевые
#              бинари обеих архитектур;
#   tarball  — DIR/cream_<v>_linux_<arch>.tar.gz: точный состав, владелец 0/0.
set -eu

kind="$1" version="$2" arch="$3" dir="$4"
unit_src=internal/systemd/cream-configure@.service
fail=0
bad() { echo "check-deb: $*" >&2; fail=1; }
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# elf_machine FILE — e_machine заголовка ELF (x86-64 = 62, aarch64 = 183).
elf_machine() { od -An -tu2 -j18 -N2 "$1" | tr -d ' '; }
machine_of() { case "$1" in amd64) echo 62 ;; arm64) echo 183 ;; *) echo "?" ;; esac; }

check_deb() {
	deb="$dir/cream_${version}_${arch}.deb"
	[ -f "$deb" ] || { bad "нет $deb"; return; }

	# Файлы (не каталоги) с режимом и владельцем, отсортированные.
	dpkg-deb -c "$deb" >"$work/list"
	awk '$1 !~ /^d/ {sub(/^\.\//, "", $6); print $1, $2, "/" $6}' "$work/list" | sort >"$work/files"
	cat >"$work/want" <<-LIST
		-rw-r--r-- root/root /usr/lib/systemd/system/cream-configure@.service
		-rw-r--r-- root/root /usr/share/doc/cream/changelog.gz
		-rw-r--r-- root/root /usr/share/doc/cream/copyright
		-rwxr-xr-x root/root /usr/bin/cream
		-rwxr-xr-x root/root /usr/lib/cream/guest/linux-amd64/cream
		-rwxr-xr-x root/root /usr/lib/cream/guest/linux-arm64/cream
	LIST
	sort -o "$work/want" "$work/want"
	if ! cmp -s "$work/want" "$work/files"; then
		bad "$deb: состав не совпал с §15 (ожидание <, факт >):"
		diff "$work/want" "$work/files" >&2 || true
	fi
	if awk '$2 != "root/root"' "$work/list" | grep -q .; then
		bad "$deb: есть записи не root/root"
	fi
	if awk '$1 ~ /^d/ && $1 != "drwxr-xr-x"' "$work/list" | grep -q .; then
		bad "$deb: каталог не 0755"
	fi

	field() { dpkg-deb -f "$deb" "$1"; }
	[ "$(field Package)" = cream ] || bad "$deb: Package=$(field Package)"
	[ "$(field Version)" = "$version" ] || bad "$deb: Version=$(field Version), ожидалось $version"
	[ "$(field Architecture)" = "$arch" ] || bad "$deb: Architecture=$(field Architecture)"
	[ -z "$(field Depends)" ] || bad "$deb: есть Depends: $(field Depends)"
	[ -z "$(field Pre-Depends)" ] || bad "$deb: есть Pre-Depends: $(field Pre-Depends)"
	[ "$(field Suggests)" = "git, libguestfs-tools" ] || bad "$deb: Suggests=$(field Suggests)"

	dpkg-deb -e "$deb" "$work/control"
	for s in postinst prerm postrm; do
		[ -x "$work/control/$s" ] || bad "$deb: нет исполняемого $s"
		cmp -s "$work/control/$s" "packaging/scripts/$s" || bad "$deb: $s отличается от packaging/scripts/$s"
	done

	dpkg-deb -x "$deb" "$work/root"
	cmp -s "$work/root/usr/lib/systemd/system/cream-configure@.service" "$unit_src" ||
		bad "$deb: юнит отличается от $unit_src"
	cmp -s "$work/root/usr/bin/cream" "$work/root/usr/lib/cream/guest/linux-$arch/cream" ||
		bad "$deb: /usr/bin/cream не совпадает с гостевым linux-$arch"
	for g in amd64 arm64; do
		m="$(elf_machine "$work/root/usr/lib/cream/guest/linux-$g/cream")"
		[ "$m" = "$(machine_of "$g")" ] || bad "$deb: гостевой linux-$g собран под e_machine=$m"
	done
	gzip -t "$work/root/usr/share/doc/cream/changelog.gz" || bad "$deb: changelog.gz битый"
	zcat "$work/root/usr/share/doc/cream/changelog.gz" | head -n1 | grep -q "^cream ($version)" ||
		bad "$deb: заголовок changelog не про версию $version"
	# Бинарь своей архитектуры запускается: статический, версия встроена.
	if [ "$(uname -m)" = "$(case "$arch" in amd64) echo x86_64 ;; arm64) echo aarch64 ;; esac)" ]; then
		"$work/root/usr/bin/cream" version >"$work/version" || bad "$deb: cream version упал"
		grep -q . "$work/version" || bad "$deb: cream version ничего не напечатал"
	fi
	[ "$fail" = 0 ] && echo "check-deb: $deb — состав, поля, сценарии и юнит верны"
}

check_tarball() {
	tgz="$dir/cream_${version}_linux_${arch}.tar.gz"
	[ -f "$tgz" ] || { bad "нет $tgz"; return; }
	tar -tvzf "$tgz" --numeric-owner | awk '{print $1, $2, $6}' | sort >"$work/files"
	cat >"$work/want" <<-LIST
		-rw-r--r-- 0/0 LICENSE
		-rw-r--r-- 0/0 NOTICE
		-rw-r--r-- 0/0 copyright
		-rw-r--r-- 0/0 cream-configure@.service
		-rwxr-xr-x 0/0 cream
	LIST
	sort -o "$work/want" "$work/want"
	if ! cmp -s "$work/want" "$work/files"; then
		bad "$tgz: состав не совпал (ожидание <, факт >):"
		diff "$work/want" "$work/files" >&2 || true
	fi
	mkdir "$work/t" && tar -xzf "$tgz" -C "$work/t"
	cmp -s "$work/t/cream-configure@.service" "$unit_src" || bad "$tgz: юнит отличается от $unit_src"
	cmp -s "$work/t/LICENSE" LICENSE || bad "$tgz: LICENSE отличается"
	cmp -s "$work/t/copyright" packaging/copyright || bad "$tgz: copyright отличается от packaging/copyright"
	grep -q 'Permission is hereby granted' "$work/t/copyright" || bad "$tgz: в copyright нет текста MIT"
	grep -q 'Neither the name of Google LLC' "$work/t/copyright" || bad "$tgz: в copyright нет текста BSD-3-Clause"
	[ "$(elf_machine "$work/t/cream")" = "$(machine_of "$arch")" ] || bad "$tgz: cream не под $arch"
	[ "$fail" = 0 ] && echo "check-deb: $tgz — состав верен"
}

case "$kind" in
deb) check_deb ;;
tarball) check_tarball ;;
*) echo "usage: check-deb.sh deb|tarball VERSION ARCH DIR" >&2; exit 64 ;;
esac
exit "$fail"
