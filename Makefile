GOOS = $(shell go env GOOS)
GOARCH = $(shell go env GOARCH)
BUILD_DIR = dist/${GOOS}_${GOARCH}
GENERATED_CONF := pkg/config/conf.gen.go

ifeq ($(GOOS),windows)
OUTPUT_PATH = ${BUILD_DIR}/baton-zoom.exe
else
OUTPUT_PATH = ${BUILD_DIR}/baton-zoom
endif

# Set the build tag conditionally based on ENABLE_LAMBDA
ifdef BATON_LAMBDA_SUPPORT
	BUILD_TAGS = -tags baton_lambda_support
else
	BUILD_TAGS =
endif

.PHONY: build
build: $(GENERATED_CONF)
	go build $(BUILD_TAGS) -o $(OUTPUT_PATH) ./cmd/baton-zoom

$(GENERATED_CONF): pkg/config/config.go
	@echo "Generating $(GENERATED_CONF)..."
	go generate ./pkg/config

.PHONY: update-deps
update-deps:
	go get -u ./...
	go mod tidy
	go mod vendor

.PHONY: add-deps
add-deps:
	go mod tidy
	go mod vendor

.PHONY: lint
lint:
	golangci-lint run ./...
