APP_NAME  := autohide
VERSION   := 0.1.0
BUILD_DIR := build

GOARCH  := $(shell go env GOARCH)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build clean tidy

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) .

clean:
	rm -rf $(BUILD_DIR)

tidy:
	go mod tidy
