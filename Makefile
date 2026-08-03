BINARY := uplet
PKG    := ./cmd/uplet
BINDIR := build

.PHONY: build run test install clean

build:
	go build -o $(BINDIR)/$(BINARY) $(PKG)

run:
	go run $(PKG) $(ARGS)

test:
	go test ./...

install:
	go install $(PKG)

clean:
	rm -rf $(BINDIR)
