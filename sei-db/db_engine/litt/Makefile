SHELL := /bin/bash


# Build the litt CLI tool.
build:
	go build -o ./bin/litt ./cli

# Remove the bin directory if it exists.
clean:
	rm -rf ./bin

# Build the litt CLI tool with debug flags.
debug-build: clean
	go mod tidy
	go build -gcflags "all=-N -l"  -o ./bin/litt ./cli

# Run all LittDB unit tests.
test: build
	go test ./... -timeout=10m -v -p=1 -parallel=8

# Run all LittDB unit tests with verbose output.
test-verbose: build
	go test ./... -v -timeout=10m -p=1 -parallel=8
