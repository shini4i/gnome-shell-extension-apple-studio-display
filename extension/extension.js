// SPDX-License-Identifier: GPL-3.0-only

/**
 * Apple Studio Display Brightness GNOME Shell Extension.
 *
 * Adds a Quick Settings panel entry for controlling brightness
 * of Apple Studio Display monitors via the asd-brightness-daemon D-Bus service.
 */

import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

import {AsdDaemon} from './lib/asdDaemon.js';
import {AsdIndicator} from './ui/asdToggle.js';

/**
 * Main extension class.
 */
export default class AsdBrightnessExtension extends Extension {
    constructor(metadata) {
        super(metadata);
        this._daemon = null;
        this._indicator = null;
        this._enabled = false;
    }

    /**
     * Called when the extension is enabled.
     */
    async enable() {
        console.log('[AsdBrightness] Enabling extension');
        this._enabled = true;

        // Initialize D-Bus daemon client
        this._daemon = new AsdDaemon();
        const connected = await this._daemon.init();

        // Check if disabled during async init
        if (!this._enabled) {
            console.log('[AsdBrightness] Extension disabled during initialization');
            if (this._daemon) {
                this._daemon.destroy();
                this._daemon = null;
            }
            return;
        }

        if (!connected) {
            console.warn('[AsdBrightness] Failed to connect to daemon, extension may not work correctly');
        }

        // Create and add the system indicator with Quick Settings toggle
        this._indicator = new AsdIndicator(this._daemon);
        Main.panel.statusArea.quickSettings.addExternalIndicator(this._indicator);

        console.log('[AsdBrightness] Extension enabled');
    }

    /**
     * Called when the extension is disabled.
     */
    disable() {
        console.log('[AsdBrightness] Disabling extension');
        this._enabled = false;

        // Clean up indicator
        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }

        // Clean up daemon connection
        if (this._daemon) {
            this._daemon.destroy();
            this._daemon = null;
        }

        console.log('[AsdBrightness] Extension disabled');
    }
}
