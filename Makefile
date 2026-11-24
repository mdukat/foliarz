BINARY ?= foliarz
GO ?= go

# CGO disabled + static tags produces a self-contained binary that runs on
# both musl-based (Alpine) and glibc-based (Debian) Linux.
STATIC_ENV := CGO_ENABLED=0
BUILD_TAGS := netgo,osusergo
LDFLAGS := -s -w

.PHONY: build test vet fmt check clean

build:
	$(STATIC_ENV) $(GO) build -trimpath -tags $(BUILD_TAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	@files="$$(gofmt -l .)"; if [ -n "$$files" ]; then echo "$$files"; exit 1; fi

check: fmt vet test

clean:
	rm -f $(BINARY)
