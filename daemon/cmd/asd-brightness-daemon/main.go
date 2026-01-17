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

// refreshDisplaysWithRetry attempts to refresh displays with linear backoff.
// It retries up to maxRetries times with increasing delays between attempts.
func refreshDisplaysWithRetry(manager *hid.Manager, maxRetries int) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Linear backoff: 500ms, 1000ms, 1500ms, ...
			backoff := time.Duration(attempt) * 500 * time.Millisecond
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

		// Success
		if attempt > 0 {
			log.Info().Int("attempts", attempt+1).Msg("Display refresh succeeded after retry")
		}
		return nil
	}
	return lastErr
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
		if err := refreshDisplaysWithRetry(manager, 3); err != nil {
			log.Error().Err(err).Msg("Failed to refresh displays after hot-plug event (all retries exhausted)")
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

		// Wait a moment for any pending USB operations to settle
		time.Sleep(500 * time.Millisecond)

		// Refresh with retry
		if err := refreshDisplaysWithRetry(manager, 3); err != nil {
			log.Error().Err(err).Msg("Recovery refresh failed (all retries exhausted)")
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
