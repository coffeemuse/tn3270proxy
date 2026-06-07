# syntax=docker/dockerfile:1

# --- build stage ---
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -o /out/tn3270proxy ./cmd/tn3270proxy && \
    go build -o /out/dummy3270 ./cmd/dummy3270

# --- runtime stage ---
# alpine (not distroless) so the two-step quickstart+serve CMD has a real /bin/sh.
FROM alpine:3.20
COPY --from=build /out/tn3270proxy /usr/local/bin/tn3270proxy
COPY --from=build /out/dummy3270 /usr/local/bin/dummy3270
# Data dir is a bind mount in compose; declare it so bare `docker run` also works.
VOLUME ["/data"]
EXPOSE 2323 2324
# Runs as root deliberately: the quick start writes into a host-owned bind mount,
# and a non-root UID typically can't. This is a quick-start trade-off — the
# security-hardening doc covers running non-root. (SETUP-DEFAULTS.TXT is written
# world-readable so the host user can open it; secrets stay 0600.)
# No ENTRYPOINT: CMD is exec'd directly. The proxy service uses this default
# (provision then serve); the dummy3270 service overrides `command` in compose.
CMD ["sh", "-c", "tn3270proxy quickstart -data /data && exec tn3270proxy serve -config /data/tn3270proxy.json"]
