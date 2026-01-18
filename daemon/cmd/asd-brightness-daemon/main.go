// Package main provides the entry point for the Apple Studio Display brightness daemon.
package main

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	gohid "github.com/sstallion/go-hid"

	"github.com/shini4i/asd-brightness-daemon/internal/dbus"
	"github.com/shini4i/asd-brightness-daemon/internal/hid"
	"github.com/shini4i/asd-brightness-daemon/internal/udev"
)

var (
	verbose bool
	rootCmd = &cobra.Command{
		Use:   "asd-brightness-daemon",
		Short: "D-Bus daemon for controlling Apple Studio Display brightness",
		Long: `asd-brightness-daemon is a D-Bus service that provides an interface
for controlling the brightness of Apple Studio Display monitors via USB HID.

It exposes methods for listing connected displays, getting and setting
brightness levels, and emits signals when displays are connected or disconnected.`,
		Run: func(cmd *cobra.Command, args []string) {
			run()
		},
	}
)

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
}

func run() {
	// Configure logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	log.Info().Msg("Starting asd-brightness-daemon")

	// Initialize HID library (recommended for concurrent programs)
	if err := gohid.Init(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize HID library")
	}
	defer func() {
		if err := gohid.Exit(); err != nil {
			log.Error().Err(err).Msg("Failed to cleanup HID library")
		}
	}()

	// Initialize HID manager
	manager := hid.NewManager()
	if err := manager.RefreshDisplays(); err != nil {
		log.Error().Err(err).Msg("Failed to enumerate displays")
	}

	displayCount := manager.Count()
	if displayCount == 0 {
		log.Warn().Msg("No Apple Studio Displays found")
	} else {
		log.Info().Int("count", displayCount).Msg("Found Apple Studio Displays")
	}

	// Initialize D-Bus server
	server := dbus.NewServer(manager)
	if err := server.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start D-Bus server")
	}

	// Set up device error recovery handler
	server.SetDeviceErrorHandler(createDeviceErrorHandler(manager, server))

	// Initialize udev monitor for hot-plug detection
	monitor := udev.NewMonitor(createHotplugHandler(manager, server))
	monitor.SetRecoveryHandler(createRecoveryHandler(manager, server))
	if err := monitor.Start(); err != nil {
		log.Error().Err(err).Msg("Failed to start udev monitor (hot-plug detection disabled)")
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Info().Msg("Daemon running, press Ctrl+C to stop")
	<-sigChan

	// Cleanup
	log.Info().Msg("Shutting down...")
	if err := monitor.Stop(); err != nil {
		log.Error().Err(err).Msg("Failed to stop udev monitor")
	}
	if err := server.Stop(); err != nil {
		log.Error().Err(err).Msg("Failed to stop D-Bus server")
	}
	if err := manager.Close(); err != nil {
		log.Error().Err(err).Msg("Failed to close display manager")
	}

	log.Info().Msg("Daemon stopped")
}

// refreshMu serializes display refresh operations to prevent race conditions
// between hotplug handlers and recovery handlers.
var refreshMu sync.Mutex

const (
	// maxBackoffDuration caps the exponential backoff to prevent excessive waits.
	maxBackoffDuration = 16 * time.Second
)

// refreshDisplaysWithRetry attempts to refresh displays with exponential backoff.
// It retries up to maxRetries times with exponentially increasing delays (1s, 2s, 4s, 8s, 16s).
// The function checks if displays were found, not just if RefreshDisplays succeeded,
// since USB-C dock connected displays may take time for HID interfaces to become ready.
// Returns (found, err) where found indicates whether any displays were discovered.
func refreshDisplaysWithRetry(manager *hid.Manager, maxRetries int) (bool, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, 8s, 16s (capped)
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if backoff > maxBackoffDuration {
				backoff = maxBackoffDuration
			}
			log.Debug().
				Int("attempt", attempt).
				Dur("backoff", backoff).
				Msg("Retrying display refresh")
			time.Sleep(backoff)
		}

		if err := manager.RefreshDisplays(); err != nil {
			lastErr = err
			log.Warn().
				Err(err).
				Int("attempt", attempt+1).
				Int("maxRetries", maxRetries+1).
				Msg("Display refresh failed")
			continue
		}

		// Check if we actually found displays (HID interface may not be ready yet)
		if manager.Count() > 0 {
			if attempt > 0 {
				log.Info().Int("attempts", attempt+1).Msg("Display refresh succeeded after retry")
			}
			return true, nil
		}

		// RefreshDisplays succeeded but found 0 displays - HID interface not ready yet
		log.Debug().
			Int("attempt", attempt+1).
			Int("maxRetries", maxRetries+1).
			Msg("Refresh succeeded but no displays found, HID interface may not be ready")
		lastErr = nil // Clear error since refresh itself succeeded
	}

	// All retries exhausted
	if lastErr != nil {
		return false, lastErr
	}
	return false, nil // No error, just no displays found
}

