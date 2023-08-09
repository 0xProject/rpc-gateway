FROM golang:1.21-alpine3.18 AS builder

RUN apk add --update-cache \
        git \
        build-base

WORKDIR /src
COPY . .

RUN go build -o rpc-gateway cmd/rpcgateway/main.go

FROM alpine:3.18

RUN apk add --update-cache --no-cache \
        ca-certificates

COPY --from=builder /src/rpc-gateway /app/

VOLUME ["/app"]

USER nobody
CMD ["/app/rpc-gateway"]
