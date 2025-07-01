//go:generate go run generate_version.go

package main

import (
	"fmt"
	"os"
)

func main() {
	// TODO: Implement dcfhfind find-style interface
	// For now, this is a placeholder that shows usage
	
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		showUsage()
		return
	}
	
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("dcfhfind %s\n", getVersionString())
		return
	}
	
	fmt.Fprintf(os.Stderr, "dcfhfind: find-style interface for dcfh repositories (not yet implemented)\n")
	fmt.Fprintf(os.Stderr, "This tool will provide Unix find(1)-style searching through dcfh index files.\n")
	fmt.Fprintf(os.Stderr, "\nRun 'dcfhfind --help' for usage information.\n")
	os.Exit(1)
}

func showUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfhfind [starting-points...] [expressions]\n\n")
	fmt.Fprintf(os.Stderr, "Find-style interface for searching dcfh repositories.\n\n")
	fmt.Fprintf(os.Stderr, "Starting points:\n")
	fmt.Fprintf(os.Stderr, "  main              Search main index\n")
	fmt.Fprintf(os.Stderr, "  cache             Search cache index\n")
	fmt.Fprintf(os.Stderr, "  scan              Search all scan indices\n")
	fmt.Fprintf(os.Stderr, "  all               Search all indices\n")
	fmt.Fprintf(os.Stderr, "  /path/to/index    Search specific index file\n")
	fmt.Fprintf(os.Stderr, "\nExpressions:\n")
	fmt.Fprintf(os.Stderr, "  --name PATTERN    Match filename\n")
	fmt.Fprintf(os.Stderr, "  --path PATTERN    Match full path\n")
	fmt.Fprintf(os.Stderr, "  --size N          Match size\n")
	fmt.Fprintf(os.Stderr, "  --mtime N         Modified N days ago\n")
	fmt.Fprintf(os.Stderr, "  --print           Print paths (default)\n")
	fmt.Fprintf(os.Stderr, "  --ls              Detailed listing\n")
	fmt.Fprintf(os.Stderr, "\n(Full implementation coming soon)\n")
}