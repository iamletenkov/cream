#!/bin/sh
# Подготовка файлов выпуска (спека §15). Запускается в контейнере debian из
# корня репозитория (make deb / make dist-release), всё детерминировано от
# SOURCE_DATE_EPOCH: mtime, порядок, владелец 0/0, gzip без имени и времени.
#   stage   VERSION DIR       — DIR/stage/changelog.gz для nfpm;
#   archive VERSION DIR ARCH… — DIR/cream_<v>_linux_<arch>.tar.gz и
#                               DIR/SHA256SUMS по всем .deb и .tar.gz в DIR.
set -eu

cmd="$1" version="$2" dir="$3"
shift 3
: "${SOURCE_DATE_EPOCH:?нужен SOURCE_DATE_EPOCH (время коммита)}"

case "$cmd" in
stage)
	mkdir -p "$dir/stage"
	# Debian-changelog выпуска: история изменений — CHANGELOG.md репозитория
	# (release-please), сюда — только запись о версии с датой коммита.
	date="$(date -u -R -d "@$SOURCE_DATE_EPOCH")"
	printf '%s\n' \
		"cream ($version) unstable; urgency=medium" \
		"" \
		"  * Выпуск $version; изменения — CHANGELOG.md в репозитории" \
		"    https://github.com/iamletenkov/cream." \
		"" \
		" -- Letenkov Maksim <letenkovmaksim@yandex.ru>  $date" |
		gzip -9n >"$dir/stage/changelog.gz"
	touch -d "@$SOURCE_DATE_EPOCH" "$dir/stage/changelog.gz"
	chmod 0644 "$dir/stage/changelog.gz"
	;;
archive)
	for arch in "$@"; do
		tree="$dir/stage/tar-$arch"
		rm -rf "$tree"
		mkdir -p "$tree"
		cp "dist/cream-linux-$arch" "$tree/cream"
		cp LICENSE NOTICE packaging/copyright internal/systemd/cream-configure@.service \
			internal/systemd/cream-configure-cloud-user.service "$tree/"
		chmod 0755 "$tree/cream"
		chmod 0644 "$tree/LICENSE" "$tree/NOTICE" "$tree/copyright" "$tree/cream-configure@.service" \
			"$tree/cream-configure-cloud-user.service"
		tar --sort=name --format=gnu --mtime="@$SOURCE_DATE_EPOCH" \
			--owner=0 --group=0 --numeric-owner -C "$tree" -cf - \
			cream LICENSE NOTICE copyright cream-configure@.service cream-configure-cloud-user.service |
			gzip -9n >"$dir/cream_${version}_linux_${arch}.tar.gz"
		rm -rf "$tree"
	done
	rm -rf "$dir/stage"
	(cd "$dir" && sha256sum -- *.deb *.tar.gz >SHA256SUMS)
	;;
*)
	echo "usage: dist-release.sh stage|archive VERSION DIR [ARCH…]" >&2
	exit 64
	;;
esac
