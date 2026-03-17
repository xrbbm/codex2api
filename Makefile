GO      := $(HOME)/go/bin/go
APP     := codex2api
LDFLAGS := -ldflags="-s -w"
GCFLAGS :=
UPX     := $(shell command -v upx 2>/dev/null)

.PHONY: build linux-amd64 linux-arm64 all clean

build:
	$(GO) build $(LDFLAGS) -trimpath -o $(APP) .

linux-amd64:
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -trimpath -o dist/$(APP)-linux-amd64 .
	$(if $(UPX),$(UPX) --best dist/$(APP)-linux-amd64,@echo "upx not found, skipping compression")

linux-arm64:
	@mkdir -p dist
	GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -trimpath -o dist/$(APP)-linux-arm64 .
	$(if $(UPX),$(UPX) --best dist/$(APP)-linux-arm64,@echo "upx not found, skipping compression")

all: linux-amd64 linux-arm64

clean:
	rm -rf dist/ $(APP)
