SHELL := /bin/bash

TEMPL ?= templ
GO ?= go
BIN_DIR := .bin
NATS_BIN := $(BIN_DIR)/crawlerdb-nats
CORE_BIN := $(BIN_DIR)/crawlerdb-core
CRAWLER_BIN := $(BIN_DIR)/crawlerdb-crawler
GUI_BIN := $(BIN_DIR)/crawlerdb-gui
NATS_HOST ?= 127.0.0.1
NATS_PORT ?= 4222
DEBUG ?= 0
CRAWLER_FLAGS ?= $(if $(filter 1 true yes TRUE YES,$(DEBUG)),--debug,)

.PHONY: generate build run clean test

generate:
	$(TEMPL) generate

build: generate
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(NATS_BIN) ./cmd/nats
	$(GO) build -o $(CORE_BIN) ./cmd/core
	$(GO) build -o $(CRAWLER_BIN) ./cmd/crawler
	$(GO) build -o $(GUI_BIN) ./cmd/gui

run: build
	@echo "Starting nats, core, crawler, and gui"
	@cleanup() { \
		trap - INT TERM EXIT; \
		kill $$nats_pid $$core_pid $$crawler_pid $$gui_pid 2>/dev/null || true; \
		wait $$nats_pid $$core_pid $$crawler_pid $$gui_pid 2>/dev/null || true; \
	}; \
	$(NATS_BIN) & \
	nats_pid=$$!; \
	until (echo > /dev/tcp/$(NATS_HOST)/$(NATS_PORT)) >/dev/null 2>&1; do sleep 0.2; done; \
	$(CORE_BIN) & \
	core_pid=$$!; \
	$(CRAWLER_BIN) $(CRAWLER_FLAGS) & \
	crawler_pid=$$!; \
	$(GUI_BIN) & \
	gui_pid=$$!; \
	trap 'cleanup' INT TERM EXIT; \
	echo "nats pid=$$nats_pid"; \
	echo "core pid=$$core_pid"; \
	echo "crawler pid=$$crawler_pid"; \
	echo "gui pid=$$gui_pid"; \
	wait $$nats_pid $$core_pid $$crawler_pid $$gui_pid

test: generate
	$(GO) test ./...

clean:
	rm -rf $(BIN_DIR)
