# Makefile for vtrace

BINARY_NAME := vtrace
DIST_DIR := dist
MAIN_PKG := ./cmd/vtrace

.PHONY: all build clean

# Builds the binary into dist folder.
all: build

# Compiles the Go binary and outputs to dist folder.
build:
	@mkdir -p $(DIST_DIR)
	go build -o $(DIST_DIR)/$(BINARY_NAME) $(MAIN_PKG)

# Removes the dist folder.
clean:
	rm -rf $(DIST_DIR)
