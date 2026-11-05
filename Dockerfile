# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# HTMX and the Sora font from npm, see package.json
FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:c6ad9e9c1d80f6eb607c26b4b0a132ff7a2344fad1c78ea3baa2a7d4c8de6d94 AS assets
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci && npm run assets

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=assets /src/internal/server/web/static/lib internal/server/web/static/lib
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/homenclature ./cmd/homenclature
RUN mkdir /out/data

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
COPY --from=build /out/homenclature /homenclature
# For HOMENCLATURE_DATA_DIR=/data (off unless set): owned by nonroot, so a
# named volume mounted there is writable.
COPY --from=build --chown=65532:65532 /out/data /data
ENV HOMENCLATURE_CONFIG=/config/config.toml
ENV HOMENCLATURE_BACKGROUND_DIR=/backgrounds
EXPOSE 8000
ENTRYPOINT ["/homenclature"]
