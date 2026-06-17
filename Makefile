APP_NAME := api
CMD_PATH := ./cmd/api
BIN_DIR := bin
BIN_PATH := $(BIN_DIR)/$(APP_NAME)
GOCACHE ?= /tmp/xadrez-go-build-cache

.PHONY: build-debug build-release test clean

build-debug:
	@mkdir -p $(BIN_DIR)
	GOCACHE=$(GOCACHE) go build -gcflags="all=-N -l" -o $(BIN_PATH)-debug $(CMD_PATH)

build-release:
	@mkdir -p $(BIN_DIR)
	GOCACHE=$(GOCACHE) go build -trimpath -ldflags="-s -w" -o $(BIN_PATH) $(CMD_PATH)

test:
	GOCACHE=$(GOCACHE) go test -count=1 ./...

clean:
	rm -rf $(BIN_DIR)
