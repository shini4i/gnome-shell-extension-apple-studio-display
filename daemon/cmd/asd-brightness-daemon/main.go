// Package main provides the entry point for the Apple Studio Display brightness daemon.
package main

import (
	"context"
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

	// Graceful shutdown with timeout
	log.Info().Msg("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	shutdownDone := make(chan struct{})
	go func() {
		if err := monitor.Stop(); err != nil {
			log.Error().Err(err).Msg("Failed to stop udev monitor")
		}
		if err := server.Stop(); err != nil {
			log.Error().Err(err).Msg("Failed to stop D-Bus server")
		}
		if err := manager.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close display manager")
		}
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		log.Info().Msg("Daemon stopped gracefully")
	case <-ctx.Done():
		log.Warn().Dur("timeout", shutdownTimeout).Msg("Shutdown timed out, forcing exit")
	}
}

// refreshMu serializes display refresh operations to prevent race conditions
// between hotplug handlers and recovery handlers.
//
// Design rationale: This is package-level because:
// 1. The daemon is a single-instance application (only one run() execution)
// 2. The mutex is shared by closures created in createHotplugHandler,
//    createDeviceErrorHandler, and createRecoveryHandler
// 3. Encapsulating in a struct would add complexity without benefit for this use case
// 4. The handlers need to coordinate access to the shared Manager state
var refreshMu sync.Mutex

const (
	// maxBackoffDuration caps the exponential backoff to prevent excessive waits.
	maxBackoffDuration = 16 * time.Second

	// shutdownTimeout is the maximum time to wait for graceful shutdown.
	shutdownTimeout = 10 * time.Second
)

// displayChanges represents changes detected during a display refresh.
type displayChanges struct {
	added   []hid.DeviceInfo // displays that were added
	removed []string         // serials of displays that were removed
}

// getDisplaysSnapshot returns a map of serial -> DeviceInfo for current displays.
func getDisplaysSnapshot(manager *hid.Manager) map[string]hid.DeviceInfo {
	snapshot := make(map[string]hid.DeviceInfo)
	for _, d := range manager.ListDisplays() {
		snapshot[d.Serial] = d
	}
	return snapshot
}

// diffDisplays compares old and new snapshots and returns the changes.
func diffDisplays(oldDisplays, newDisplays map[string]hid.DeviceInfo) displayChanges {
	var changes displayChanges

	for serial, info := range newDisplays {
		if _, exists := oldDisplays[serial]; !exists {
			changes.added = append(changes.added, info)
		}
	}

	for serial := range oldDisplays {
		if _, exists := newDisplays[serial]; !exists {
			changes.removed = append(changes.removed, serial)
		}
	}

	return changes
}

// emitDisplayChanges emits D-Bus signals for display changes.
func emitDisplayChanges(server *dbus.Server, changes displayChanges) {
	for _, info := range changes.added {
		server.EmitDisplayAdded(info.Serial, info.Product)
	}
	for _, serial := range changes.removed {
		server.EmitDisplayRemoved(serial)
	}
}

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

		oldDisplays := getDisplaysSnapshot(manager)

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

		newDisplays := getDisplaysSnapshot(manager)
		changes := diffDisplays(oldDisplays, newDisplays)
		emitDisplayChanges(server, changes)
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

		oldDisplays := getDisplaysSnapshot(manager)

		// Refresh displays to clean up stale entries and find new ones
		if refreshErr := manager.RefreshDisplays(); refreshErr != nil {
			log.Error().Err(refreshErr).Msg("Device error recovery: refresh failed")
			return
		}

		newDisplays := getDisplaysSnapshot(manager)
		changes := diffDisplays(oldDisplays, newDisplays)

		// Log changes for debugging
		for _, info := range changes.added {
			log.Info().Str("serial", info.Serial).Msg("Device error recovery: display found")
		}
		for _, removedSerial := range changes.removed {
			log.Info().Str("serial", removedSerial).Msg("Device error recovery: display removed")
		}

		emitDisplayChanges(server, changes)

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

		oldDisplays := getDisplaysSnapshot(manager)

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

		newDisplays := getDisplaysSnapshot(manager)
		changes := diffDisplays(oldDisplays, newDisplays)

		// Log changes for debugging
		for _, info := range changes.added {
			log.Info().Str("serial", info.Serial).Msg("Display found during recovery")
		}
		for _, removedSerial := range changes.removed {
			log.Info().Str("serial", removedSerial).Msg("Display lost during recovery")
		}

		emitDisplayChanges(server, changes)

		log.Info().Int("displays", len(newDisplays)).Msg("Recovery refresh completed")
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Failed to execute command")
	}
}
