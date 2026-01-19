// SPDX-License-Identifier: GPL-3.0-only

/**
 * Quick Settings toggle menu for Apple Studio Display brightness control.
 *
 * Provides a system menu entry that shows connected displays
 * with individual brightness sliders.
 */

import GObject from 'gi://GObject';

import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import {QuickMenuToggle, SystemIndicator} from 'resource:///org/gnome/shell/ui/quickSettings.js';

import {DisplayControlItem} from './displayControlItem.js';

/**
 * Quick Settings toggle button with display brightness menu.
 */
export const AsdToggle = GObject.registerClass({
    GTypeName: 'AsdQuickMenuToggle',
}, class AsdToggle extends QuickMenuToggle {
    /**
     * Creates a new ASD toggle.
     *
     * @param {import('../lib/asdDaemon.js').AsdDaemon} daemon - D-Bus daemon client
     */
    _init(daemon) {
        super._init({
            title: 'Displays',
            iconName: 'video-display-symbolic',
            toggleMode: false,
        });

        this._daemon = daemon;
        this._displayItems = new Map(); // Map<serial, {item, signalId}>
        this._pendingAdds = new Set(); // Track serials being added (prevents race conditions)
        this._refreshVersion = 0; // Version counter for refresh operations
        this._isRefreshing = false; // Flag to prevent concurrent refreshes
        this._noDisplaysItem = null;
        this._daemonDisconnects = [];

        // Create menu header
        this.menu.setHeader('video-display-symbolic', 'Apple Studio Display');

        // Add separator after header
        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Placeholder for when no displays are connected
        this._noDisplaysItem = new PopupMenu.PopupMenuItem('No displays connected', {
            reactive: false,
            can_focus: false,
        });
        this._noDisplaysItem.label.add_style_class_name('asd-no-displays');
        this.menu.addMenuItem(this._noDisplaysItem);

        // Connect D-Bus signals and store disconnect functions
        this._daemonDisconnects.push(
            this._daemon.onDisplayAdded((serial, productName) => {
                this._onDisplayAdded(serial, productName);
            })
        );

        this._daemonDisconnects.push(
            this._daemon.onDisplayRemoved((serial) => {
                this._onDisplayRemoved(serial);
            })
        );

        this._daemonDisconnects.push(
            this._daemon.onBrightnessChanged((serial, brightness) => {
                this._onBrightnessChanged(serial, brightness);
            })
        );

        // Handle daemon availability changes (restart/crash recovery)
        this._daemonDisconnects.push(
            this._daemon.onDaemonAvailable(() => {
                console.log('[AsdBrightness] Daemon available, refreshing displays');
                this._refreshDisplays();
            })
        );

        this._daemonDisconnects.push(
            this._daemon.onDaemonUnavailable(() => {
                console.log('[AsdBrightness] Daemon unavailable, clearing displays');
                this._clearDisplays();
            })
        );

        // Initial display enumeration
        this._refreshDisplays();
    }

    /**
     * Clears all display items when daemon becomes unavailable.
     */
    _clearDisplays() {
        if (!this._displayItems) {
            return;
        }

        this._displayItems.forEach(({item, signalId}, _serial) => {
            item.disconnect(signalId);
            item.destroy();
        });
        this._displayItems.clear();

        this._updateVisibility();
    }

    /**
     * Refreshes the list of connected displays from the daemon.
     *
     * Uses version tracking to prevent race conditions:
     * - Only one refresh can run at a time
     * - If a newer refresh starts, older ones abort gracefully
     * - Incoming D-Bus signals during refresh don't corrupt state
     */
    async _refreshDisplays() {
        // Prevent concurrent refreshes
        if (this._isRefreshing) {
            return;
        }

        this._isRefreshing = true;
        const currentVersion = ++this._refreshVersion;

        try {
            const displays = await this._daemon.listDisplays();

            // Abort if superseded by newer refresh or destroyed
            if (!this._displayItems || this._refreshVersion !== currentVersion) {
                return;
            }

            // Clear existing items with proper signal disconnection
            this._displayItems.forEach(({item, signalId}, _serial) => {
                item.disconnect(signalId);
                item.destroy();
            });
            this._displayItems.clear();
            this._pendingAdds?.clear();

            // Add items for each display
            for (const display of displays) {
                // Check version before each async add to abort if superseded
                if (this._refreshVersion !== currentVersion) {
                    return;
                }
                await this._addDisplayItem(display.serial, display.productName);
            }

            // Final version check before updating visibility
            if (this._refreshVersion === currentVersion) {
                this._updateVisibility();
            }
        } finally {
            // Only clear the flag if no newer refresh has started.
            // A newer refresh would have incremented _refreshVersion and set its own flag.
            if (this._refreshVersion === currentVersion) {
                this._isRefreshing = false;
            }
        }
    }

    /**
     * Adds a display control item to the menu.
     *
     * Guards against race conditions by tracking in-flight additions.
     * If the same serial is already being added or exists in the map,
     * the call is silently ignored.
     *
     * @param {string} serial - Display serial number
     * @param {string} productName - Display product name
     */
    async _addDisplayItem(serial, productName) {
        // Guard against concurrent adds for the same serial (race condition fix)
        // Also guard against destroyed state where _pendingAdds is null
        if (!this._displayItems || !this._pendingAdds || this._displayItems.has(serial) || this._pendingAdds.has(serial)) {
            return;
        }

        // Mark this serial as being added to prevent concurrent additions
        this._pendingAdds.add(serial);

        try {
            // Pass null to DisplayControlItem if fetch fails - it will show error state
            const brightness = await this._daemon.getBrightness(serial);

            // Guard against destruction during await or if already added by another call
            if (!this._displayItems || this._displayItems.has(serial)) {
                return;
            }

            const item = new DisplayControlItem(serial, productName, brightness);
            const signalId = item.connect('brightness-changed', (_item, value) => {
                this._daemon.setBrightness(serial, value);
            });

            // Insert before the "no displays" placeholder
            const position = this.menu.numMenuItems - 1;
            this.menu.addMenuItem(item, position);
            this._displayItems.set(serial, {item, signalId});

            this._updateVisibility();
        } finally {
            // Always remove from pending set, even on error
            this._pendingAdds?.delete(serial);
        }
    }

    /**
     * Removes a display control item from the menu.
     *
     * @param {string} serial - Display serial number
     */
    _removeDisplayItem(serial) {
        // Guard against destruction
        if (!this._displayItems) {
            return;
        }

        const entry = this._displayItems.get(serial);
        if (entry) {
            entry.item.disconnect(entry.signalId);
            entry.item.destroy();
            this._displayItems.delete(serial);
        }

        this._updateVisibility();
    }

    /**
     * Updates the visibility of the "no displays" placeholder.
     */
    _updateVisibility() {
        // Guard against destruction
        if (!this._displayItems || !this._noDisplaysItem) {
            return;
        }

        const hasDisplays = this._displayItems.size > 0;
        this._noDisplaysItem.visible = !hasDisplays;

        // Update toggle checked state based on displays
        this.checked = hasDisplays;
    }

    /**
     * Handles DisplayAdded D-Bus signal.
     *
     * @param {string} serial - Display serial number
     * @param {string} productName - Display product name
     */
    _onDisplayAdded(serial, productName) {
        this._addDisplayItem(serial, productName);
    }

    /**
     * Handles DisplayRemoved D-Bus signal.
     *
     * @param {string} serial - Display serial number
     */
    _onDisplayRemoved(serial) {
        this._removeDisplayItem(serial);
    }

    /**
     * Handles BrightnessChanged D-Bus signal.
     *
     * @param {string} serial - Display serial number
     * @param {number} brightness - New brightness value
     */
    _onBrightnessChanged(serial, brightness) {
        // Guard against destruction
        if (!this._displayItems) {
            return;
        }

        const entry = this._displayItems.get(serial);
        if (entry) {
            entry.item.updateBrightness(brightness);
        }
    }

    /**
     * Cleans up resources.
     */
    destroy() {
        // Invalidate any in-flight refresh operations by incrementing version
        this._refreshVersion++;
        this._isRefreshing = false;

        // Disconnect daemon callbacks
        this._daemonDisconnects.forEach(disconnect => disconnect());
        this._daemonDisconnects = [];

        // Clean up display items
        if (this._displayItems) {
            this._displayItems.forEach(({item, signalId}, _serial) => {
                item.disconnect(signalId);
                item.destroy();
            });
            this._displayItems.clear();
            this._displayItems = null;
        }

        // Clean up race condition prevention state
        this._pendingAdds?.clear();
        this._pendingAdds = null;

        this._noDisplaysItem = null;

        super.destroy();
    }
});

