FROM golang:1.24.4-alpine3.22 AS builder
WORKDIR /go/src
COPY . .
RUN go mod tidy; go build -ldflags="-w -s"

FROM alpine:3.22.0
COPY --from=builder /go/src/goofys /usr/bin
RUN apk add --no-cache mailcap fuse s6-overlay
ENTRYPOINT ["/init"]
