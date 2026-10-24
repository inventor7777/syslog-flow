FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/syslog-flow ./cmd/syslog-flow

FROM alpine:latest

RUN apk add --no-cache rsyslog ca-certificates

COPY --from=build /out/syslog-flow /usr/local/bin/syslog-flow
COPY rsyslog.conf /etc/rsyslog.conf
COPY entrypoint.sh /entrypoint.sh
RUN chmod 755 /entrypoint.sh
COPY resources /resources

VOLUME ["/logs", "/config", "/var/spool/rsyslog"]

EXPOSE 2200/tcp 514/tcp 514/udp

ENTRYPOINT ["/entrypoint.sh"]
