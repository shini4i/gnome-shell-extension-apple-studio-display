#!/bin/sh
set -e

# Reload udev rules for hidraw device permissions
if command -v udevadm >/dev/null 2>&1; then
    udevadm control --reload-rules
    udevadm trigger --subsystem-match=hidraw
fi

# Enable the user service globally for all users (current and future)
if command -v systemctl >/dev/null 2>&1; then
    systemctl --global enable asd-brightness.service
fi

cat <<'EOF'
================================================================================
Apple Studio Display Brightness Control installed successfully!

Please LOG OUT and LOG BACK IN to apply changes.
(This is required for the GNOME Extension to load and the service to start)
================================================================================
EOF
