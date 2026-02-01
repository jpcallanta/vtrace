# Makefile for vtrace

DIST_DIR := dist

.PHONY: all build clean

# Builds all binaries into dist folder.
all: build

# Compiles all Go binaries and outputs to dist folder.
build:
	@mkdir -p $(DIST_DIR)
	go build -o $(DIST_DIR)/vtrace ./cmd/vtrace
	go build -o $(DIST_DIR)/atrace ./cmd/atrace

# Removes the dist folder.
clean:
	rm -rf $(DIST_DIR)
