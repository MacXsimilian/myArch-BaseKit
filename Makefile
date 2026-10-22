GO ?= go
GOFMT ?= $(shell "$(GO)" env GOROOT)/bin/gofmt
VERSION ?=
export GO VERSION

.DEFAULT_GOAL := build
.PHONY: help build test test-race vet check fmt fmt-check lint automation-test checksum package package-check release-check release

help:
	@printf '%s\n' \
	  'make build             Build the local development executable in bin/' \
	  'make check             Check formatting, tests and vet' \
	  'make test-race         Run tests with the race detector' \
	  'make lint              Run actionlint and ShellCheck (install separately)' \
	  'make automation-test   Test packaging and release logic offline (Python 3)' \
	  'make checksum          Build and checksum the local executable' \
	  'make package           Build the Linux x86-64 archive in dist/' \
	  'make package-check     Verify and smoke-test the existing archive' \
	  'make release VERSION=vX.Y.Z  Check, package and verify; never publish' \
	  'GO=/path/to/go selects a compiler; .go-version pins release builds.'

build:
	bash scripts/build.sh build

test:
	"$(GO)" test ./...

test-race:
	"$(GO)" test -race ./...

vet:
	"$(GO)" vet ./...

check: fmt-check test vet

fmt:
	"$(GOFMT)" -w cmd/myarch-buildkit/*.go

fmt-check:
	@unformatted=$$("$(GOFMT)" -l cmd/myarch-buildkit/*.go) || exit $$?; \
	  test -z "$$unformatted" || { printf '%s\n' "$$unformatted" 'Run make fmt before committing.'; exit 1; }

lint:
	actionlint -color
	shellcheck scripts/*.sh

automation-test:
	python3 -B -m unittest discover -s scripts/tests -p 'test_*.py' -v

checksum: build
	cd bin && sha256sum myarch-buildkit > SHA256SUMS

package:
	bash scripts/build.sh package

package-check:
	bash scripts/verify-package.sh

release-check:
	bash scripts/validate-release.sh "$(VERSION)"

# Recursive calls keep validation before packaging, including under make -j.
release: release-check
	$(MAKE) fmt-check vet test-race lint automation-test
	$(MAKE) package
	$(MAKE) package-check
