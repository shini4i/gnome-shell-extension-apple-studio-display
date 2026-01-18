#!/bin/sh
set -e

# Reload udev rules for hidraw device permissions
if command -v udevadm >/dev/null 2>&1; then
    udevadm control --reload-rules
    udevadm trigger --subsystem-match=hidraw
fi

# Enable the user service for the installing user
if [ -n "$SUDO_USER" ]; then
    SUDO_UID=$(id -u "$SUDO_USER")
    if [ -d "/run/user/$SUDO_UID" ]; then
        sudo -u "$SUDO_USER" XDG_RUNTIME_DIR="/run/user/$SUDO_UID" \
            systemctl --user enable asd-brightness.service || true
    fi
fi

cat <<'EOF'
================================================================================
Apple Studio Display Brightness Control installed successfully!

Enable and start the daemon:
    systemctl --user enable --now asd-brightness.service

The GNOME extension will be available after restarting GNOME Shell:
    - Wayland: Log out and log back in
    - X11: Press Alt+F2, type 'r', press Enter
================================================================================
EOF
