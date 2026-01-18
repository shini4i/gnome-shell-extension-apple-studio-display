/**
 * D-Bus client wrapper for asd-brightness-daemon.
 *
 * Provides async methods to control Apple Studio Display brightness
 * via the io.github.shini4i.AsdBrightness D-Bus service.
 *
 * Features automatic reconnection when the daemon restarts by watching
 * for D-Bus name owner changes.
 */

import Gio from 'gi://Gio';

const SERVICE_NAME = 'io.github.shini4i.AsdBrightness';
const OBJECT_PATH = '/io/github/shini4i/AsdBrightness';
const INTERFACE_NAME = 'io.github.shini4i.AsdBrightness';

const AsdBrightnessInterface = `
<node>
  <interface name="${INTERFACE_NAME}">
    <method name="ListDisplays">
      <arg name="displays" type="a(ss)" direction="out"/>
    </method>
    <method name="GetBrightness">
      <arg name="serial" type="s" direction="in"/>
      <arg name="brightness" type="u" direction="out"/>
    </method>
    <method name="SetBrightness">
      <arg name="serial" type="s" direction="in"/>
      <arg name="brightness" type="u" direction="in"/>
    </method>
    <method name="IncreaseBrightness">
      <arg name="serial" type="s" direction="in"/>
      <arg name="step" type="u" direction="in"/>
    </method>
    <method name="DecreaseBrightness">
      <arg name="serial" type="s" direction="in"/>
      <arg name="step" type="u" direction="in"/>
    </method>
    <method name="SetAllBrightness">
      <arg name="brightness" type="u" direction="in"/>
    </method>
    <signal name="DisplayAdded">
      <arg name="serial" type="s"/>
      <arg name="productName" type="s"/>
    </signal>
    <signal name="DisplayRemoved">
      <arg name="serial" type="s"/>
    </signal>
    <signal name="BrightnessChanged">
      <arg name="serial" type="s"/>
      <arg name="brightness" type="u"/>
    </signal>
  </interface>
</node>
`;

/**
 * D-Bus client for asd-brightness-daemon.
 *
 * Manages connection to the brightness daemon and provides
 * methods and signals for display brightness control.
 * Automatically reconnects when the daemon restarts.
 */
export class AsdDaemon {
    constructor() {
        this._proxy = null;
        this._signalIds = [];
        this._nameWatcherId = 0;
        this._isReconnecting = false;
        this._callbacks = {
            displayAdded: [],
            displayRemoved: [],
            brightnessChanged: [],
            daemonAvailable: [],
            daemonUnavailable: [],
        };
    }

    /**
     * Initializes the D-Bus proxy connection and name watcher.
     *
     * @returns {Promise<boolean>} True if connection succeeded
     */
    async init() {
        // Set up name watcher to detect daemon restarts
        this._setupNameWatcher();

        // Try initial connection
        return await this._connect();
    }

    /**
     * Sets up the D-Bus name watcher to detect daemon availability changes.
     */
    _setupNameWatcher() {
        if (this._nameWatcherId !== 0) {
            return;
        }

        this._nameWatcherId = Gio.bus_watch_name(
            Gio.BusType.SESSION,
            SERVICE_NAME,
            Gio.BusNameWatcherFlags.NONE,
            this._onDaemonAppeared.bind(this),
            this._onDaemonVanished.bind(this)
        );

        console.log('[AsdBrightness] Name watcher set up for daemon');
    }

    /**
     * Called when the daemon name appears on the bus.
     */
    async _onDaemonAppeared() {
        console.log('[AsdBrightness] Daemon appeared on D-Bus');

        // Guard against destroyed state
        if (this._callbacks === null) {
            return;
        }

        // If we already have a proxy, we need to reconnect to get fresh subscriptions
        if (this._proxy !== null && !this._isReconnecting) {
            console.log('[AsdBrightness] Reconnecting to daemon after restart');
            await this._reconnect();
        } else if (this._proxy === null && !this._isReconnecting) {
            // Initial connect failed, attempt a fresh connection now that daemon is available
            console.log('[AsdBrightness] Attempting fresh connection to daemon');
            await this._connect();
        }

        // Guard against destruction during await
        if (this._callbacks === null) {
            return;
        }

        // Only notify listeners if we actually have a connection
        if (this._proxy !== null) {
            this._callbacks.daemonAvailable.forEach(cb => cb());
        }
    }

    /**
     * Called when the daemon name vanishes from the bus.
     */
    _onDaemonVanished() {
        console.log('[AsdBrightness] Daemon vanished from D-Bus');

        // Guard against destroyed state
        if (this._callbacks === null) {
            return;
        }

        // Clear the stale proxy
        this._disconnectSignals();
        this._proxy = null;

        // Notify listeners that daemon is unavailable
        this._callbacks.daemonUnavailable.forEach(cb => cb());
    }

