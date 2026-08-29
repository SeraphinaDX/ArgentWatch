APP := ArgentWatch
PKG := ./cmd/argentwatch

.PHONY: build test vet clean install

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(APP) $(PKG)

test:
	CGO_ENABLED=0 go test ./...

vet:
	CGO_ENABLED=0 go vet ./...

install: build
	install -Dm755 $(APP) $(HOME)/bin/$(APP)

clean:
	rm -f $(APP)
