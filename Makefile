.PHONY: build test check release
build:
	go build -buildvcs=false -trimpath -o bin/chatwoot-cli .
test:
	go test -race ./...
check:
	test -z "$$(gofmt -l *.go)"
	go vet ./...
	go test -race ./...
release:
	bash scripts/package.sh
