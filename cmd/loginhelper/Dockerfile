FROM golang:1-alpine3.20 AS builder

RUN apk add --no-cache build-base

COPY . /build
WORKDIR /build
ENV CGO_ENABLED=1
RUN go build -o /usr/bin/loginhelper ./cmd/loginhelper

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=builder /usr/bin/loginhelper /usr/bin/loginhelper
CMD ["/usr/bin/loginhelper"]
