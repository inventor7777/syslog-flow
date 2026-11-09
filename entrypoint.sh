#!/bin/sh
set -eu

[ -f /var/run/rsyslogd.pid ] && rm -f /var/run/rsyslogd.pid

_start_rsyslogd() {
    rsyslogd -n ; exit 0
}

_start_rsyslogd &
SYSLOG_FLOW_RSYSLOG_PID="$(pgrep rsyslogd)" /usr/local/bin/syslog-flow
