# Build from nexus/proxy (no application data or media in this context).
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -o /out/ptmove-media-agent .

FROM debian:bookworm-slim
ARG APT_MIRROR=http://deb.debian.org
RUN sed -i "s|http://deb.debian.org|${APT_MIRROR}|g" /etc/apt/sources.list.d/debian.sources && apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg mpv mediainfo && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ptmove-media-agent /usr/local/bin/ptmove-media-agent
ENV LANG=C.UTF-8 PTMOVE_LISTEN=0.0.0.0:19090 HOME=/tmp PTNEXUS_DATA_DIR=/tmp/ptmove
USER 65534:65534
EXPOSE 19090
ENTRYPOINT ["/usr/local/bin/ptmove-media-agent"]
