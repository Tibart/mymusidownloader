.PHONY: test build build-arm64

test:
	go test ./...

build:
	mkdir -p bin
	go build -o bin/mymusidownloader ./cmd/mymusidownloader

build-arm64:
	mkdir -p bin/linux-arm64
	GOOS=linux GOARCH=arm64 go build -o bin/linux-arm64/mymusidownloader ./cmd/mymusidownloader
