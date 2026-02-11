APP_NAME     := autohide
OVERLAY_NAME := autohide-overlay
VERSION      := 0.1.0
BUILD_DIR    := build
INSTALL_DIR  := /usr/local/bin

.PHONY: all build build-cli build-overlay install uninstall clean tidy

all: build

build: build-cli build-overlay

build-cli:
	@$(MAKE) -C $(APP_NAME) build VERSION=$(VERSION) BUILD_DIR=$(CURDIR)/$(BUILD_DIR)

build-overlay:
	cd $(OVERLAY_NAME) && swift build -c release
	@mkdir -p $(BUILD_DIR)
	@cp $(OVERLAY_NAME)/.build/release/$(OVERLAY_NAME) $(BUILD_DIR)/$(OVERLAY_NAME)

install: build
	@echo "Installing to $(INSTALL_DIR)..."
	@install -d $(INSTALL_DIR)
	@install -m 755 $(BUILD_DIR)/$(APP_NAME) $(INSTALL_DIR)/$(APP_NAME)
	@install -m 755 $(BUILD_DIR)/$(OVERLAY_NAME) $(INSTALL_DIR)/$(OVERLAY_NAME)
	@echo "Installed. Run 'autohide install' to enable auto-start on login."

uninstall:
	@echo "Removing $(APP_NAME) and $(OVERLAY_NAME)..."
	@-$(INSTALL_DIR)/$(APP_NAME) uninstall 2>/dev/null || true
	@rm -f $(INSTALL_DIR)/$(APP_NAME)
	@rm -f $(INSTALL_DIR)/$(OVERLAY_NAME)
	@echo "Done."

clean:
	rm -rf $(BUILD_DIR)
	@-cd $(OVERLAY_NAME) && swift package clean 2>/dev/null || true

tidy:
	@$(MAKE) -C $(APP_NAME) tidy
