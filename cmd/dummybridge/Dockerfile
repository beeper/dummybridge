FROM golang:1-alpine3.20 AS builder

RUN apk add --no-cache build-base olm-dev

COPY . /build
WORKDIR /build
ENV CGO_ENABLED=1
RUN go build -o /usr/bin/dummybridge ./cmd/dummybridge

FROM alpine:3.20

RUN apk add --no-cache ca-certificates olm su-exec bash jq yq curl
COPY --from=builder /usr/bin/dummybridge /usr/bin/dummybridge
CMD ["/usr/bin/dummybridge"]
