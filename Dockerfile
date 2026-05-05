# syntax=docker/dockerfile:1

FROM golang:alpine AS build-env
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.* /src/
RUN go mod download
COPY . .
RUN go build -a -o websitewatcher -ldflags="-s -w" -trimpath

FROM alpine:latest

RUN apk add --no-cache ca-certificates git \
    && rm -rf /var/cache/*

RUN mkdir -p /app \
    && adduser -D user \
    && chown -R user:user /app

USER user
WORKDIR /app

COPY --from=build-env /src/websitewatcher .

ENTRYPOINT [ "/app/websitewatcher" ]
