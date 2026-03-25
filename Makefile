APP_NAME     := autohide
OVERLAY_NAME := autohide-overlay
VERSION      := 0.1.0
BUILD_DIR    := $(CURDIR)/build
INSTALL_DIR  := /usr/local/bin
APP_DIR      := $(HOME)/Applications/autohide.app
GOBIN        := $(shell go env GOPATH)/bin

GOARCH  := $(shell go env GOARCH)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build build-cli build-overlay build-app install uninstall clean tidy

all: build

build: build-cli build-overlay

build-cli:
	@mkdir -p $(BUILD_DIR)
	cd $(APP_NAME) && GOOS=darwin GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) .

build-overlay:
	@mkdir -p $(BUILD_DIR)
	cd $(OVERLAY_NAME) && swift build -c release
	cp $(OVERLAY_NAME)/.build/release/$(OVERLAY_NAME) $(BUILD_DIR)/$(OVERLAY_NAME)

build-app: build
	@mkdir -p $(APP_DIR)/Contents/MacOS $(APP_DIR)/Contents/Resources
	cp $(BUILD_DIR)/$(APP_NAME) $(APP_DIR)/Contents/MacOS/$(APP_NAME)
	cp $(BUILD_DIR)/$(OVERLAY_NAME) $(APP_DIR)/Contents/MacOS/$(OVERLAY_NAME)
	codesign --force --sign - $(APP_DIR)/Contents/MacOS/$(APP_NAME)
	@printf '<?xml version="1.0" encoding="UTF-8"?>\n\
	<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"\n\
	  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n\
	<plist version="1.0">\n\
	<dict>\n\
	    <key>CFBundleIdentifier</key>\n\
	    <string>com.autohide.daemon</string>\n\
	    <key>CFBundleName</key>\n\
	    <string>autohide</string>\n\
	    <key>CFBundleExecutable</key>\n\
	    <string>autohide</string>\n\
	    <key>LSUIElement</key>\n\
	    <true/>\n\
	</dict>\n\
	</plist>\n' > $(APP_DIR)/Contents/Info.plist

install: build-app
	@echo "Installing autohide.app..."
	ln -sf $(APP_DIR)/Contents/MacOS/$(APP_NAME) $(GOBIN)/$(APP_NAME)
	install -m 755 $(BUILD_DIR)/$(OVERLAY_NAME) $(INSTALL_DIR)/$(OVERLAY_NAME)
	-$(APP_DIR)/Contents/MacOS/$(APP_NAME) uninstall 2>/dev/null || true
	$(APP_DIR)/Contents/MacOS/$(APP_NAME) install
	@echo "Installed $(APP_NAME) to $(APP_DIR) (CLI symlinked to $(GOBIN)/$(APP_NAME))"

uninstall:
	@echo "Removing $(APP_NAME) and $(OVERLAY_NAME)..."
	-$(GOBIN)/$(APP_NAME) uninstall 2>/dev/null || true
	rm -f $(GOBIN)/$(APP_NAME)
	rm -f $(INSTALL_DIR)/$(OVERLAY_NAME)
	rm -rf $(APP_DIR)
	@echo "Done."

clean:
	rm -rf $(BUILD_DIR)
	-cd $(OVERLAY_NAME) && swift package clean 2>/dev/null || true

tidy:
	cd $(APP_NAME) && go mod tidy