    /**
     * Reconnects to the daemon after it restarts.
     */
    async _reconnect() {
        if (this._isReconnecting) {
            return;
        }

        this._isReconnecting = true;

        // Clean up old connection
        this._disconnectSignals();
        this._proxy = null;

        // Establish new connection
        const connected = await this._connect();

        // Guard against destruction during await
        if (this._callbacks === null) {
            return;
        }

        this._isReconnecting = false;

        if (connected) {
            console.log('[AsdBrightness] Successfully reconnected to daemon');
        }
    }

    /**
     * Establishes the D-Bus proxy connection.
     *
     * @returns {Promise<boolean>} True if connection succeeded
     */
    async _connect() {
        try {
            const ProxyClass = Gio.DBusProxy.makeProxyWrapper(AsdBrightnessInterface);
            this._proxy = await new Promise((resolve, reject) => {
                new ProxyClass(
                    Gio.DBus.session,
                    SERVICE_NAME,
                    OBJECT_PATH,
                    (proxy, error) => {
                        if (error) {
                            reject(error);
                        } else {
                            resolve(proxy);
                        }
                    }
                );
            });

            this._connectSignals();
            return true;
        } catch (e) {
            console.error(`[AsdBrightness] Failed to connect to daemon: ${e.message}`);
            return false;
        }
    }

    /**
     * Connects D-Bus signal handlers.
     */
    _connectSignals() {
        if (!this._proxy) {
            return;
        }

        const displayAddedId = this._proxy.connectSignal(
            'DisplayAdded',
            (_proxy, _sender, [serial, productName]) => {
                this._callbacks.displayAdded.forEach(cb => cb(serial, productName));
            }
        );
        this._signalIds.push(displayAddedId);

        const displayRemovedId = this._proxy.connectSignal(
            'DisplayRemoved',
            (_proxy, _sender, [serial]) => {
                this._callbacks.displayRemoved.forEach(cb => cb(serial));
            }
        );
        this._signalIds.push(displayRemovedId);

        const brightnessChangedId = this._proxy.connectSignal(
            'BrightnessChanged',
            (_proxy, _sender, [serial, brightness]) => {
                this._callbacks.brightnessChanged.forEach(cb => cb(serial, brightness));
            }
        );
        this._signalIds.push(brightnessChangedId);
    }

    /**
     * Disconnects D-Bus signal handlers.
     */
    _disconnectSignals() {
        if (this._proxy && this._signalIds.length > 0) {
            this._signalIds.forEach(id => this._proxy.disconnectSignal(id));
        }
        this._signalIds = [];
    }

    /**
     * Destroys the D-Bus connection and cleans up resources.
     */
    destroy() {
        // Unwatch the name
        if (this._nameWatcherId !== 0) {
            Gio.bus_unwatch_name(this._nameWatcherId);
            this._nameWatcherId = 0;
        }

        // Disconnect signals and clean up proxy
        this._disconnectSignals();
        this._proxy = null;

        // Clear callbacks and set to null to signal destruction
        // (null check is used in async methods to detect destruction)
        if (this._callbacks) {
            Object.keys(this._callbacks).forEach(key => {
                this._callbacks[key].length = 0;
            });
        }
        this._callbacks = null;
    }

    /**
     * Registers a callback for DisplayAdded signals.
     *
     * @param {function(string, string): void} callback - Called with (serial, productName)
     * @returns {function(): void} Function to unregister the callback
     */
    onDisplayAdded(callback) {
        this._callbacks.displayAdded.push(callback);
        return () => {
            const idx = this._callbacks.displayAdded.indexOf(callback);
            if (idx !== -1)
                this._callbacks.displayAdded.splice(idx, 1);
        };
    }

    /**
     * Registers a callback for DisplayRemoved signals.
     *
     * @param {function(string): void} callback - Called with serial
     * @returns {function(): void} Function to unregister the callback
     */
    onDisplayRemoved(callback) {
        this._callbacks.displayRemoved.push(callback);
        return () => {
            const idx = this._callbacks.displayRemoved.indexOf(callback);
            if (idx !== -1)
                this._callbacks.displayRemoved.splice(idx, 1);
        };
    }

    /**
     * Registers a callback for BrightnessChanged signals.
     *
     * @param {function(string, number): void} callback - Called with (serial, brightness)
     * @returns {function(): void} Function to unregister the callback
     */
    onBrightnessChanged(callback) {
        this._callbacks.brightnessChanged.push(callback);
        return () => {
            const idx = this._callbacks.brightnessChanged.indexOf(callback);
            if (idx !== -1)
                this._callbacks.brightnessChanged.splice(idx, 1);
        };
    }

