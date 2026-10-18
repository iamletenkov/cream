# cream — публичный API репозитория. Go на хосте не нужен: всё в контейнере.
# Кэш модулей, сборки и линтера — в .cache/ (gitignored), бинари — в dist/.

GO_IMAGE       ?= golang:1.25.11
GOLANGCI_IMAGE ?= golangci/golangci-lint:v2.14.0
MODULE         := github.com/iamletenkov/cream
VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
ARCHES         ?= amd64 arm64
LDFLAGS        := -s -w -X $(MODULE)/internal/version.Version=$(VERSION)

UID := $(shell id -u)
GID := $(shell id -g)

# -buildvcs=false: в git worktree .git — файл-указатель наружу, VCS-штамп внутри
# контейнера не собирается; версия всё равно приходит через ldflags.
DOCKER_RUN = docker run --rm -u $(UID):$(GID) \
	-v "$(CURDIR):/src" -w /src \
	-v "$(CURDIR)/.cache:/cache" \
	-e HOME=/tmp -e GOCACHE=/cache/go-build -e GOMODCACHE=/cache/mod \
	-e GOLANGCI_LINT_CACHE=/cache/golangci -e GOFLAGS=-buildvcs=false
GO = $(DOCKER_RUN) $(GO_IMAGE)

.DEFAULT_GOAL := help
.PHONY: help build build-all test lint fmt ci clean cache

help: ## список целей
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-10s %s\n", $$1, $$2}'

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
test: cache ## go test -race ./...
	$(GO) env CGO_ENABLED=1 go test -race -count=1 ./...

fmt: cache ## gofmt -w по всему модулю
	$(GO) gofmt -w .

lint: cache ## gofmt -l, go vet, golangci-lint
	@out="$$($(GO) gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt: не отформатированы (make fmt):"; echo "$$out"; exit 1; fi
	$(GO) go vet ./...
	$(DOCKER_RUN) $(GOLANGCI_IMAGE) golangci-lint run ./...

ci: lint test build-all ## всё, что гоняет CI: lint + test + build-all

clean: ## удалить dist/ (кэш .cache/ сохраняется)
	rm -rf dist
