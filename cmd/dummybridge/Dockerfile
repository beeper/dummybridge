FROM golang:1-alpine3.20 AS builder

COPY . /build
WORKDIR /build
ENV CGO_ENABLED=0
RUN go build ./cmd/dummybridge -o /usr/bin/dummybridge

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=builder /usr/bin/dummybridge /usr/bin/dummybridge
CMD ["/usr/bin/dummybridge"]
