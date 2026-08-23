# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG VERSION=dev
ARG COMMIT=""
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Dependencies first so a source-only change reuses this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/swagger-merger ./cmd/swagger-merger

FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.title="swagger-merger" \
      org.opencontainers.image.description="Merge OpenAPI and Swagger documents into one" \
      org.opencontainers.image.source="https://github.com/efureev/go-swagger-merger" \
      org.opencontainers.image.licenses="MIT" \
      maintainer="Eugene Fureev <fureev@gmail.com>"

COPY --from=builder /out/swagger-merger /swagger-merger

WORKDIR /data
USER nonroot:nonroot

# The v1 image declared no entrypoint at all, so "docker run image" did nothing.
ENTRYPOINT ["/swagger-merger"]
CMD ["help"]
