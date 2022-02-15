FROM golang:1.17-alpine3.13 as builder

RUN apk update && apk add git build-base

WORKDIR /src

ADD . ./

RUN go build

# final image
FROM alpine:3.13

RUN apk update && apk add ca-certificates --no-cache

RUN mkdir -p /app
    
COPY --from=builder /src/rpc-gateway /app/rpc-gateway

USER app
VOLUME ["/app"]
CMD ["/app/rpc-gateway"]