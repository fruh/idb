MODULE  := github.com/fruh/idb
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%d)
LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) \
           -X $(MODULE)/internal/version.Commit=$(COMMIT) \
           -X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: build install test dist
build:
	go build -ldflags "$(LDFLAGS)" -o bin/idb .
install:
	go install -ldflags "$(LDFLAGS)" .
test:
	go vet ./... && go test ./...
dist:
	for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do \
	  GOOS=$${t%/*} GOARCH=$${t#*/} CGO_ENABLED=0 go build -trimpath -ldflags "-s -w $(LDFLAGS)" \
	    -o dist/idb-$${t%/*}-$${t#*/}$$( [ $${t%/*} = windows ] && echo .exe ) . ; done
