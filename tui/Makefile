VERSION ?= 0.2.0
PREFIX  ?= $(HOME)/.local
BIN     ?= bin/rsync-tui

.PHONY: all build test vet fmt install uninstall clean

all: build

build:
	mkdir -p $(dir $(BIN))
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/rsync-tui

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal

install: build
	install -Dm755 $(BIN) $(DESTDIR)$(PREFIX)/bin/rsync-tui

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/rsync-tui

clean:
	rm -rf bin
