//go:generate go run generate_version.go

package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // G108: pprof handlers only reachable via the opt-in, localhost-only DCFH_PPROF server
	"os"
	"runtime"
	"runtime/pprof"
)

func main() {
	// Start pprof server if DCFH_PPROF environment variable is set
	if os.Getenv("DCFH_PPROF") != "" {
		go func() {
			log.Println("Starting pprof server on :6060")
			log.Println(http.ListenAndServe("localhost:6060", nil)) //nolint:gosec // G114: opt-in localhost-only debug profiler, not a network endpoint
		}()
	}

	// CPU profiling: DCFH_CPUPROFILE=path writes a CPU profile
	if cpuprofile := os.Getenv("DCFH_CPUPROFILE"); cpuprofile != "" {
		f, err := os.Create(cpuprofile) //nolint:gosec // G703: path is the user's own DCFH_CPUPROFILE env var; no trust boundary
		if err != nil {
			log.Fatalf("Failed to create CPU profile: %v", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatalf("Failed to start CPU profile: %v", err)
		}
		defer func() {
			pprof.StopCPUProfile()
			_ = f.Close()
		}()
	}

	// Memory profiling: DCFH_MEMPROFILE=path writes a heap profile on exit
	if memprofile := os.Getenv("DCFH_MEMPROFILE"); memprofile != "" {
		defer func() {
			runtime.GC()                    // get accurate heap profile
			f, err := os.Create(memprofile) //nolint:gosec // G703: path is the user's own DCFH_MEMPROFILE env var; no trust boundary
			if err != nil {
				log.Fatalf("Failed to create memory profile: %v", err)
			}
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Fatalf("Failed to write memory profile: %v", err)
			}
			_ = f.Close()
		}()
	}

	// Set up signal handling via context — cancelled on SIGINT/SIGTERM/SIGPIPE
	ctx, cancel := setupSignalContext()
	defer cancel()
	rootCmd.SetContext(ctx)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		cancel()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: cancel() called explicitly above
	}
}
