FROM ubuntu:24.04 AS builder
RUN apt-get update && apt-get install -y golang-go
WORKDIR /src
COPY . .
RUN go build -o /glut ./cmd/glut

FROM ubuntu:24.04
ARG GCL_VERSION=4.55.0
RUN apt-get update && apt-get install -y \
    nodejs npm git bash rsync \
    && npm install -g gitlab-ci-local@${GCL_VERSION} \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /glut /usr/local/bin/glut
ENTRYPOINT ["/usr/local/bin/glut"]
