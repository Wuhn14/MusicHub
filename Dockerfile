# syntax=docker/dockerfile:1

# Stage 1: build the web interface (output goes to internal/web/dist).
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: build the static Go binary. The build always runs on the host
# architecture and cross-compiles, which keeps multi-arch builds fast.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/musichub ./cmd/musichub \
    && mkdir -p /out/data /out/music

# Stage 3: runtime image. Alpine provides ffmpeg/ffprobe and fpcalc (Chromaprint),
# which MusicHub runs as sub-processes. It runs as the non-root user musichub (65532).
FROM alpine:3.22
RUN apk add --no-cache ffmpeg chromaprint ca-certificates tzdata     && addgroup -g 65532 -S musichub     && adduser -u 65532 -S -G musichub -H -h /data -s /sbin/nologin musichub
COPY --from=build /out/musichub /usr/local/bin/musichub
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/music /music
ENV MUSICHUB_DATA=/data \
    MUSICHUB_MUSIC=/music
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/musichub"]
