# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/homenclature ./cmd/homenclature

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/homenclature /homenclature
ENV HOMENCLATURE_CONFIG=/config/config.toml
EXPOSE 8000
ENTRYPOINT ["/homenclature"]
