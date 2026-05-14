.PHONY: build build-all test lint clean

build: build-all

build-all:
	go build -o rex-daemon ./cmd/rex-daemon
	go build -o rex ./cmd/rex

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -f rex-daemon rex
