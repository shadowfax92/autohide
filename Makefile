# Build and test the Swift package at the repo root.

BUILD_DIR := $(CURDIR)/build

.PHONY: all build test icon clean

all: build

build:
	swift build -c release

test:
	swift test

# Regenerates the committed assets/autohide.icns from scripts/make-icon.swift.
icon:
	@mkdir -p $(BUILD_DIR)
	rm -rf $(BUILD_DIR)/autohide.iconset
	swift scripts/make-icon.swift $(BUILD_DIR)/autohide.iconset
	iconutil -c icns $(BUILD_DIR)/autohide.iconset -o assets/autohide.icns

clean:
	rm -rf .build $(BUILD_DIR)
