APP_NAME     := autohide
HELPER_NAME  := autohide-helper
UI_NAME      := autohide-ui
VERSION      := 0.1.0
BUILD_DIR    := $(CURDIR)/build
APP_DIR      := /Applications/autohide.app
LEGACY_DIR   := $(HOME)/Applications/autohide.app
APP_BIN      := $(APP_DIR)/Contents/MacOS/$(APP_NAME)
GOBIN        := $(shell go env GOPATH)/bin

GOARCH  := $(shell go env GOARCH)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build build-cli build-helper build-ui icon install uninstall clean tidy

all: build

build: build-cli build-helper build-ui

build-cli:
	@mkdir -p $(BUILD_DIR)
	cd $(APP_NAME) && GOOS=darwin GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) .

build-helper:
	@mkdir -p $(BUILD_DIR)
	cd $(HELPER_NAME) && swift build -c release
	cp $(HELPER_NAME)/.build/release/$(HELPER_NAME) $(BUILD_DIR)/$(HELPER_NAME)

build-ui:
	@mkdir -p $(BUILD_DIR)
	cd $(UI_NAME) && swift build -c release --product $(UI_NAME)
	cp $(UI_NAME)/.build/release/$(UI_NAME) $(BUILD_DIR)/$(UI_NAME)

# Regenerates the committed assets/autohide.icns from scripts/make-icon.swift.
icon:
	@mkdir -p $(BUILD_DIR)
	rm -rf $(BUILD_DIR)/$(APP_NAME).iconset
	swift scripts/make-icon.swift $(BUILD_DIR)/$(APP_NAME).iconset
	iconutil -c icns $(BUILD_DIR)/$(APP_NAME).iconset -o assets/$(APP_NAME).icns

# Stop the old daemon (via the fresh build, so this works on first install),
# assemble the bundle, sign nested binaries then the bundle, and restart.
# The pkill catches daemons too old to honor IPC-shutdown takeover (they exit
# gracefully on SIGTERM); without it the new agent thrashes against them.
# The autohide-overlay pkill/rm reap what pre-removal installs shipped: the
# daemon-side orphan cleanup is gone, so a stale widget would float forever.
install: build
	$(BUILD_DIR)/$(APP_NAME) uninstall 2>/dev/null || true
	pkill -f '(^|/)autohide daemon' 2>/dev/null || true
	pkill -x autohide-overlay 2>/dev/null || true
	pkill -x $(UI_NAME) 2>/dev/null || true
	rm -f $(HOME)/.config/autohide/focus.json
	@mkdir -p $(APP_DIR)/Contents/MacOS $(APP_DIR)/Contents/Resources
	rm -f $(APP_BIN) $(APP_DIR)/Contents/MacOS/autohide-overlay $(APP_DIR)/Contents/MacOS/$(HELPER_NAME) $(APP_DIR)/Contents/MacOS/$(UI_NAME)
	cp $(BUILD_DIR)/$(APP_NAME) $(APP_BIN)
	cp $(BUILD_DIR)/$(HELPER_NAME) $(APP_DIR)/Contents/MacOS/$(HELPER_NAME)
	cp $(BUILD_DIR)/$(UI_NAME) $(APP_DIR)/Contents/MacOS/$(UI_NAME)
	cp assets/$(APP_NAME).icns $(APP_DIR)/Contents/Resources/$(APP_NAME).icns
	sed 's/@VERSION@/$(VERSION)/' assets/Info.plist.in > $(APP_DIR)/Contents/Info.plist
	codesign --force --sign - $(APP_DIR)/Contents/MacOS/$(HELPER_NAME)
	codesign --force --sign - $(APP_DIR)/Contents/MacOS/$(UI_NAME)
	codesign --force --sign - $(APP_DIR)
	rm -rf $(LEGACY_DIR)
	@mkdir -p $(GOBIN)
	ln -sf $(APP_BIN) $(GOBIN)/$(APP_NAME)
	$(APP_BIN) install
	@touch $(APP_DIR)
	@echo "Installed $(APP_NAME) to $(APP_DIR) (CLI symlinked to $(GOBIN)/$(APP_NAME))"

uninstall:
	@echo "Removing $(APP_NAME)..."
	$(APP_BIN) uninstall 2>/dev/null || true
	pkill -f '(^|/)autohide daemon' 2>/dev/null || true
	pkill -f 'autohide.app/Contents/MacOS/autohide' 2>/dev/null || true
	pkill -x autohide-overlay 2>/dev/null || true
	pkill -x $(UI_NAME) 2>/dev/null || true
	rm -f $(HOME)/.config/autohide/focus.json
	rm -f $(GOBIN)/$(APP_NAME)
	rm -rf $(APP_DIR) $(LEGACY_DIR)
	@echo "Done."

clean:
	rm -rf $(BUILD_DIR)
	-cd $(HELPER_NAME) && swift package clean 2>/dev/null || true
	-cd $(UI_NAME) && swift package clean 2>/dev/null || true

tidy:
	cd $(APP_NAME) && go mod tidy
