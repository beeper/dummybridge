FROM golang:1-alpine3.20 AS builder

COPY . /build
WORKDIR /build
ENV CGO_ENABLED=0
RUN go build ./cmd/loginhelper -o /usr/bin/loginhelper

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=builder /usr/bin/loginhelper /usr/bin/loginhelper
CMD ["/usr/bin/loginhelper"]
