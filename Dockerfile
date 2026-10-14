# syntax=docker/dockerfile:1

# gopkgreg registry server. The SQLite driver is pure Go, so the binaries
# are static and the runtime image has no shell or package manager.
FROM golang:1.27.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=""
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w -X github.com/gopkgreg/gopkgreg/internal/version.Version=${VERSION}" \
        -o /out/gopkgregd ./cmd/gopkgregd && \
    go build -trimpath -ldflags "-s -w -X github.com/gopkgreg/gopkgreg/internal/version.Version=${VERSION}" \
        -o /out/gopkgreg ./cmd/gopkgreg && \
    mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gopkgregd /out/gopkgreg /usr/local/bin/
COPY --from=build --chown=nonroot:nonroot /out/data /data
# Every flag can be set as GOPKGREG_<FLAG>; see deploy/gopkgreg.env.example.
ENV GOPKGREG_ADDR=:8080 \
    GOPKGREG_DB=/data/gopkgreg.db \
    GOPKGREG_BLOBS=/data/blobs
VOLUME /data
EXPOSE 8080
USER nonroot
HEALTHCHECK --interval=30s --timeout=5s CMD ["/usr/local/bin/gopkgregd", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/gopkgregd"]
