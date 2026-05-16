VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS  = -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"
COVER_PACKAGES = ./cmd/glut ./internal/... ./schema

build:
	go build $(LDFLAGS) -o glut ./cmd/glut

test:
	go test ./...

test-cover:
	go test $(COVER_PACKAGES) -covermode=atomic -coverprofile=coverage.out
	go tool cover -func=coverage.out

test-cover-check:
	go test $(COVER_PACKAGES) -covermode=atomic -coverprofile=coverage.out
	sh ./scripts/check-coverage.sh coverage.out 90

test-cover-html:
	go test $(COVER_PACKAGES) -covermode=atomic -coverprofile=coverage.out
	go tool cover -html=coverage.out

lint:
	golangci-lint run

docker:
	docker build -t glut:dev .

INCONTAINER := $(shell [ -f /.dockerenv ] && echo 1 || echo 0)
DOCKER_TEST_CONFIG ?= /tmp/glut-docker-config
GLUT_RUN_FLAGS ?= --verbose --copy-strategy=auto

ifeq ($(INCONTAINER),1)
test-integration: build
	@mkdir -p $(DOCKER_TEST_CONFIG) && printf '{}' > $(DOCKER_TEST_CONFIG)/config.json
	DOCKER_CONFIG=$(DOCKER_TEST_CONFIG) ./glut run $(GLUT_RUN_FLAGS) ./tests/passing/
	@if DOCKER_CONFIG=$(DOCKER_TEST_CONFIG) ./glut run $(GLUT_RUN_FLAGS) ./tests/failing/; then \
		echo "Expected tests to fail but they passed"; exit 1; \
	fi
else
test-integration: docker
	docker run --rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$(PWD):/repo" \
		-w /repo \
		glut:dev run $(GLUT_RUN_FLAGS) ./tests/passing/
	@if docker run --rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$(PWD):/repo" \
		-w /repo \
		glut:dev run $(GLUT_RUN_FLAGS) ./tests/failing/; then \
		echo "Expected tests to fail but they passed"; exit 1; \
	fi
endif

release:
	goreleaser release --snapshot --clean
