package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// setupSignalHandler sets up signal handling for graceful shutdown
// Returns a channel that will be closed when a shutdown signal is received
func setupSignalHandler() <-chan struct{} {
	// Create a channel to notify of shutdown
	shutdown := make(chan struct{})

	// Create a channel to receive OS signals
	sigChan := make(chan os.Signal, 1)

	// Register the channel to receive specific signals
	// SIGINT (Ctrl+C), SIGTERM (termination), and SIGPIPE (broken pipe)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)

	// Start a goroutine to handle signals
	go func() {
		sig := <-sigChan

		// Log the signal received
		fmt.Fprintf(os.Stderr, "\nReceived signal: %v\n", sig)

		// Close the shutdown channel to notify all listeners
		close(shutdown)

		// Stop receiving signals
		signal.Stop(sigChan)

		// For SIGPIPE, we don't exit immediately as it's often recoverable
		if sig != syscall.SIGPIPE {
			fmt.Fprintf(os.Stderr, "Initiating graceful shutdown...\n")
		}
	}()

	return shutdown
}