/**
 * System indicator that appears in the top panel.
 */
export const AsdIndicator = GObject.registerClass({
    GTypeName: 'AsdSystemIndicator',
}, class AsdIndicator extends SystemIndicator {
    /**
     * Creates a new ASD system indicator.
     *
     * @param {import('../lib/asdDaemon.js').AsdDaemon} daemon - D-Bus daemon client
     */
    _init(daemon) {
        super._init();

        this._daemon = daemon;
        this._daemonDisconnects = [];

        // Create indicator icon (hidden by default)
        this._indicator = this._addIndicator();
        this._indicator.iconName = 'video-display-symbolic';
        this._indicator.visible = false;

        // Create Quick Settings toggle
        this._toggle = new AsdToggle(daemon);
        this.quickSettingsItems.push(this._toggle);

        // Update indicator visibility based on connected displays
        this._updateIndicator();

        // Connect D-Bus signals for indicator updates and store disconnect functions
        this._daemonDisconnects.push(
            this._daemon.onDisplayAdded(() => this._updateIndicator())
        );
        this._daemonDisconnects.push(
            this._daemon.onDisplayRemoved(() => this._updateIndicator())
        );

        // Handle daemon availability changes
        this._daemonDisconnects.push(
            this._daemon.onDaemonAvailable(() => this._updateIndicator())
        );
        this._daemonDisconnects.push(
            this._daemon.onDaemonUnavailable(() => {
                if (this._indicator) {
                    this._indicator.visible = false;
                }
            })
        );
    }

    /**
     * Updates the indicator visibility based on connected displays.
     */
    async _updateIndicator() {
        const displays = await this._daemon.listDisplays();

        // Guard against destruction during await
        if (this._indicator) {
            this._indicator.visible = displays.length > 0;
        }
    }

    /**
     * Cleans up resources.
     */
    destroy() {
        // Disconnect daemon callbacks
        this._daemonDisconnects.forEach(disconnect => disconnect());
        this._daemonDisconnects = [];

        this._toggle?.destroy();
        this._toggle = null;
        this._indicator = null;

        super.destroy();
    }
});
