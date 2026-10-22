GO ?= go
GOFMT ?= $(shell "$(GO)" env GOROOT)/bin/gofmt

.DEFAULT_GOAL := build
.PHONY: build test test-race vet check fmt fmt-check package

build:
	mkdir -p bin
	"$(GO)" build -trimpath -o bin/myarch-buildkit ./cmd/myarch-buildkit

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

# Local packaging only: no tags, uploads, or automatic publication.
package:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$(GO)" build -trimpath -o dist/myarch-buildkit ./cmd/myarch-buildkit
	tar -czf dist/myarch-buildkit-linux-x86_64.tar.gz -C dist myarch-buildkit -C "$(CURDIR)" README.md CONTRIBUTING.md LICENSE docs
	cd dist && sha256sum myarch-buildkit-linux-x86_64.tar.gz > SHA256SUMS
