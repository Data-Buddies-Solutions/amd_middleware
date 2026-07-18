# syntax=docker/dockerfile:1

FROM golang:1.26.4-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/gateway \
    ./cmd/api

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S gateway \
    && adduser -S -G gateway -H gateway

COPY --from=build --chown=gateway:gateway /out/gateway /usr/local/bin/gateway

USER gateway
ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/gateway"]
