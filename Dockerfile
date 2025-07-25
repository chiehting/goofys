FROM golang:1.24.4-bookworm AS builder
WORKDIR /go/src
COPY . .
RUN apt-get update && \
 apt-get install -y --no-install-recommends \
 fuse3=3.14.0-4 dumb-init=1.2.5-2 && \
 rm -rf /var/lib/apt/lists/* && \
 go mod download && \
 go build -ldflags="-w -s"

FROM gcr.io/distroless/base-nossl-debian12
COPY --from=builder /usr/bin/fusermount /usr/bin/fusermount
COPY --from=builder /usr/bin/dumb-init /usr/bin/dumb-init
COPY --from=builder /go/src/goofys /usr/bin
ENTRYPOINT ["/usr/bin/dumb-init", "--"]
