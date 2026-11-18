# Makefile of the DNS change monitor.
#
# The targets are the command line interface of this repository. Run
# "make help" for the menu.

# --- Preamble ----------------------------------------------------------------
SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.ONESHELL:
.DELETE_ON_ERROR:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules
MAKEFLAGS += --no-builtin-variables
.DEFAULT_GOAL := help

# "make <target>" stays quiet, "make <target> V=1" echoes every command.
V ?= 0
ifeq ($(V),1)
  Q :=
else
  Q := @
endif

# --- Project -----------------------------------------------------------------
BINARY   := dnsmon
MODULE   := github.com/aognio/dns-change-monitor
CMD      := ./cmd/$(BINARY)
BUILDPKG := $(MODULE)/pkg/build

OUTDIR   := bin
TARGET   := $(OUTDIR)/$(BINARY)

PREFIX   ?= $(HOME)/.local
BINDIR   ?= $(PREFIX)/bin
DESTDIR  ?=

GO       ?= go
GOFLAGS  ?=

# The version comes from git, never from a literal in this file.
ifeq ($(origin VERSION),undefined)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
endif
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X $(BUILDPKG).Version=$(VERSION) \
           -X $(BUILDPKG).Commit=$(COMMIT) \
           -X $(BUILDPKG).Date=$(DATE)

SOURCES := $(shell find . -name '*.go' -not -path './vendor/*' 2>/dev/null)

# Configuration file used by the run, daemon and status targets.
CONFIG ?= config.toml

# Extra arguments for the run and test targets:
#   make run ARGS='records list'
#   make test ARGS='-run TestPolling -v'
ARGS ?=

##@ Build

.PHONY: build
build: $(TARGET) ## Build the binary into bin/

$(TARGET): $(SOURCES)
	$(Q)mkdir -p $(@D)
	$(Q)$(GO) build $(GOFLAGS) -trimpath -ldflags '$(LDFLAGS)' -o $@ $(CMD)
	$(Q)echo "built $@ ($(VERSION))"

.PHONY: run
run: $(TARGET) ## Run the monitor in the foreground (make run ARGS='...')
	$(Q)$(TARGET) run --config $(CONFIG) $(ARGS)

.PHONY: daemon
daemon: $(TARGET) ## Run the monitor in the background
	$(Q)$(TARGET) run --daemon --config $(CONFIG) $(ARGS)

.PHONY: stop
stop: $(TARGET) ## Stop the background monitor
	$(Q)$(TARGET) stop --config $(CONFIG)

.PHONY: status
status: $(TARGET) ## Show the state of the monitor
	$(Q)$(TARGET) status --config $(CONFIG)

.PHONY: sync
sync: $(TARGET) ## Apply the configuration file to the database
	$(Q)$(TARGET) sync --config $(CONFIG)

.PHONY: check
check-records: $(TARGET) ## Resolve the monitored records once
	$(Q)$(TARGET) check --config $(CONFIG)

##@ Quality

.PHONY: test
test: ## Run the test suite
	$(Q)$(GO) test $(ARGS) ./...

.PHONY: test-race
test-race: ## Run the test suite with the race detector, uncached
	$(Q)$(GO) test -race -count=1 ./...

.PHONY: cover
cover: ## Run the test suite and report the coverage
	$(Q)$(GO) test -coverprofile=coverage.out ./...
	$(Q)$(GO) tool cover -func=coverage.out | tail -n 1

.PHONY: vet
vet: ## Run go vet
	$(Q)$(GO) vet ./...

.PHONY: fmt
fmt: ## Format the sources (gofumpt if available, else gofmt)
	$(Q)if command -v gofumpt >/dev/null 2>&1; then
		gofumpt -w .
	else
		gofmt -s -w .
	fi

.PHONY: fmt-check
fmt-check: ## Fail if any source file is unformatted
	$(Q)out=$$(gofmt -l .)
	if [ -n "$$out" ]; then
		echo "these files are not formatted:" >&2
		echo "$$out" >&2
		exit 1
	fi

.PHONY: lint
lint: fmt-check vet ## Run the formatting and the compiler checks

.PHONY: ci
ci: fmt-check vet test ## Run everything the continuous integration runs

##@ Modules

.PHONY: tidy
tidy: ## Refresh the dependency list
	$(Q)$(GO) mod tidy

.PHONY: tidy-check
tidy-check: ## Fail if go.mod and go.sum are not tidy
	$(Q)$(GO) mod tidy -diff

##@ Install

.PHONY: install
install: $(TARGET) ## Install the binary into PREFIX/bin
	$(Q)install -D -m 0755 $(TARGET) $(DESTDIR)$(BINDIR)/$(BINARY)
	$(Q)echo "installed $(DESTDIR)$(BINDIR)/$(BINARY)"

.PHONY: uninstall
uninstall: ## Remove the installed binary
	$(Q)rm -f $(DESTDIR)$(BINDIR)/$(BINARY)

.PHONY: install-systemd
install-systemd: $(TARGET) ## Install the binary and the systemd unit (needs root)
	$(Q)install -D -m 0755 $(TARGET) /usr/local/bin/$(BINARY)
	$(Q)install -D -m 0644 deploy/systemd/dnsmon.service /etc/systemd/system/dnsmon.service
	$(Q)install -D -m 0600 deploy/systemd/dnsmon.env.example /etc/dnsmon/dnsmon.env
	$(Q)install -D -m 0644 deploy/systemd/config.toml.example /etc/dnsmon/config.toml
	$(Q)systemctl daemon-reload
	$(Q)echo "edit /etc/dnsmon/dnsmon.env and /etc/dnsmon/config.toml, then run: systemctl enable --now dnsmon"

.PHONY: uninstall-systemd
uninstall-systemd: ## Remove the systemd unit and the installed binary (needs root)
	$(Q)systemctl disable --now dnsmon 2>/dev/null || true
	$(Q)rm -f /etc/systemd/system/dnsmon.service /usr/local/bin/$(BINARY)
	$(Q)systemctl daemon-reload

##@ Housekeeping

.PHONY: clean
clean: ## Remove the build artifacts
	$(Q)rm -rf $(OUTDIR) coverage.out

##@ Help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z0-9_%-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@echo ""

.PHONY: require-%
require-%:
	@command -v $* >/dev/null 2>&1 || { \
		echo "error: '$*' is required but not installed" >&2; exit 1; }

.PHONY: print-%
print-%:
	@echo '$*=$($*)'
