// SPDX-License-Identifier: GPL-3.0-only

/**
 * Quick Settings slider control for a single Apple Studio Display.
 *
 * Provides a labeled slider in the Quick Settings panel to control
 * the brightness of an individual display.
 */

import GObject from 'gi://GObject';
import St from 'gi://St';
import Clutter from 'gi://Clutter';

import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import * as Slider from 'resource:///org/gnome/shell/ui/slider.js';

/**
 * A Quick Settings menu item with a brightness slider for a display.
 */
export const DisplayControlItem = GObject.registerClass({
    GTypeName: 'AsdDisplayControlItem',
    Signals: {
        'brightness-changed': {param_types: [GObject.TYPE_DOUBLE]},
    },
}, class DisplayControlItem extends PopupMenu.PopupBaseMenuItem {
    /**
     * Creates a new display control item.
     *
     * @param {string} serial - Display serial number
     * @param {string} productName - Display product name
     * @param {number} initialBrightness - Initial brightness (0-100), or null if fetch failed
     */
    _init(serial, productName, initialBrightness = 50) {
        super._init({
            activate: false,
            can_focus: false,
        });

        this._serial = serial;
        this._hasError = initialBrightness === null;
        this._brightness = this._hasError ? 50 : initialBrightness;
        this._dragging = false;

        // Container for the entire control
        const box = new St.BoxLayout({
            vertical: true,
            x_expand: true,
            style_class: 'asd-display-control',
        });

        // Label row with display name and brightness percentage
        const labelBox = new St.BoxLayout({
            x_expand: true,
            style_class: 'asd-display-label-box',
        });

        // Display icon
        const icon = new St.Icon({
            icon_name: 'video-display-symbolic',
            style_class: 'asd-display-icon popup-menu-icon',
        });
        labelBox.add_child(icon);

        // Display name label (truncate if too long)
        const displayName = productName.length > 20
            ? `${productName.substring(0, 17)}...`
            : productName;
        this._nameLabel = new St.Label({
            text: displayName,
            x_expand: true,
            y_align: Clutter.ActorAlign.CENTER,
            style_class: 'asd-display-name',
        });
        labelBox.add_child(this._nameLabel);

        // Brightness percentage label
        this._percentLabel = new St.Label({
            text: `${Math.round(this._brightness)}%`,
            y_align: Clutter.ActorAlign.CENTER,
            style_class: 'asd-brightness-percent',
        });
        labelBox.add_child(this._percentLabel);

        box.add_child(labelBox);

        // Brightness slider
        this._slider = new Slider.Slider(this._brightness / 100);
        this._slider.x_expand = true;

        // Connect slider events and store signal IDs for cleanup
        this._sliderSignalIds = [
            this._slider.connect('notify::value', () => {
                this._onSliderChanged();
            }),
            this._slider.connect('drag-begin', () => {
                this._dragging = true;
            }),
            this._slider.connect('drag-end', () => {
                this._dragging = false;
                this._emitBrightnessChanged();
            }),
        ];

        const sliderBox = new St.BoxLayout({
            style_class: 'asd-slider-box',
            x_expand: true,
        });

        // Min brightness icon
        const minIcon = new St.Icon({
            icon_name: 'display-brightness-symbolic',
            style_class: 'asd-brightness-icon-min',
        });
        sliderBox.add_child(minIcon);

        sliderBox.add_child(this._slider);

        // Max brightness icon
        const maxIcon = new St.Icon({
            icon_name: 'display-brightness-symbolic',
            style_class: 'asd-brightness-icon-max',
        });
        sliderBox.add_child(maxIcon);

        box.add_child(sliderBox);

        this.add_child(box);

        // If there was an error fetching brightness, disable the slider
        if (this._hasError) {
            this._setErrorState(true);
        }
    }

    /**
     * Sets the error state of the control.
     * When in error state, the slider is disabled and shows an error indication.
     *
     * @param {boolean} hasError - Whether the control is in error state
     */
    _setErrorState(hasError) {
        this._hasError = hasError;
        this._slider.reactive = !hasError;

        if (hasError) {
            this._percentLabel.text = '--';
            this._slider.add_style_class_name('asd-slider-error');
        } else {
            this._percentLabel.text = `${Math.round(this._brightness)}%`;
            this._slider.remove_style_class_name('asd-slider-error');
        }
    }

    /**
     * Gets the display serial number.
     *
     * @returns {string} Serial number
     */
    get serial() {
        return this._serial;
    }

    /**
     * Gets the current brightness value.
     *
     * @returns {number} Brightness (0-100)
     */
    get brightness() {
        return this._brightness;
    }

    /**
     * Sets the brightness value and updates the slider.
     *
     * @param {number} value - Brightness (0-100)
     */
    set brightness(value) {
        this._brightness = Math.max(0, Math.min(100, value));
        this._updateUI();
    }

    /**
     * Updates the brightness from an external source (e.g., D-Bus signal).
     * Does not emit brightness-changed signal to avoid loops.
     * Clears error state if previously in error.
     *
     * @param {number} value - Brightness (0-100)
     */
    updateBrightness(value) {
        if (this._dragging) {
            return;
        }

        // Clear error state if we receive a valid brightness update
        if (this._hasError) {
            this._setErrorState(false);
        }

        this._brightness = Math.max(0, Math.min(100, value));
        this._updateUI();
    }

    /**
     * Updates UI elements to reflect current brightness.
     */
    _updateUI() {
        this._slider.value = this._brightness / 100;
        this._percentLabel.text = `${Math.round(this._brightness)}%`;
    }

    /**
     * Handles slider value changes during dragging.
     */
    _onSliderChanged() {
        const newBrightness = Math.round(this._slider.value * 100);
        if (newBrightness !== this._brightness) {
            this._brightness = newBrightness;
            this._percentLabel.text = `${this._brightness}%`;

            // Emit during drag for real-time updates
            if (!this._dragging) {
                this._emitBrightnessChanged();
            }
        }
    }

    /**
     * Emits the brightness-changed signal.
     */
    _emitBrightnessChanged() {
        this.emit('brightness-changed', this._brightness);
    }

    /**
     * Cleans up resources and disconnects signals.
     */
    destroy() {
        if (this._sliderSignalIds) {
            this._sliderSignalIds.forEach(id => this._slider.disconnect(id));
            this._sliderSignalIds = null;
        }
        super.destroy();
    }
});
