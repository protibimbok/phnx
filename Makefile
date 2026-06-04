BINARY  := phnx
MODULE  := github.com/protibimbok/phnx
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -ldflags "-X $(MODULE)/cmd.Version=$(VERSION) -X $(MODULE)/cmd.Commit=$(COMMIT) -s -w"

.PHONY: build install clean release snapshot lint

build:
	go build $(LDFLAGS) -o $(BINARY) .

install:
	go install $(LDFLAGS) .

clean:
	rm -f $(BINARY)
	rm -rf dist/

snapshot:
	goreleaser release --snapshot --clean

release:
	goreleaser release --clean

lint:
	go vet ./...