// createHotplugHandler returns an event handler that refreshes displays and emits D-Bus signals.
// The handler uses the shared refreshMu to prevent race conditions with recovery handlers.
func createHotplugHandler(manager *hid.Manager, server *dbus.Server) udev.EventHandler {
	return func(event udev.Event) {
		// Use shared mutex to serialize with recovery handler
		refreshMu.Lock()
		defer refreshMu.Unlock()

		// Get the list of displays before refresh to detect changes
		oldDisplays := make(map[string]hid.DeviceInfo)
		for _, d := range manager.ListDisplays() {
			oldDisplays[d.Serial] = d
		}

		// For add events, wait for the device to fully initialize.
		// USB devices need time to enumerate all interfaces before HID is accessible.
		// Remove events don't need this delay as the device is already gone.
		if event.Type == udev.EventAdd {
			time.Sleep(500 * time.Millisecond)
		}

		// Refresh displays with retry logic for resilience
		found, err := refreshDisplaysWithRetry(manager, 3)
		if err != nil {
			log.Error().Err(err).Msg("Failed to refresh displays after hot-plug event (all retries exhausted)")
			return
		}

		// If no displays found and no error, log and return early
		// Don't emit spurious DisplayRemoved events when we simply couldn't find displays
		if !found && len(oldDisplays) == 0 {
			log.Debug().Msg("No displays found after hot-plug event, nothing to update")
			return
		}

		// Get the list of displays after refresh
		newDisplays := make(map[string]hid.DeviceInfo)
		for _, d := range manager.ListDisplays() {
			newDisplays[d.Serial] = d
		}

		// Emit signals for added displays
		for serial, info := range newDisplays {
			if _, exists := oldDisplays[serial]; !exists {
				server.EmitDisplayAdded(serial, info.Product)
			}
		}

		// Emit signals for removed displays
		for serial := range oldDisplays {
			if _, exists := newDisplays[serial]; !exists {
				server.EmitDisplayRemoved(serial)
			}
		}
	}
}

// createDeviceErrorHandler returns a handler for device errors detected during brightness operations.
// When a stale device handle is detected (e.g., "No such device" error), this triggers a display
// refresh to clean up disconnected displays and discover any newly connected ones.
// This handles the edge case where disconnect events were missed (e.g., during system suspend).
func createDeviceErrorHandler(manager *hid.Manager, server *dbus.Server) dbus.DeviceErrorHandler {
	return func(serial string, err error) {
		// Use shared mutex to serialize with hotplug and recovery handlers
		refreshMu.Lock()
		defer refreshMu.Unlock()

		log.Info().
			Str("serial", serial).
			Err(err).
			Msg("Device error recovery: refreshing displays")

		// Get current displays before refresh
		oldDisplays := make(map[string]hid.DeviceInfo)
		for _, d := range manager.ListDisplays() {
			oldDisplays[d.Serial] = d
		}

		// Refresh displays to clean up stale entries and find new ones
		if refreshErr := manager.RefreshDisplays(); refreshErr != nil {
			log.Error().Err(refreshErr).Msg("Device error recovery: refresh failed")
			return
		}

		// Get displays after refresh
		newDisplays := make(map[string]hid.DeviceInfo)
		for _, d := range manager.ListDisplays() {
			newDisplays[d.Serial] = d
		}

		// Emit signals for changes
		for serial, info := range newDisplays {
			if _, exists := oldDisplays[serial]; !exists {
				log.Info().Str("serial", serial).Msg("Device error recovery: display found")
				server.EmitDisplayAdded(serial, info.Product)
			}
		}

		for serial := range oldDisplays {
			if _, exists := newDisplays[serial]; !exists {
				log.Info().Str("serial", serial).Msg("Device error recovery: display removed")
				server.EmitDisplayRemoved(serial)
			}
		}

		log.Info().
			Int("before", len(oldDisplays)).
			Int("after", len(newDisplays)).
			Msg("Device error recovery completed")
	}
}

// createRecoveryHandler returns a handler for netlink buffer overflow recovery.
// It triggers a display refresh to recover from potentially missed udev events.
// The handler uses the shared refreshMu to prevent race conditions with hotplug handlers.
func createRecoveryHandler(manager *hid.Manager, server *dbus.Server) udev.RecoveryHandler {
	return func() {
		// Use shared mutex to serialize with hotplug handler
		refreshMu.Lock()
		defer refreshMu.Unlock()

		log.Info().Msg("Performing recovery refresh after netlink buffer overflow")

		// Get current displays before refresh
		oldDisplays := make(map[string]hid.DeviceInfo)
		for _, d := range manager.ListDisplays() {
			oldDisplays[d.Serial] = d
		}

		// Wait for USB operations to settle - USB-C dock connected displays
		// may take several seconds for HID interfaces to become ready
		time.Sleep(2 * time.Second)

		// Refresh with retry using exponential backoff
		// Total max wait: 2s initial + 1s + 2s + 4s + 8s + 16s = ~33 seconds
		found, err := refreshDisplaysWithRetry(manager, 5)
		if err != nil {
			log.Error().Err(err).Msg("Recovery refresh failed (all retries exhausted)")
			return
		}

		// If no displays found and none existed before, nothing to do
		if !found && len(oldDisplays) == 0 {
			log.Info().Msg("Recovery refresh completed, no displays found")
			return
		}

		// Get displays after refresh
		newDisplays := make(map[string]hid.DeviceInfo)
		for _, d := range manager.ListDisplays() {
			newDisplays[d.Serial] = d
		}

		// Emit signals for any changes detected
		for serial, info := range newDisplays {
			if _, exists := oldDisplays[serial]; !exists {
				log.Info().Str("serial", serial).Msg("Display found during recovery")
				server.EmitDisplayAdded(serial, info.Product)
			}
		}

		for serial := range oldDisplays {
			if _, exists := newDisplays[serial]; !exists {
				log.Info().Str("serial", serial).Msg("Display lost during recovery")
				server.EmitDisplayRemoved(serial)
			}
		}

		log.Info().Int("displays", len(newDisplays)).Msg("Recovery refresh completed")
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Failed to execute command")
	}
}
