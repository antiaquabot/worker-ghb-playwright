VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY    := worker-ghb-playwright
LDFLAGS   := -ldflags "-s -w -X main.Version=$(VERSION)"

PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

.PHONY: all build clean lint test dist

all: build

build:
	go build $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)
	rm -rf dist/

dist: $(PLATFORMS)

$(PLATFORMS):
	$(eval GOOS=$(word 1,$(subst /, ,$@)))
	$(eval GOARCH=$(word 2,$(subst /, ,$@)))
	$(eval EXT=$(if $(filter windows,$(GOOS)),.exe,))
	$(eval OUT=dist/$(BINARY)-$(GOOS)-$(GOARCH)$(EXT))
	@echo "Building $(OUT)..."
	@mkdir -p dist
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build $(LDFLAGS) -o $(OUT) .

.PHONY: $(PLATFORMS)
