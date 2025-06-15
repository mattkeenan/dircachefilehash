//go:build ignore
// +build ignore

// Test runner utility for dircachefilehash package
// This file provides convenient functions to run different categories of tests
// Run with: go run run_tests.go [category]

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type TestCategory struct {
	Name        string
	Description string
	Command     []string
	Timeout     time.Duration
}

var testCategories = []TestCategory{
	{
		Name:        "unit",
		Description: "Run unit tests (fast, basic functionality)",
		Command:     []string{"go", "test", "-short", "-v"},
		Timeout:     time.Minute * 2,
	},
	{
		Name:        "integration",
		Description: "Run integration tests (medium speed, real workflows)",
		Command:     []string{"go", "test", "-run", "Integration", "-v", "-timeout=10m"},
		Timeout:     time.Minute * 10,
	},
	{
		Name:        "stress",
		Description: "Run stress tests (slow, large datasets)",
		Command:     []string{"go", "test", "-run", "Stress|Concurrent|Memory", "-v", "-timeout=15m"},
		Timeout:     time.Minute * 15,
	},
	{
		Name:        "race",
		Description: "Run race condition detection tests",
		Command:     []string{"go", "test", "-race", "-tags=race", "-v", "-timeout=20m"},
		Timeout:     time.Minute * 20,
	},
	{
		Name:        "bench",
		Description: "Run performance benchmarks",
		Command:     []string{"go", "test", "-bench=.", "-benchmem", "-timeout=10m"},
		Timeout:     time.Minute * 10,
	},
	{
		Name:        "coverage",
		Description: "Run tests with coverage analysis",
		Command:     []string{"go", "test", "-cover", "-coverprofile=coverage.out"},
		Timeout:     time.Minute * 5,
	},
	{
		Name:        "all",
		Description: "Run all tests (comprehensive, very slow)",
		Command:     []string{"go", "test", "-race", "-v", "-timeout=30m"},
		Timeout:     time.Minute * 30,
	},
	{
		Name:        "quick",
		Description: "Run quick tests only (development workflow)",
		Command:     []string{"go", "test", "-short", "-race"},
		Timeout:     time.Minute * 3,
	},
	{
		Name:        "examples",
		Description: "Run example tests and documentation tests",
		Command:     []string{"go", "test", "-run", "Example", "-v"},
		Timeout:     time.Minute * 2,
	},
	{
		Name:        "edge",
		Description: "Run edge case and error condition tests",
		Command:     []string{"go", "test", "-run", "Edge|Error", "-v"},
		Timeout:     time.Minute * 5,
	},
}

func main() {
	var (
		list     = flag.Bool("list", false, "List available test categories")
		parallel = flag.Int("parallel", runtime.NumCPU(), "Number of parallel test processes")
		verbose  = flag.Bool("verbose", false, "Enable verbose output")
		dry      = flag.Bool("dry", false, "Show commands without executing")
	)
	flag.Parse()

	if *list {
		listCategories()
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Println("Usage: go run run_tests.go [options] <category>")
		fmt.Println("\nAvailable categories:")
		listCategories()
		fmt.Println("\nUse -list to see detailed descriptions")
		os.Exit(1)
	}

	category := args[0]

	// Find the test category
	var testCat *TestCategory
	for i := range testCategories {
		if testCategories[i].Name == category {
			testCat = &testCategories[i]
			break
		}
	}

	if testCat == nil {
		fmt.Printf("Unknown test category: %s\n", category)
		fmt.Println("Use -list to see available categories")
		os.Exit(1)
	}

	// Prepare command
	cmd := make([]string, len(testCat.Command))
	copy(cmd, testCat.Command)

	// Add parallel flag if supported
	if *parallel > 1 && !containsString(cmd, "-parallel") {
		cmd = append(cmd, fmt.Sprintf("-parallel=%d", *parallel))
	}

	// Add verbose flag if requested and not already present
	if *verbose && !containsString(cmd, "-v") {
		cmd = append(cmd, "-v")
	}

	fmt.Printf("Running %s tests...\n", testCat.Name)
	fmt.Printf("Description: %s\n", testCat.Description)
	fmt.Printf("Command: %s\n", strings.Join(cmd, " "))
	fmt.Printf("Timeout: %s\n", testCat.Timeout)
	fmt.Printf("Parallel: %d\n", *parallel)
	fmt.Println(strings.Repeat("-", 60))

	if *dry {
		fmt.Println("Dry run - command would be executed but wasn't")
		return
	}

	// Execute the command
	start := time.Now()
	err := runCommand(cmd, testCat.Timeout)
	duration := time.Since(start)

	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("Completed in: %s\n", duration)

	if err != nil {
		fmt.Printf("Tests failed: %v\n", err)
		os.Exit(1)
	} else {
		fmt.Printf("All %s tests passed!\n", testCat.Name)
	}

	// Special handling for coverage category
	if category == "coverage" {
		fmt.Println("\nGenerating coverage report...")
		if err := runCommand([]string{"go", "tool", "cover", "-html=coverage.out", "-o=coverage.html"}, time.Minute); err != nil {
			fmt.Printf("Failed to generate coverage report: %v\n", err)
		} else {
			fmt.Println("Coverage report generated: coverage.html")
		}
	}
}

