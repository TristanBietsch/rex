.PHONY: build build-all install uninstall dist \
	test test-race test-e2e vet lint lint-windows vuln \
	cover cover-check fuzz check clean \
	bench bench-integration bench-profile bench-check bench-compare

# PREFIX is where binaries are installed (override with `make install PREFIX=...`).
PREFIX ?= $(HOME)/.local

# Lint tools run via `go run` at pinned versions so they're built with the
# repo's Go toolchain (a golangci-lint built with an older Go can't read this
# toolchain's export data).
GOLANGCI_LINT_PKG := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GOLANGCI_LINT ?= go run $(GOLANGCI_LINT_PKG)
# lint-windows type-checks the Windows build. GOOS must reach the linter but
# not the `go run` build of it, so it goes through -exec. With a prebuilt
# binary: make lint-windows GOLANGCI_LINT_WINDOWS='env GOOS=windows golangci-lint'
GOLANGCI_LINT_WINDOWS ?= go run -exec 'env GOOS=windows' $(GOLANGCI_LINT_PKG)
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@v1.8.0

# VERSION is stamped into both binaries (rex --version). Release builds pass
# the tag (tools/release/build.sh); local builds use git describe, or "dev".
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VERSION_LDFLAGS := -X github.com/tristanbietsch/rex/internal/version.Version=$(VERSION)

# FUZZTIME is how long `make fuzz` runs each fuzz target.
FUZZTIME ?= 30s

build: build-all

build-all:
	go build -ldflags '$(VERSION_LDFLAGS)' -o rex-daemon ./cmd/rex-daemon
	go build -ldflags '$(VERSION_LDFLAGS)' -o rex ./cmd/rex

# dist builds the release archives and SHA256SUMS into dist/ for the
# targets this host can build (see tools/release/build.sh).
# Example: make dist VERSION=v2.0.0
dist:
	tools/release/build.sh $(VERSION)

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
	go -C third_party/vt10x test ./...

test-race:
	go test -race ./...
	go -C third_party/vt10x test -race ./...

# test-e2e runs the end-to-end suite under e2e/. It is a no-op until that
# directory exists.
test-e2e:
	@if [ -d e2e ]; then \
		go test ./e2e/...; \
	else \
		echo "test-e2e: no e2e/ directory; skipping"; \
	fi

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run ./...

lint-windows:
	$(GOLANGCI_LINT_WINDOWS) run ./...

vuln:
	$(GOVULNCHECK) ./...

# cover writes a merged profile to coverage.out. go test prints the
# per-package summary; the last line is the total.
cover:
	go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -n 1

# cover-check fails when a package drops below its floor in
# tools/coverage/baseline.txt.
cover-check:
	@sh tools/coverage/check.sh

# fuzz runs every Fuzz* target in every package for FUZZTIME each. go test
# takes -fuzz for one package and one target at a time, so targets are
# discovered with -list first. Packages without fuzz targets are skipped.
fuzz:
	@set -e; \
	for pkg in $$(go list ./...); do \
		list=$$(go test -list '^Fuzz' "$$pkg"); \
		for fn in $$(printf '%s\n' "$$list" | grep '^Fuzz' || true); do \
			echo "==> $$pkg $$fn ($(FUZZTIME))"; \
			go test -run='^$$' -fuzz="^$$fn\$$" -fuzztime=$(FUZZTIME) "$$pkg"; \
		done; \
	done

# check is the pre-release gate: vet, race-enabled tests, lint (host and
# Windows), coverage floors, known vulns.
check: vet test-race lint lint-windows cover-check vuln

clean:
	rm -f rex-daemon rex coverage.out

# Unit benchmarks (S3–S5 + persist). Excludes integration-tagged package.
bench:
	@mkdir -p bench/results
	go test -bench=. -benchmem -count=5 -timeout=30m \
		./internal/daemon/state/... \
		./internal/wire/protocol/... \
		./internal/surface/tui/... \
		| tee bench/results/unit-$$(date +%Y%m%d-%H%M%S).txt

bench-integration:
	@mkdir -p bench/results
	go test -tags=integration -bench=. -benchmem -count=3 -timeout=30m \
		./bench/integration/... \
		| tee bench/results/integration-$$(date +%Y%m%d-%H%M%S).txt

# SCENARIO: s1|s2|s3|s4|s5|s6 — writes cpu+mem profiles under bench/profiles/
SCENARIO ?= s3
bench-profile:
	@mkdir -p bench/profiles
	@case "$(SCENARIO)" in \
	  s1) go test -tags=integration -bench=BenchmarkS1 -cpuprofile=bench/profiles/s1-cpu.prof -memprofile=bench/profiles/s1-mem.prof -count=1 ./bench/integration/... ;; \
	  s2) go test -tags=integration -bench=BenchmarkS2 -cpuprofile=bench/profiles/s2-cpu.prof -memprofile=bench/profiles/s2-mem.prof -count=1 ./bench/integration/... ;; \
	  s3) go test -bench=BenchmarkRenderBoard_50 -cpuprofile=bench/profiles/s3-cpu.prof -memprofile=bench/profiles/s3-mem.prof -count=1 ./internal/surface/tui/... ;; \
	  s4) go test -bench=BenchmarkCodec -cpuprofile=bench/profiles/s4-cpu.prof -memprofile=bench/profiles/s4-mem.prof -count=1 ./internal/wire/protocol/... ;; \
	  s5) go test -bench=BenchmarkLoadAll_200 -cpuprofile=bench/profiles/s5-cpu.prof -memprofile=bench/profiles/s5-mem.prof -count=1 ./internal/daemon/state/... ;; \
	  s6) go test -bench=BenchmarkAttachFrame -cpuprofile=bench/profiles/s6-cpu.prof -memprofile=bench/profiles/s6-mem.prof -count=1 ./internal/surface/tui/... ;; \
	  *) echo "unknown SCENARIO=$(SCENARIO)"; exit 1 ;; \
	esac

bench-check:
	@chmod +x bench/check_budgets.sh
	@./bench/check_budgets.sh

bench-compare:
	@echo "Usage: benchstat bench/results/baseline.txt bench/results/new.txt"
