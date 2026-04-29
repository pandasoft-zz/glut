VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS  = -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"

build:
	go build $(LDFLAGS) -o glut ./cmd/glut

test:
	go test ./...

lint:
	golangci-lint run

docker:
	docker build -t glut:dev .

release:
	goreleaser release --snapshot --clean
