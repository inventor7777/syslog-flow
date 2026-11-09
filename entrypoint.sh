#!/bin/sh
set -eu

[ -f /var/run/rsyslogd.pid ] && rm -f /var/run/rsyslogd.pid

( rsyslogd -n || true ; pkill syslog-flow ) &

SYSLOG_FLOW_RSYSLOG_PID="$(pgrep rsyslogd)" /usr/local/bin/syslog-flow

