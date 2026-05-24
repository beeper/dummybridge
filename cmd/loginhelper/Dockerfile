FROM golang:1.25-alpine3.23 AS builder

RUN apk add --no-cache build-base

COPY . /build
WORKDIR /build
ENV CGO_ENABLED=1
RUN go build -o /usr/bin/loginhelper ./cmd/loginhelper

FROM alpine:3.23

RUN apk add --no-cache ca-certificates \
	&& addgroup -S loginhelper \
	&& adduser -S -G loginhelper loginhelper
COPY --from=builder --chown=loginhelper:loginhelper /usr/bin/loginhelper /usr/bin/loginhelper
USER loginhelper:loginhelper
CMD ["/usr/bin/loginhelper"]
