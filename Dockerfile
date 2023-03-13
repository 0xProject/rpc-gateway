FROM golang:1.20.2-alpine3.16 AS builder

RUN apk add --update-cache \
        git \
        build-base

WORKDIR /src
COPY . .

RUN go build -o rpc-gateway cmd/rpcgateway/main.go

FROM alpine:3.17

RUN apk add --update-cache --no-cache \
        ca-certificates

COPY --from=builder /src/rpc-gateway /app/

VOLUME ["/app"]

USER nobody
CMD ["/app/rpc-gateway"]