    /**
     * Registers a callback for when the daemon becomes available.
     * This is called when the daemon starts or restarts.
     *
     * @param {function(): void} callback - Called when daemon is available
     * @returns {function(): void} Function to unregister the callback
     */
    onDaemonAvailable(callback) {
        this._callbacks.daemonAvailable.push(callback);
        return () => {
            const idx = this._callbacks.daemonAvailable.indexOf(callback);
            if (idx !== -1)
                this._callbacks.daemonAvailable.splice(idx, 1);
        };
    }

    /**
     * Registers a callback for when the daemon becomes unavailable.
     * This is called when the daemon stops or crashes.
     *
     * @param {function(): void} callback - Called when daemon is unavailable
     * @returns {function(): void} Function to unregister the callback
     */
    onDaemonUnavailable(callback) {
        this._callbacks.daemonUnavailable.push(callback);
        return () => {
            const idx = this._callbacks.daemonUnavailable.indexOf(callback);
            if (idx !== -1)
                this._callbacks.daemonUnavailable.splice(idx, 1);
        };
    }

    /**
     * Lists all connected displays.
     *
     * @returns {Promise<Array<{serial: string, productName: string}>>} Array of display info
     */
    async listDisplays() {
        if (!this._proxy) {
            return [];
        }

        try {
            const [displays] = await this._callMethod('ListDisplays');
            return displays.map(([serial, productName]) => ({serial, productName}));
        } catch (e) {
            console.error(`[AsdBrightness] ListDisplays failed: ${e.message}`);
            return [];
        }
    }

    /**
     * Gets the brightness of a specific display.
     *
     * @param {string} serial - Display serial number
     * @returns {Promise<number|null>} Brightness percentage (0-100) or null on error
     */
    async getBrightness(serial) {
        if (!this._proxy) {
            return null;
        }

        try {
            const [brightness] = await this._callMethod('GetBrightness', serial);
            return brightness;
        } catch (e) {
            console.error(`[AsdBrightness] GetBrightness failed: ${e.message}`);
            return null;
        }
    }

    /**
     * Sets the brightness of a specific display.
     *
     * @param {string} serial - Display serial number
     * @param {number} brightness - Brightness percentage (0-100)
     * @returns {Promise<boolean>} True if successful
     */
    async setBrightness(serial, brightness) {
        if (!this._proxy) {
            return false;
        }

        try {
            await this._callMethod('SetBrightness', serial, brightness);
            return true;
        } catch (e) {
            console.error(`[AsdBrightness] SetBrightness failed: ${e.message}`);
            return false;
        }
    }

    /**
     * Increases brightness of a display by a step.
     *
     * @param {string} serial - Display serial number
     * @param {number} step - Amount to increase (default: 5)
     * @returns {Promise<boolean>} True if successful
     */
    async increaseBrightness(serial, step = 5) {
        if (!this._proxy) {
            return false;
        }

        try {
            await this._callMethod('IncreaseBrightness', serial, step);
            return true;
        } catch (e) {
            console.error(`[AsdBrightness] IncreaseBrightness failed: ${e.message}`);
            return false;
        }
    }

    /**
     * Decreases brightness of a display by a step.
     *
     * @param {string} serial - Display serial number
     * @param {number} step - Amount to decrease (default: 5)
     * @returns {Promise<boolean>} True if successful
     */
    async decreaseBrightness(serial, step = 5) {
        if (!this._proxy) {
            return false;
        }

        try {
            await this._callMethod('DecreaseBrightness', serial, step);
            return true;
        } catch (e) {
            console.error(`[AsdBrightness] DecreaseBrightness failed: ${e.message}`);
            return false;
        }
    }

    /**
     * Sets brightness of all connected displays.
     *
     * @param {number} brightness - Brightness percentage (0-100)
     * @returns {Promise<boolean>} True if successful
     */
    async setAllBrightness(brightness) {
        if (!this._proxy) {
            return false;
        }

        try {
            await this._callMethod('SetAllBrightness', brightness);
            return true;
        } catch (e) {
            console.error(`[AsdBrightness] SetAllBrightness failed: ${e.message}`);
            return false;
        }
    }

    /**
     * Checks if the daemon is available.
     *
     * @returns {boolean} True if connected to daemon
     */
    isAvailable() {
        return this._proxy !== null;
    }

    /**
     * Calls a D-Bus method with promise wrapper.
     *
     * @param {string} method - Method name
     * @param {...*} args - Method arguments
     * @returns {Promise<*>} Method result
     */
    _callMethod(method, ...args) {
        return new Promise((resolve, reject) => {
            if (!this._proxy) {
                reject(new Error('D-Bus proxy is not available'));
                return;
            }
            this._proxy[`${method}Remote`](...args, (result, error) => {
                if (error) {
                    reject(error);
                } else {
                    resolve(result);
                }
            });
        });
    }
}
