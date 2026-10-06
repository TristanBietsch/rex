.PHONY: build build-all test test-race vet lint vuln check clean install uninstall bench bench-integration bench-profile bench-check

# PREFIX is where binaries are installed (override with `make install PREFIX=...`).
PREFIX ?= $(HOME)/.local

# Lint tools run via `go run` at pinned versions so they're built with the
# repo's Go toolchain (a golangci-lint built with an older Go can't read this
# toolchain's export data).
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@v1.8.0

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

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run ./...

vuln:
	$(GOVULNCHECK) ./...

# check is the pre-release gate: vet, race-enabled tests, lint, known vulns.
check: vet test-race lint vuln

clean:
	rm -f rex-daemon rex

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

# SCENARIO: s1|s2|s3|s4|s5 — writes cpu+mem profiles under bench/profiles/
SCENARIO ?= s3
bench-profile:
	@mkdir -p bench/profiles
	@case "$(SCENARIO)" in \
	  s1) go test -tags=integration -bench=BenchmarkS1 -cpuprofile=bench/profiles/s1-cpu.prof -memprofile=bench/profiles/s1-mem.prof -count=1 ./bench/integration/... ;; \
	  s2) go test -tags=integration -bench=BenchmarkS2 -cpuprofile=bench/profiles/s2-cpu.prof -memprofile=bench/profiles/s2-mem.prof -count=1 ./bench/integration/... ;; \
	  s3) go test -bench=BenchmarkRenderBoard_50 -cpuprofile=bench/profiles/s3-cpu.prof -memprofile=bench/profiles/s3-mem.prof -count=1 ./internal/surface/tui/... ;; \
	  s4) go test -bench=BenchmarkCodec -cpuprofile=bench/profiles/s4-cpu.prof -memprofile=bench/profiles/s4-mem.prof -count=1 ./internal/wire/protocol/... ;; \
	  s5) go test -bench=BenchmarkLoadAll_200 -cpuprofile=bench/profiles/s5-cpu.prof -memprofile=bench/profiles/s5-mem.prof -count=1 ./internal/daemon/state/... ;; \
	  *) echo "unknown SCENARIO=$(SCENARIO)"; exit 1 ;; \
	esac

bench-check:
	@chmod +x bench/check_budgets.sh
	@./bench/check_budgets.sh

bench-compare:
	@echo "Usage: benchstat bench/results/baseline.txt bench/results/new.txt"
