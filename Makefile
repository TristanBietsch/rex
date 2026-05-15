.PHONY: build build-all test lint clean install uninstall

# PREFIX is where binaries are installed (override with `make install PREFIX=...`).
PREFIX ?= $(HOME)/.local

build: build-all

build-all:
	go build -o rex-daemon ./cmd/rex-daemon
	go build -o rex ./cmd/rex

# install drops the rex and rex-daemon binaries into $(PREFIX)/bin.
# Default PREFIX is ~/.local — make sure ~/.local/bin is on your PATH.
install: build
	@mkdir -p "$(PREFIX)/bin"
	install -m 0755 rex "$(PREFIX)/bin/rex"
	install -m 0755 rex-daemon "$(PREFIX)/bin/rex-daemon"
	@echo
	@echo "  rex installed to $(PREFIX)/bin"
	@echo "  make sure $(PREFIX)/bin is on your PATH (e.g. add to ~/.zshrc):"
	@echo "    export PATH=\"$(PREFIX)/bin:\$$PATH\""
	@echo

uninstall:
	rm -f "$(PREFIX)/bin/rex" "$(PREFIX)/bin/rex-daemon"

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -f rex-daemon rex
