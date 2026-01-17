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

// createHotplugHandler returns an event handler that refreshes displays and emits D-Bus signals.
// The handler serializes hot-plug event processing to prevent race conditions.
func createHotplugHandler(manager *hid.Manager, server *dbus.Server) udev.EventHandler {
	var mu sync.Mutex

	return func(event udev.Event) {
		// Serialize hot-plug event processing to prevent race conditions
		mu.Lock()
		defer mu.Unlock()

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

		// Refresh displays
		if err := manager.RefreshDisplays(); err != nil {
			log.Error().Err(err).Msg("Failed to refresh displays after hot-plug event")
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

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Failed to execute command")
	}
}
