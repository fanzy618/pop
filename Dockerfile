ARG GO_BUILDER_IMAGE=golang:1.25.5-bookworm

FROM --platform=$BUILDPLATFORM ${GO_BUILDER_IMAGE} AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=unknown

ENV GOTOOLCHAIN=local

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags "-s -w -X github.com/fanzy618/pop/internal/buildinfo.Version=$VERSION" -o /out/pop ./cmd/pop

RUN mkdir -p /out/data && chown -R 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/pop /pop
COPY --from=build --chown=65532:65532 /out/data /data

ENV POP_PROXY_LISTEN=0.0.0.0:5128 \
    POP_CONSOLE_LISTEN=0.0.0.0:5080 \
    POP_SQLITE_PATH=/data/pop.sqlite

EXPOSE 5128 5080

ENTRYPOINT ["/pop"]
