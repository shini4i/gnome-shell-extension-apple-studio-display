#!/bin/sh
set -e

# Stop and disable user service for all logged-in users (best effort)
if command -v systemctl >/dev/null 2>&1; then
    for uid in $(loginctl list-users --no-legend 2>/dev/null | awk '{print $1}'); do
        systemctl --user --machine="$uid@.host" disable --now asd-brightness.service 2>/dev/null || true
    done
fi
