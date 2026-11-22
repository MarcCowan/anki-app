# Multi-stage build (deploy spec §5.1). Base images are pinned by multi-arch index digest.

FROM node:26-bookworm-slim@sha256:5df79989ed685e09f0a98019db4cebfb22fb9df1a53c0161664a011f7ff6b0f1 AS web
# vite builds into ../server/internal/webui/dist, and the build script writes .gitkeep there.
RUN mkdir -p /src/server/internal/webui/dist
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS build
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/server/internal/webui/dist ./internal/webui/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/anki-app ./cmd/anki-app
RUN mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG VERSION=dev
LABEL org.opencontainers.image.revision=${VERSION} \
      org.opencontainers.image.source=https://github.com/MarcCowan/anki-app
COPY --from=build /out/anki-app /anki-app
# A fresh named volume copies this directory's ownership, so uid 65532 can write to it.
COPY --from=build --chown=65532:65532 /out/data /data
ENV DB_PATH=/data/anki-app.db \
    BACKUP_DIR=/data/backups
USER 65532:65532
ENTRYPOINT ["/anki-app"]
CMD ["serve"]
