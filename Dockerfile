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

# Stage 3: minimal runtime image, running as the non-root user (uid 65532).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/musichub /usr/local/bin/musichub
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/music /music
ENV MUSICHUB_DATA=/data \
    MUSICHUB_MUSIC=/music
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/musichub"]
