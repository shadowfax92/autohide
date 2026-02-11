APP_NAME := autohide
VERSION  := 0.1.0
BUILD_DIR := build
INSTALL_DIR := /usr/local/bin

GOARCH  := $(shell go env GOARCH)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build install uninstall clean tidy

all: build

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) .

install: build
	@echo "Installing $(APP_NAME) to $(INSTALL_DIR)..."
	@install -d $(INSTALL_DIR)
	@install -m 755 $(BUILD_DIR)/$(APP_NAME) $(INSTALL_DIR)/$(APP_NAME)
	@echo "Installed. Run 'autohide install' to enable auto-start on login."

uninstall:
	@echo "Removing $(APP_NAME)..."
	@-$(INSTALL_DIR)/$(APP_NAME) uninstall 2>/dev/null || true
	@rm -f $(INSTALL_DIR)/$(APP_NAME)
	@echo "Done."

clean:
	rm -rf $(BUILD_DIR)

tidy:
	go mod tidy
