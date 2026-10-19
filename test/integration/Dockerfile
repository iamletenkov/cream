# Машина с systemd как PID 1 для живых проверок cream (спека §14): эталоны
# EnvironmentFile (capture-envfile.sh) и сценарии test/integration/scenario.
# Один файл на все дистрибутивы — база в ARG BASE (debian:12, ubuntu:24.04, …).
ARG BASE=debian:12
FROM ${BASE}
ENV DEBIAN_FRONTEND=noninteractive container=docker
# jq — разбор --json в сценариях; procps — ps для аудита секретов; dbus —
# systemd-run --pipe (эталоны EnvironmentFile) ходит через системную шину.
# Консоль и вход контейнеру не нужны: getty и logind замаскированы.
RUN apt-get update \
	&& apt-get install -y --no-install-recommends systemd systemd-sysv ca-certificates procps jq dbus \
	&& apt-get clean \
	&& rm -rf /var/lib/apt/lists/* \
	&& systemctl mask getty.target console-getty.service systemd-logind.service systemd-firstboot.service
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
