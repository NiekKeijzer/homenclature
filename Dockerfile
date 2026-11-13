# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# HTMX and the Sora font from npm, see package.json
FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS assets
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci && npm run assets

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=assets /src/internal/server/web/static/lib internal/server/web/static/lib
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/homenclature ./cmd/homenclature
RUN mkdir /out/data

FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /out/homenclature /homenclature
# For HOMENCLATURE_DATA_DIR=/data (off unless set): owned by nonroot, so a
# named volume mounted there is writable.
COPY --from=build --chown=65532:65532 /out/data /data
ENV HOMENCLATURE_CONFIG=/config/config.toml
ENV HOMENCLATURE_BACKGROUND_DIR=/backgrounds
EXPOSE 8000
ENTRYPOINT ["/homenclature"]
