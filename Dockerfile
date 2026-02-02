# syntax=docker/dockerfile:1

#############################
# Build stage
#############################
FROM golang:1.24-alpine AS builder
WORKDIR /src

# Pre-cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY app/ ./app/

# Build-time version injection
ARG APP_VERSION=dev

# Build static Linux amd64 binary (default GitHub runner arch)
RUN CGO_ENABLED=0 GOOS=linux \
    go build -ldflags "-s -w -X main.Version=${APP_VERSION}" -o /out/fax-ui ./app

#############################
# Runtime stage
#############################
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

# Copy binary and templates
COPY --from=builder /out/fax-ui /app/fax-ui
COPY --from=builder /src/app/web /app/web

ENV PORT=8080
EXPOSE 8080

# Run as non-root for safety
USER nonroot:nonroot
ENTRYPOINT ["/app/fax-ui"]
