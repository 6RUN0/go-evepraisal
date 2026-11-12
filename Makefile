ENV?=dev
ifeq ($(ENV), dev)
	BUILD_OPTS?=-tags dev
else
	BUILD_OPTS?=
endif

GO ?= go
PKG_DIRS=$(shell $(GO) list ./... | grep -v /vendor/)
GOOS?=$(shell $(GO) env GOOS)
GOARCH?=$(shell $(GO) env GOARCH)
GOPATH?=$(shell $(GO) env GOPATH)
SHELL := $(shell which bash)
export PATH := $(PATH):$(GOPATH)/bin

# Tools are pinned in tools/go.mod, actionlint in tools/actionlint/go.mod
# because its yaml dependency conflicts with golangci-lint's. "go tool
# -modfile" would load the root vendor/, so "go tool -n" prints the binary.
tool = $$($(GO) -C $(1) tool -n $(2))
GOLANGCI_LINT = $(call tool,tools,golangci-lint)
GOVULNCHECK = $(call tool,tools,govulncheck)
ACTIONLINT = $(call tool,tools/actionlint,actionlint)
TOOL_MODULES := tools tools/actionlint

RUMDL_VERSION := 0.3.2
RUMDL ?= uvx rumdl@$(RUMDL_VERSION)
HADOLINT ?= hadolint
SHELLCHECK ?= shellcheck

# A binary built with -race sleeps 1 s on exit by default, every test binary
# included.
GORACE ?= atexit_sleep_ms=0

# Typographic characters a keyboard does not type, refused in comments, docs
# and messages. Excluded paths hold such characters as data: pasted client
# text in parser tests and constants, SDE lines, third-party assets and UI.
SYMBOLS := [\x{2010}-\x{2015}\x{2018}-\x{201F}\x{2026}\x{00AB}\x{00BB}\x{00A0}\x{00AD}\x{200B}\x{2190}-\x{21FF}\x{2713}\x{2717}\x{2022}\x{2500}-\x{257F}\x{00D7}\x{00F7}\x{2264}\x{2265}]
SYMBOLS_EXCLUDE := ':!vendor' ':!web/resources' ':!staticdump/testdata' ':!parsers/utils.go' ':!parsers/*_test.go'

default: build
.PHONY: build install generate clean run run-reload test-reload \
	check lint test vuln tidy markdown hadolint actionlint shellcheck

check: lint build test tidy markdown hadolint actionlint shellcheck
	-$(MAKE) vuln

build: generate
	@$(GO) build -gcflags=-trimpath=$(shell pwd) \
		-asmflags=-trimpath=$(shell pwd) \
		-gcflags=-trimpath=$(shell pwd) \
		-mod vendor \
		${BUILD_OPTS} \
		-o ./target/evepraisal-${GOOS}-${GOARCH} \
		./evepraisal
	$(GO) build ./...

install: generate
	@$(GO) install -gcflags=-trimpath=$(shell pwd) \
		-asmflags=-trimpath=$(shell pwd) \
		-gcflags=-trimpath=$(shell pwd) \
		-mod vendor \
		${BUILD_OPTS} \
		${PKG_DIRS}

generate:
	$(GO) generate ${BUILD_OPTS} ${PKG_DIRS}

clean:
	$(GO) clean ./...
	rm -rf target

lint:
	$(GOLANGCI_LINT) run
	$(GOLANGCI_LINT) fmt --diff

# checkptr is off because the vendored github.com/boltdb/bolt fails it
# ("converted pointer straddles multiple allocations") under -race.
test:
	GORACE=$(GORACE) $(GO) test -race -gcflags=all=-d=checkptr=0 ./...

vuln:
	$(GOVULNCHECK) ./...

# go mod vendor -o writes a fresh copy aside, so a stale vendor/ fails the
# check without the tree being modified.
tidy:
	$(GO) mod tidy -diff
	@tmp=$$(mktemp -d) || { echo "tidy: mktemp failed"; exit 1; }; \
		trap 'rm -rf "$$tmp"' EXIT; \
		$(GO) mod vendor -o "$$tmp/vendor" || { echo "tidy: go mod vendor -o failed, vendor/ not compared"; exit 1; }; \
		diff -r vendor "$$tmp/vendor" || { echo "vendor/ is out of date: run go mod vendor"; exit 1; }
	@for m in $(TOOL_MODULES); do (cd $$m && $(GO) mod tidy -diff) || exit 1; done

markdown:
	$(RUMDL) check .
	@LC_ALL=C.UTF-8 git grep -nIP --untracked '$(SYMBOLS)' -- $(SYMBOLS_EXCLUDE); rc=$$?; \
		case $$rc in \
		1) ;; \
		0) echo "non-keyboard characters found"; exit 1 ;; \
		*) echo "symbol check failed: git grep exit $$rc"; exit 1 ;; \
		esac

hadolint:
	$(HADOLINT) Dockerfile

actionlint:
	$(ACTIONLINT)

shellcheck:
	@files=$$(git ls-files --cached --others --exclude-standard -- '*.sh' ':!vendor' | \
		while read -r f; do [ -e "$$f" ] && echo "$$f"; done); \
		[ -z "$$files" ] || $(SHELLCHECK) $$files

run: install
	evepraisal

run-reload:
	reflex -c reflex.conf

test-reload:
	reflex -c reflex.test.conf
