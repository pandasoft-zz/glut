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

release:
	goreleaser release --snapshot --clean
