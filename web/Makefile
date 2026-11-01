VERSION ?= 0.1.0
PREFIX  ?= /usr/local
BIN     ?= bin/rsync-web

.PHONY: all build test vet fmt install uninstall clean

all: build

build:
	mkdir -p $(dir $(BIN))
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/rsync-web

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal ./presets

install: build
	install -Dm755 $(BIN) $(DESTDIR)$(PREFIX)/bin/rsync-web

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/rsync-web

clean:
	rm -rf bin
