# Окружение make test-build (план T22): libguestfs с ядром для appliance,
# qemu-img для overlay, jq и procps для проверок. cream в образ не ставится:
# сценарий распаковывает проверяемый .deb сам (dpkg-deb -x), как bisquite
# берёт гостевой бинарь из пакета cream на хосте.
FROM debian:13
RUN apt-get update \
	&& DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
		libguestfs-tools qemu-utils linux-image-amd64 jq procps ca-certificates \
	&& rm -rf /var/lib/apt/lists/*
