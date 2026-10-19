# cream — публичный API репозитория. Go на хосте не нужен: всё в контейнере.
# Кэш модулей, сборки и линтера — в .cache/ (gitignored), бинари — в dist/.

GO_IMAGE       ?= golang:1.26.9
GOLANGCI_IMAGE ?= golangci/golangci-lint:v2.14.0
NFPM_IMAGE     ?= goreleaser/nfpm:v2.47.0
DEBIAN_IMAGE   ?= debian:13
GOVULNCHECK    ?= golang.org/x/vuln/cmd/govulncheck@v1.8.0
MODULE         := github.com/iamletenkov/cream
VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
ARCHES         ?= amd64 arm64
PKG            ?= ./...
TEST_P         ?= 4
# Замок тяжёлых прогонов: на общем хосте один ci за раз (и у людей, и у агентов).
# HEAVY берёт замок, если его ещё не держит вызывающий (CREAM_HEAVY_HELD): цель
# под замком может звать другую такую же без взаимной блокировки.
HEAVY_LOCK     ?= /tmp/cream-heavy.lock
HEAVY           = $(if $(CREAM_HEAVY_HELD),,flock $(HEAVY_LOCK) env CREAM_HEAVY_HELD=1)
# Воспроизводимость (§15): время коммита — mtime в пакетах и архивах.
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || echo 0)
# Версия пакета — без «v»; describe не от тега (хеш коммита) — 0.0.0~<describe>.
DEB_VERSION    ?= $(if $(and $(filter 0% 1% 2% 3% 4% 5% 6% 7% 8% 9%,$(VERSION:v%=%)),$(findstring .,$(VERSION))),$(VERSION:v%=%),0.0.0~$(VERSION:v%=%))
ARCH           ?= amd64
RELEASE_DIR    ?= dist/release
# Подкаталог .cache/ под кеш сборки Go: repro-check собирает в разных кешах.
GOCACHE_DIR    ?= go-build
# Живая интеграция (§14): DISTRO — один из test/integration/run.sh или all
# (= INTEGRATION_DISTROS по очереди); пакет — $(RELEASE_DIR)/cream_<v>_$(ARCH).deb.
DISTRO             ?= debian-12
INTEGRATION_DISTROS ?= debian-12 ubuntu-24.04
LDFLAGS        := -s -w -X $(MODULE)/internal/version.Version=$(VERSION)

UID := $(shell id -u)
GID := $(shell id -g)

# -buildvcs=false: в git worktree .git — файл-указатель наружу, VCS-штамп внутри
# контейнера не собирается; версия всё равно приходит через ldflags.
DOCKER_RUN = docker run --rm -u $(UID):$(GID) \
	-v "$(CURDIR):/src" -w /src \
	-v "$(CURDIR)/.cache:/cache" \
	-e HOME=/tmp -e GOCACHE=/cache/$(GOCACHE_DIR) -e GOMODCACHE=/cache/mod \
	-e GOLANGCI_LINT_CACHE=/cache/golangci -e GOFLAGS=-buildvcs=false \
	-e SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH)
GO = $(DOCKER_RUN) $(GO_IMAGE)
# Go-файлы модуля без кэша модулей (.cache/mod) и сборки: gofmt по «.» зашёл бы в зависимости.
GO_FILES = find . \( -path ./.cache -o -path ./dist -o -path ./.scratchpad -o -path ./.git \) -prune -o -name '*.go' -print

.DEFAULT_GOAL := help
.PHONY: help build build-all test test-golden lint fmt tidy ci ci-unlocked clean cache \
	deb deb-unlocked deb-pkg dist-release dist-release-unlocked repro-check vuln \
	test-integration test-integration-unlocked envfile-refs

help: ## список целей
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-12s %s\n", $$1, $$2}'

cache:
	@mkdir -p .cache dist

build: cache ## dist/cream под архитектуру хоста (статический, CGO_ENABLED=0)
	$(GO) env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/cream ./cmd/cream

build-all: cache ## dist/cream-linux-{amd64,arm64} (статические, -trimpath)
	@set -e; for arch in $(ARCHES); do \
		echo "== linux/$$arch"; \
		$(GO) env CGO_ENABLED=0 GOOS=linux GOARCH=$$arch \
			go build -trimpath -ldflags "$(LDFLAGS)" -o dist/cream-linux-$$arch ./cmd/cream; \
	done

# -race требует cgo: тесты собираются с CGO_ENABLED=1 (gcc есть в образе golang),
# релизный бинарь — без cgo.
test: cache ## go test -race $(PKG) (по умолчанию ./..., параллелизм пакетов TEST_P)
	$(GO) env CGO_ENABLED=1 go test -race -count=1 -p $(TEST_P) $(PKG)

test-golden: cache ## переписать golden-файлы CLI (internal/cli/testdata/golden) по текущему выводу
	$(GO) go test -count=1 ./internal/cli -update

