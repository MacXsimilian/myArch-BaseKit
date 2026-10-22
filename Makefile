GO ?= go
VERSION ?=

.PHONY: build test vet check fmt fmt-check checksum package release-check

build:
	mkdir -p bin
	$(GO) build -trimpath $(if $(VERSION),-ldflags "-X main.BuildID=$(VERSION)") -o bin/myarch-buildkit ./cmd/myarch-buildkit

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: fmt-check test vet

fmt:
	gofmt -w cmd/myarch-buildkit/*.go

fmt-check:
	@unformatted=$$(gofmt -l cmd/myarch-buildkit/*.go) || exit $$?; \
	  test -z "$$unformatted" || { echo "$$unformatted"; echo 'Run make fmt before committing.'; exit 1; }

checksum: build
	cd bin && sha256sum myarch-buildkit > SHA256SUMS

package: checksum
	mkdir -p dist
	tar -czf dist/myarch-buildkit-linux-x86_64.tar.gz -C bin myarch-buildkit SHA256SUMS -C "$(CURDIR)" LICENSE README.md CONTRIBUTING.md docs/user-guide.md
	cd dist && sha256sum myarch-buildkit-linux-x86_64.tar.gz > SHA256SUMS

release-check:
	bash scripts/validate-release.sh "$(VERSION)"
