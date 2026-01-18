#!/bin/sh
set -e

# Reload udev rules for hidraw device permissions
if command -v udevadm >/dev/null 2>&1; then
    udevadm control --reload-rules
    udevadm trigger --subsystem-match=hidraw
fi

# Add the installing user to the video group for hidraw access
if [ -n "$SUDO_USER" ]; then
    if ! id -nG "$SUDO_USER" | grep -qw video; then
        usermod -aG video "$SUDO_USER"
        echo "User '$SUDO_USER' has been added to the 'video' group."
    else
        echo "User '$SUDO_USER' is already in the 'video' group."
    fi
fi

cat <<'EOF'
================================================================================
Apple Studio Display Brightness Control installed successfully!

NOTE: You need to log out and back in for the group change to take effect.

The GNOME extension will be available after restarting GNOME Shell:
    - Wayland: Log out and log back in
    - X11: Press Alt+F2, type 'r', press Enter
================================================================================
EOF