tidy: cache ## go mod tidy (go.mod/go.sum)
	$(GO) go mod tidy

fmt: cache ## gofmt -w по всему модулю
	$(GO) sh -c "$(GO_FILES) | xargs -r gofmt -w"

lint: cache ## gofmt -l, go vet, golangci-lint
	@out="$$($(GO) sh -c "$(GO_FILES) | xargs -r gofmt -l")"; if [ -n "$$out" ]; then echo "gofmt: не отформатированы (make fmt):"; echo "$$out"; exit 1; fi
	$(GO) go vet ./...
	$(DOCKER_RUN) $(GOLANGCI_IMAGE) golangci-lint run ./...

ci: ## всё, что гоняет CI: lint + test + build-all (под замком HEAVY_LOCK)
	$(HEAVY) $(MAKE) --no-print-directory ci-unlocked

ci-unlocked: lint test build-all

deb: ## $(RELEASE_DIR)/cream_<v>_$(ARCH).deb (nfpm; ARCH=amd64|arm64, под замком)
	$(HEAVY) $(MAKE) --no-print-directory deb-unlocked

deb-unlocked: build-all
	$(MAKE) --no-print-directory deb-pkg

# Один .deb из готовых dist/cream-linux-*: changelog.gz → nfpm → проверка
# состава в debian (scripts/check-deb.sh).
deb-pkg:
	@mkdir -p $(RELEASE_DIR)
	$(DOCKER_RUN) $(DEBIAN_IMAGE) sh scripts/dist-release.sh stage $(DEB_VERSION) $(RELEASE_DIR)
	$(DOCKER_RUN) -e ARCH=$(ARCH) -e DEB_VERSION=$(DEB_VERSION) -e STAGE=$(RELEASE_DIR)/stage \
		$(NFPM_IMAGE) package -f packaging/nfpm.yaml -p deb -t $(RELEASE_DIR)/cream_$(DEB_VERSION)_$(ARCH).deb
	$(DOCKER_RUN) $(DEBIAN_IMAGE) sh scripts/check-deb.sh deb $(DEB_VERSION) $(ARCH) $(RELEASE_DIR)

dist-release: ## оба .deb, тарболы cream_<v>_linux_<arch>.tar.gz и SHA256SUMS в $(RELEASE_DIR) (под замком)
	$(HEAVY) $(MAKE) --no-print-directory dist-release-unlocked

dist-release-unlocked: build-all
	rm -rf "$(RELEASE_DIR)"
	@set -e; for a in $(ARCHES); do $(MAKE) --no-print-directory deb-pkg ARCH=$$a; done
	$(DOCKER_RUN) $(DEBIAN_IMAGE) sh scripts/dist-release.sh archive $(DEB_VERSION) $(RELEASE_DIR) $(ARCHES)
	@set -e; for a in $(ARCHES); do \
		$(DOCKER_RUN) $(DEBIAN_IMAGE) sh scripts/check-deb.sh tarball $(DEB_VERSION) $$a $(RELEASE_DIR); \
	done

repro-check: ## две сборки dist-release с одним SOURCE_DATE_EPOCH — sha256 совпадают (под замком)
	$(HEAVY) env MAKE="$(MAKE)" sh scripts/repro-check.sh

test-integration: ## .deb в контейнере с systemd, сценарии test/integration (DISTRO=debian-12|ubuntu-24.04|debian-13|all; под замком)
	$(HEAVY) $(MAKE) --no-print-directory test-integration-unlocked

# Контейнеры — по одному (cream-it-<distro>, удаляются run.sh); лог каждого
# прогона — .scratchpad/test-runs/integration-<distro>-<время>/.
test-integration-unlocked: deb-unlocked
	@set -e; for d in $(if $(filter all,$(DISTRO)),$(INTEGRATION_DISTROS),$(DISTRO)); do \
		log=.scratchpad/test-runs/integration-$$d-$$(date +%Y%m%d-%H%M%S); \
		echo "== integration $$d ($(ARCH)) → $$log"; \
		ARCH=$(ARCH) bash test/integration/run.sh $$d $(RELEASE_DIR)/cream_$(DEB_VERSION)_$(ARCH).deb $$log; \
	done

envfile-refs: ## переснять эталоны EnvironmentFile systemd 249/252/255/257 (internal/envfile/testdata; под замком)
	$(HEAVY) bash test/integration/capture-envfile.sh

# go run …@версия собирает govulncheck вне модуля: go.mod/go.sum не меняются,
# модули инструмента — в отдельном .cache/vuln-mod.
vuln: cache ## govulncheck по модулю (версия закреплена в GOVULNCHECK)
	$(DOCKER_RUN) -e GOMODCACHE=/cache/vuln-mod -e GOFLAGS="-buildvcs=false -mod=mod" \
		$(GO_IMAGE) go run $(GOVULNCHECK) ./...

clean: ## удалить dist/ (кэш .cache/ сохраняется)
	rm -rf dist
