FROM golang:1.25-alpine3.23 AS builder

RUN apk add --no-cache build-base olm-dev

COPY . /build
WORKDIR /build
ENV CGO_ENABLED=1
RUN go build -o /usr/bin/dummybridge ./cmd/dummybridge

FROM alpine:3.20

RUN apk add --no-cache ca-certificates olm su-exec bash jq yq curl \
	&& addgroup -S dummybridge \
	&& adduser -S -G dummybridge dummybridge
COPY --from=builder --chown=dummybridge:dummybridge /usr/bin/dummybridge /usr/bin/dummybridge
USER dummybridge:dummybridge
CMD ["/usr/bin/dummybridge"]