func listCategories() {
	fmt.Printf("%-12s %s\n", "Category", "Description")
	fmt.Println(strings.Repeat("-", 70))
	for _, cat := range testCategories {
		fmt.Printf("%-12s %s\n", cat.Name, cat.Description)
	}
}

func runCommand(cmd []string, timeout time.Duration) error {
	if len(cmd) == 0 {
		return fmt.Errorf("empty command")
	}

	execCmd := exec.Command(cmd[0], cmd[1:]...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	execCmd.Stdin = os.Stdin

	// Set up timeout
	done := make(chan error, 1)
	go func() {
		done <- execCmd.Run()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		if execCmd.Process != nil {
			execCmd.Process.Kill()
		}
		return fmt.Errorf("command timed out after %s", timeout)
	}
}

func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if strings.Contains(s, str) {
			return true
		}
	}
	return false
}

// Additional utility functions for test management

func init() {
	// Add environment-specific test categories based on OS
	if runtime.GOOS == "windows" {
		// Windows-specific test adjustments
		for i := range testCategories {
			if testCategories[i].Name == "stress" {
				// Reduce timeout on Windows due to slower file operations
				testCategories[i].Timeout = time.Minute * 10
			}
		}
	}

	// Add CPU-specific adjustments
	if runtime.NumCPU() == 1 {
		// Single CPU systems - reduce parallelism
		for i := range testCategories {
			if strings.Contains(strings.Join(testCategories[i].Command, " "), "parallel") {
				testCategories[i].Command = append(testCategories[i].Command, "-parallel=1")
			}
		}
	}
}

// Custom test categories can be added here
var customCategories = map[string]TestCategory{
	"ci": {
		Name:        "ci",
		Description: "Continuous Integration test suite (optimized for CI/CD)",
		Command:     []string{"go", "test", "-short", "-race", "-cover"},
		Timeout:     time.Minute * 5,
	},
	"dev": {
		Name:        "dev",
		Description: "Development workflow tests (fast feedback)",
		Command:     []string{"go", "test", "-short", "-failfast"},
		Timeout:     time.Minute * 1,
	},
	"nightly": {
		Name:        "nightly",
		Description: "Comprehensive nightly test suite",
		Command:     []string{"go", "test", "-race", "-tags=race", "-cover", "-v", "-timeout=45m"},
		Timeout:     time.Minute * 45,
	},
}

func addCustomCategories() {
	for name, category := range customCategories {
		category.Name = name
		testCategories = append(testCategories, category)
	}
}

// Performance test runner
func runPerformanceTests() error {
	fmt.Println("Running performance baseline tests...")

	benchmarks := []string{
		"BenchmarkScanDirectory",
		"BenchmarkLoadIndex",
		"BenchmarkStatus",
		"BenchmarkFindDuplicates",
	}

	for _, bench := range benchmarks {
		fmt.Printf("Running %s...\n", bench)
		cmd := []string{"go", "test", "-bench=" + bench, "-benchtime=3s", "-count=3"}
		if err := runCommand(cmd, time.Minute*2); err != nil {
			return fmt.Errorf("benchmark %s failed: %v", bench, err)
		}
	}

	return nil
}

// Memory test runner
func runMemoryTests() error {
	fmt.Println("Running memory efficiency tests...")

	cmd := []string{"go", "test", "-run=Memory", "-v", "-timeout=10m"}
	return runCommand(cmd, time.Minute*10)
}

// Example of how to add a new test category programmatically
func addDynamicTestCategory(name, description string, command []string, timeout time.Duration) {
	newCategory := TestCategory{
		Name:        name,
		Description: description,
		Command:     command,
		Timeout:     timeout,
	}
	testCategories = append(testCategories, newCategory)
}
