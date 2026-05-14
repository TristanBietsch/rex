.PHONY: build test lint clean

build:
	go build -o rex-daemon ./cmd/rex-daemon

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -f rex-daemon
