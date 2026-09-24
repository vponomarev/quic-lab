#!/bin/sh
set -eu
if [ "${RENEWED_LINEAGE:-}" = /etc/letsencrypt/live/quic.example.org ]; then
    nginx -t
    systemctl reload nginx
    systemctl try-restart quic-lab.service
fi
