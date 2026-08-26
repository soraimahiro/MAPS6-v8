APP       := maps6d
CTL       := maps6ctl
VERSION   := 8.0.0
GOARCH    := arm
GOARM     := 7
GOOS      := linux
LDFLAGS   := -ldflags "-s -w -X main.Version=$(VERSION)"
RELEASE   := maps6-$(VERSION)-arm

.PHONY: build test release clean

build:
	go build $(LDFLAGS) -o bin/$(APP) ./cmd/maps6d/
	go build $(LDFLAGS) -o bin/$(CTL) ./cmd/maps6ctl/

test:
	go test -v ./internal/...

release: clean test
	@echo "=== Cross-compiling for ARM ==="
	GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) go build $(LDFLAGS) -o release/$(RELEASE)/maps6d ./cmd/maps6d/
	GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) go build $(LDFLAGS) -o release/$(RELEASE)/maps6ctl ./cmd/maps6ctl/
	cp configs/maps6.yaml release/$(RELEASE)/
	cp configs/maps6d.service release/$(RELEASE)/
	cp scripts/install.sh release/$(RELEASE)/
	chmod +x release/$(RELEASE)/install.sh
	cd release && tar czf $(RELEASE).tar.gz $(RELEASE)/
	@echo "Release: release/$(RELEASE).tar.gz"

clean:
	rm -rf bin/ release/
