# Build, sign, and install the single `autohide` binary as /Applications/autohide.app.
#
#   make build     release build (.build/release/autohide)
#   make test      unit tests (AutohideCore)
#   make bundle    build/autohide.app with its compiled icon, signed with a stable identity
#   make install   replace /Applications/autohide.app and (re)start the LaunchAgent
#   make uninstall stop the LaunchAgent and remove the app; ~/.config/autohide is kept
#
# The app icon's source is assets/AppIcon.icon, an Icon Composer document
# (icon.json plus SVG layers). `bundle` compiles it with Xcode 26's actool into
# Contents/Resources: Assets.car, which macOS 26 draws with the system's glass,
# squircle mask and dark/tinted styles, and AppIcon.icns, the flattened fallback
# that macOS 13-15 read. build and test need only Swift; bundle and install
# need Xcode 26.
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

# Absolute on purpose: actool hands the .icon to a separate exporter process
# that cannot open a relative path, and then writes nothing yet still exits 0.
ICON_SRC    := $(CURDIR)/assets/AppIcon.icon
# actool's by-products stay outside the bundle, where they cannot break its seal.
ICON_PLIST  := $(BUILD_DIR)/AppIcon-partial.plist
ACTOOL_LOG  := $(BUILD_DIR)/actool.log

APP_DIR     := /Applications/$(APP_NAME).app
APP_BIN     := $(APP_DIR)/Contents/MacOS/$(APP_NAME)
CLI_LINK    := $(HOME)/go/bin/$(APP_NAME)
AGENT_LABEL := com.autohide.daemon
AGENT_PLIST := $(HOME)/Library/LaunchAgents/$(AGENT_LABEL).plist
LSREGISTER  := /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister

# First matching identity name from the keychain, or empty.
find_identity = $(shell security find-identity -v -p codesigning 2>/dev/null \
	| sed -n 's/^.*"\($(1):[^"]*\)"$$/\1/p' | head -n 1)

# SIGN_ID: Developer ID Application, else Apple Development, else ad-hoc.
# Override with `make bundle SIGN_ID=...`. The self-referencing eval makes the
# keychain lookup run once, and only for targets that sign.
SIGN_ID ?= $(eval SIGN_ID := $(or $(call find_identity,Developer ID Application),$(call find_identity,Apple Development),-))$(SIGN_ID)

.PHONY: all build test bundle install uninstall clean

all: build

build:
	swift build -c release

test:
	swift test

# The bundle is rebuilt from scratch each time: a stale extra file inside it
# would break the signature seal that `codesign --verify --strict` checks.
#
# actool's exit status is not trusted. It exits 0 having written nothing when
# its icon exporter fails (a malformed icon.json, a layer naming a missing SVG),
# and an Xcode older than 26 cannot read a .icon at all, so the icon step is
# judged by whether Assets.car and AppIcon.icns exist; Info.plist.in points at
# them through CFBundleIconName and CFBundleIconFile. A clean compile logs only
# its output list, so any warning or notice is printed even when the outputs
# exist: an edited icon cannot degrade quietly.
bundle: build
	rm -rf $(BUNDLE)
	mkdir -p $(BUNDLE)/Contents/MacOS $(BUNDLE)/Contents/Resources
	cp $(RELEASE_BIN) $(BUNDLE)/Contents/MacOS/$(APP_NAME)
	@echo "actool: compiling assets/AppIcon.icon"
	@xcrun actool $(ICON_SRC) --compile $(BUNDLE)/Contents/Resources \
		--platform macosx --minimum-deployment-target 13.0 --app-icon AppIcon \
		--target-device mac --output-partial-info-plist $(ICON_PLIST) \
		>$(ACTOOL_LOG) 2>&1; \
	if [ ! -s $(BUNDLE)/Contents/Resources/Assets.car ] \
	   || [ ! -s $(BUNDLE)/Contents/Resources/AppIcon.icns ]; then \
		cat $(ACTOOL_LOG) >&2; \
		echo "error: actool could not compile assets/AppIcon.icon (its output, if any, is above)." >&2; \
		echo "       A .icon needs actool from Xcode 26 or later; the active developer dir is $$(xcode-select -p 2>/dev/null || echo unset)." >&2; \
		echo "       Select Xcode 26 with: sudo xcode-select -s /Applications/Xcode.app" >&2; \
		exit 1; \
	fi; \
	if grep -qE 'com\.apple\.actool\.(errors|warnings|notices)' $(ACTOOL_LOG); then \
		cat $(ACTOOL_LOG) >&2; \
	fi
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
#   4. touch the bundle and force LaunchServices to re-register it. It does not
#      notice that a bundle replaced in place has a new icon, so Finder and the
#      Dock would keep drawing the cached old one;
#   5. the new binary writes its own plist and bootstraps it.
install: bundle
	-launchctl bootout gui/$$(id -u)/$(AGENT_LABEL) 2>/dev/null
	-pkill -f '/Applications/$(APP_NAME)[.]app/Contents/MacOS/'
	rm -rf $(APP_DIR) && ditto $(BUNDLE) $(APP_DIR)
	touch $(APP_DIR)
	$(LSREGISTER) -f $(APP_DIR)
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

clean:
	rm -rf .build $(BUILD_DIR)
