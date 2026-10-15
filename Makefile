# Build, sign, and install the single `autohide` binary as /Applications/autohide.app.
#
#   make build     release build (.build/release/autohide)
#   make test      unit tests (AutohideCore)
#   make bundle    build/autohide.app, signed with a stable identity
#   make install   replace /Applications/autohide.app and (re)start the LaunchAgent
#   make uninstall stop the LaunchAgent and remove the app; ~/.config/autohide is kept
#
# Signing is what keeps permissions alive. An ad-hoc signature (`-`) makes the
# designated requirement the binary's CDHash, so every rebuild silently voids
# any TCC grant (diagnosis R1). A certificate identity makes it
# `identifier "com.autohide.daemon" and ... certificate leaf[subject.OU] = TEAM`,
# which every rebuild still satisfies.

APP_NAME    := autohide
IDENTIFIER  := com.autohide.daemon
# Version.swift is the single source of truth for the version string.
VERSION     := $(shell sed -n 's/.*current = "\([^"]*\)".*/\1/p' Sources/AutohideCore/Version.swift)

BUILD_DIR   := $(CURDIR)/build
BUNDLE      := $(BUILD_DIR)/$(APP_NAME).app
RELEASE_BIN := .build/release/$(APP_NAME)

APP_DIR     := /Applications/$(APP_NAME).app
APP_BIN     := $(APP_DIR)/Contents/MacOS/$(APP_NAME)
CLI_LINK    := $(HOME)/go/bin/$(APP_NAME)
AGENT_LABEL := com.autohide.daemon
AGENT_PLIST := $(HOME)/Library/LaunchAgents/$(AGENT_LABEL).plist

# First matching identity name from the keychain, or empty.
find_identity = $(shell security find-identity -v -p codesigning 2>/dev/null \
	| sed -n 's/^.*"\($(1):[^"]*\)"$$/\1/p' | head -n 1)

# SIGN_ID: Developer ID Application, else Apple Development, else ad-hoc.
# Override with `make bundle SIGN_ID=...`. The self-referencing eval makes the
# keychain lookup run once, and only for targets that sign.
SIGN_ID ?= $(eval SIGN_ID := $(or $(call find_identity,Developer ID Application),$(call find_identity,Apple Development),-))$(SIGN_ID)

.PHONY: all build test bundle install uninstall icon clean

all: build

build:
	swift build -c release

test:
	swift test

# The bundle is rebuilt from scratch each time: a stale extra file inside it
# would break the signature seal that `codesign --verify --strict` checks.
bundle: build
	rm -rf $(BUNDLE)
	mkdir -p $(BUNDLE)/Contents/MacOS $(BUNDLE)/Contents/Resources
	cp $(RELEASE_BIN) $(BUNDLE)/Contents/MacOS/$(APP_NAME)
	cp assets/$(APP_NAME).icns $(BUNDLE)/Contents/Resources/$(APP_NAME).icns
	sed 's/@VERSION@/$(VERSION)/g' assets/Info.plist.in > $(BUNDLE)/Contents/Info.plist
	plutil -lint -s $(BUNDLE)/Contents/Info.plist
	@if [ "$(SIGN_ID)" = "-" ]; then \
		echo "" >&2; \
		echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!" >&2; \
		echo "!! WARNING: no signing certificate found; signing AD-HOC (-).          !!" >&2; \
		echo "!! Permissions such as Accessibility will NOT survive the next rebuild. !!" >&2; \
		echo "!! Install a Developer ID or Apple Development certificate, or pass     !!" >&2; \
		echo "!! SIGN_ID=\"<identity>\".                                                !!" >&2; \
		echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!" >&2; \
		echo "" >&2; \
	else \
		echo "Signing with: $(SIGN_ID)"; \
	fi
	codesign --force --sign "$(SIGN_ID)" --identifier $(IDENTIFIER) --timestamp=none $(BUNDLE)
	codesign --verify --strict $(BUNDLE)
	codesign -d -r- $(BUNDLE)

# Order matters:
#   1. bootout first, so launchd's KeepAlive cannot respawn what step 2 kills;
#   2. pkill catches whatever is left from the old Go install (daemon, its
#      autohide-helper, autohide-ui). `[.]` keeps the pattern from matching
#      this recipe's own shell, whose command line contains the pattern text;
#   3. replace the bundle wholesale with ditto (no stale files, intact seal);
#   4. the new binary writes its own plist and bootstraps it.
install: bundle
	-launchctl bootout gui/$$(id -u)/$(AGENT_LABEL) 2>/dev/null
	-pkill -f '/Applications/$(APP_NAME)[.]app/Contents/MacOS/'
	rm -rf $(APP_DIR) && ditto $(BUNDLE) $(APP_DIR)
	mkdir -p $(dir $(CLI_LINK))
	ln -sf $(APP_BIN) $(CLI_LINK)
	$(APP_BIN) install
	@echo "Installed $(APP_DIR); CLI linked at $(CLI_LINK)"

# Reverse of install. ~/.config/autohide (config and logs) is left alone.
uninstall:
	-[ -x $(APP_BIN) ] && $(APP_BIN) uninstall
	-launchctl bootout gui/$$(id -u)/$(AGENT_LABEL) 2>/dev/null
	rm -f $(AGENT_PLIST)
	-pkill -f '/Applications/$(APP_NAME)[.]app/Contents/MacOS/'
	-[ -L $(CLI_LINK) ] && rm -f $(CLI_LINK)
	rm -rf $(APP_DIR)
	@echo "Removed $(APP_DIR); config and logs are kept in ~/.config/autohide"

# Regenerates the committed assets/autohide.icns from scripts/make-icon.swift.
icon:
	@mkdir -p $(BUILD_DIR)
	rm -rf $(BUILD_DIR)/$(APP_NAME).iconset
	swift scripts/make-icon.swift $(BUILD_DIR)/$(APP_NAME).iconset
	iconutil -c icns $(BUILD_DIR)/$(APP_NAME).iconset -o assets/$(APP_NAME).icns

clean:
	rm -rf .build $(BUILD_DIR)
