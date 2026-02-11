APP_NAME     := autohide
OVERLAY_NAME := autohide-overlay
VERSION      := 0.1.0
BUILD_DIR    := $(CURDIR)/build
INSTALL_DIR  := /usr/local/bin

GOARCH  := $(shell go env GOARCH)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build build-cli build-overlay install uninstall clean tidy

all: build

build: build-cli build-overlay

build-cli:
	@mkdir -p $(BUILD_DIR)
	cd $(APP_NAME) && CGO_ENABLED=0 GOOS=darwin GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) .

build-overlay:
	@mkdir -p $(BUILD_DIR)
	cd $(OVERLAY_NAME) && swift build -c release
	cp $(OVERLAY_NAME)/.build/release/$(OVERLAY_NAME) $(BUILD_DIR)/$(OVERLAY_NAME)

install: build
	@echo "Installing to $(INSTALL_DIR)..."
	install -d $(INSTALL_DIR)
	install -m 755 $(BUILD_DIR)/$(APP_NAME) $(INSTALL_DIR)/$(APP_NAME)
	install -m 755 $(BUILD_DIR)/$(OVERLAY_NAME) $(INSTALL_DIR)/$(OVERLAY_NAME)
	@echo "Installed. Run 'autohide install' to enable auto-start on login."

uninstall:
	@echo "Removing $(APP_NAME) and $(OVERLAY_NAME)..."
	-$(INSTALL_DIR)/$(APP_NAME) uninstall 2>/dev/null || true
	rm -f $(INSTALL_DIR)/$(APP_NAME)
	rm -f $(INSTALL_DIR)/$(OVERLAY_NAME)
	@echo "Done."

clean:
	rm -rf $(BUILD_DIR)
	-cd $(OVERLAY_NAME) && swift package clean 2>/dev/null || true

tidy:
	cd $(APP_NAME) && go mod tidy
