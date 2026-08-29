APP := ArgentWatch
PKG := ./cmd/argentwatch
GO_DRIVER := ./scripts/go-toolchain.sh
ZGO ?= zgo
ZIG ?= zig
GO ?= go

.PHONY: build test vet clean install toolchain

build:
	ZGO="$(ZGO)" ZIG="$(ZIG)" GO="$(GO)" $(GO_DRIVER) build -trimpath -ldflags="-s -w" -o $(APP) $(PKG)

test:
	ZGO="$(ZGO)" ZIG="$(ZIG)" GO="$(GO)" $(GO_DRIVER) test ./...

vet:
	ZGO="$(ZGO)" ZIG="$(ZIG)" GO="$(GO)" $(GO_DRIVER) vet ./...

# Show the exact build path without compiling anything.
toolchain:
	@ZGO="$(ZGO)" ZIG="$(ZIG)" GO="$(GO)" $(GO_DRIVER) --print

install: build
	install -Dm755 $(APP) $(HOME)/bin/$(APP)

clean:
	rm -f $(APP)
