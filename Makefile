GO ?= go
VERSION ?=

.PHONY: build test vet check fmt checksum

build:
	mkdir -p bin
	$(GO) build $(if $(VERSION),-ldflags "-X main.BuildID=$(VERSION)") -o bin/myarch-buildkit ./cmd/myarch-buildkit

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: test vet

fmt:
	gofmt -w cmd/myarch-buildkit/*.go

checksum: build
	cd bin && sha256sum myarch-buildkit > SHA256SUMS
