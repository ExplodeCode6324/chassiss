GO ?= go
VERSION ?= 0.1.0-dev
BUILD_DIGEST ?= development
RELEASE_IDENTITY ?= local

MODULE := github.com/ExplodeCode6324/chassiss
LDFLAGS := -s -w \
	-X $(MODULE)/internal/cli.Version=$(VERSION) \
	-X $(MODULE)/internal/cli.BuildDigest=$(BUILD_DIGEST) \
	-X $(MODULE)/internal/cli.ReleaseIdentity=$(RELEASE_IDENTITY)

.PHONY: all build test race vet check clean

all: check build

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/chassiss ./cmd/chassiss

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

check:
	test -z "$$(gofmt -l cmd internal)"
	$(GO) test ./...
	$(GO) vet ./...
	$(GO) build ./cmd/chassiss

clean:
	$(GO) clean
	rm -f bin/chassiss

