# Build the manager binary
FROM golang:1.23 as builder

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN ["go", "mod", "download"]

# Copy the go source
COPY api api
COPY api-hub api-hub
COPY cmd cmd
COPY internal internal

# Copy Makefile
COPY Makefile Makefile
COPY docs.mk docs.mk

# Copy the .git directory which is needed to store the build info
COPY .git .git

ARG TARGET

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 make ${TARGET}

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /

ARG TARGET

COPY --from=builder /workspace/${TARGET} /usr/local/bin/manager

ENTRYPOINT ["/usr/local/bin/manager"]
