FROM golang:1.22-bookworm AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/syslog-flow ./cmd/syslog-flow

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends rsyslog ca-certificates acl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/syslog-flow /usr/local/bin/syslog-flow
COPY rsyslog.conf /etc/rsyslog.conf
COPY entrypoint.sh /entrypoint.sh
COPY resources /resources

RUN chmod 755 /entrypoint.sh \
    && mkdir -p /logs

EXPOSE 2200 514/tcp 514/udp

ENTRYPOINT ["/entrypoint.sh"]
