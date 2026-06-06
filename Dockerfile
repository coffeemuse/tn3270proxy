# syntax=docker/dockerfile:1

FROM golang:1.25 AS build
WORKDIR /src
ARG VERSION=dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/tn3270proxy ./cmd/tn3270proxy
RUN mkdir -p /data

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/tn3270proxy /tn3270proxy
COPY --from=build --chown=65532:65532 /data /data
USER nonroot:nonroot
# 2323 = plaintext listener. The config-file TLS listener (tn3270proxy.json) is :2324.
EXPOSE 2323
ENTRYPOINT ["/tn3270proxy"]
CMD ["serve"]
