.PHONY: build clean test run save restore check list docker-build docker-build-rescue docker-run help

BINARY := tanos-saver
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_TAGS := containers_image_openpgp exclude_graphdriver_btrfs exclude_graphdriver_devicemapper
LDFLAGS := -s -w -X main.version=$(VERSION)

help:
	@echo "Available targets:"
	@echo "  build               - Build binary"
	@echo "  clean               - Remove binary"
	@echo "  test                - Run tests"
	@echo "  save                - Run save command"
	@echo "  restore             - Run restore command"
	@echo "  check               - Run check command"
	@echo "  list                - Run list command"
	@echo "  docker-build        - Build docker image"
	@echo "  docker-build-rescue - Build rescue docker image"
	@echo "  docker-run          - Run in docker"

build:
	CGO_ENABLED=0 go build -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/tanos-saver

clean:
	rm -f $(BINARY)

test:
	go test -v ./...

run:
	go run -tags "$(BUILD_TAGS)" ./cmd/tanos-saver

save: build
	./$(BINARY) save

restore: build
	./$(BINARY) restore

check: build
	./$(BINARY) check

list: build
	./$(BINARY) list

docker-build:
	docker build -t $(BINARY):$(VERSION) .

docker-build-rescue:
	docker build -f Dockerfile.rescue -t tanos-rescue:latest .

docker-push-rescue: docker-build-rescue
	docker tag tanos-rescue:latest $(RESCUE_IMAGE)
	docker push $(RESCUE_IMAGE)

docker-run:
	docker run --rm -it \
		-v ~/.kube/config:/root/.kube/config:ro \
		-e NAMESPACES \
		-e REGISTRY_URL \
		-e REGISTRY_USER \
		-e REGISTRY_PASSWORD \
		$(BINARY):$(VERSION) $(CMD)

lint:
	golangci-lint run

fmt:
	go fmt ./...

.PHONY: dev-env
dev-env:
	docker-compose up -d
	@echo "MinIO console: http://localhost:9001 (minioadmin/minioadmin)"
