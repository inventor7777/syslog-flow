#!/bin/sh
set -eu

mkdir -p /logs
chmod -R a+rX /logs
setfacl -R -m o::rX /logs
setfacl -m d:o::rx /logs
umask 0000

term_handler() {
  if [ "${rsyslog_pid:-}" ]; then
    kill "$rsyslog_pid" 2>/dev/null || true
  fi
  if [ "${web_pid:-}" ]; then
    kill "$web_pid" 2>/dev/null || true
  fi
}

trap 'term_handler; wait 2>/dev/null || true; exit 0' INT TERM

rsyslogd -n &
rsyslog_pid=$!

/usr/local/bin/syslog-flow &
web_pid=$!

while :; do
  if ! kill -0 "$rsyslog_pid" 2>/dev/null; then
    wait "$rsyslog_pid" || status=$?
    kill "$web_pid" 2>/dev/null || true
    wait "$web_pid" 2>/dev/null || true
    exit "${status:-1}"
  fi

  if ! kill -0 "$web_pid" 2>/dev/null; then
    wait "$web_pid" || status=$?
    kill "$rsyslog_pid" 2>/dev/null || true
    wait "$rsyslog_pid" 2>/dev/null || true
    exit "${status:-1}"
  fi

  sleep 1
done
