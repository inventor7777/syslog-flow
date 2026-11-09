#!/bin/sh
set -eu

[ -f /var/run/rsyslogd.pid ] && rm -f /var/run/rsyslogd.pid

( rsyslogd -n || true ; pkill syslog-flow ) &

export SYSLOG_FLOW_RSYSLOG_PID="$(pgrep rsyslogd)"
[ -z "${SYSLOG_FLOW_RSYSLOG_PID}" ] || /usr/local/bin/syslog-flow

