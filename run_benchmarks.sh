#!/bin/bash

# Benchmark runner script for dircachefilehash performance testing
# This script provides different benchmark configurations for various scenarios

set -e

echo "=== dircachefilehash Performance Benchmarks ==="
echo

# Check if running in CI or development environment
if [ "${CI}" = "true" ]; then
    echo "Running in CI mode - using short benchmarks only"
    BENCH_FLAGS="-short"
else
    BENCH_FLAGS=""
fi

# Parse command line arguments
BENCHMARK_TYPE="small"
VERBOSE=""
MEMORY_PROFILE=""
CPU_PROFILE=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--type)
            BENCHMARK_TYPE="$2"
            shift 2
            ;;
        -v|--verbose)
            VERBOSE="-v"
            shift
            ;;
        --memprofile)
            MEMORY_PROFILE="-memprofile=mem.prof"
            shift
            ;;
        --cpuprofile)
            CPU_PROFILE="-cpuprofile=cpu.prof"
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  -t, --type TYPE     Benchmark type: small, medium, large, all (default: small)"
            echo "  -v, --verbose       Verbose output"
            echo "  --memprofile        Generate memory profile"
            echo "  --cpuprofile        Generate CPU profile"
            echo "  -h, --help          Show this help message"
            echo
            echo "Benchmark types:"
            echo "  small     1K files (fast, suitable for CI)"
            echo "  medium    100K files (moderate, good for development)"
            echo "  large     1M files (slow, comprehensive testing)"
            echo "  all       Run all benchmark types"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Set benchmark time and iterations based on type
case $BENCHMARK_TYPE in
    small)
        BENCH_TIME="-benchtime=5s"
        echo "Running SMALL benchmarks (1K files, ~5s each)..."
        ;;
    medium)
        BENCH_TIME="-benchtime=10s"
        echo "Running MEDIUM benchmarks (100K files, ~10s each)..."
        ;;
    large)
        BENCH_TIME="-benchtime=30s"
        echo "Running LARGE benchmarks (1M files, ~30s each)..."
        ;;
    all)
        echo "Running ALL benchmarks (this may take a while)..."
        BENCH_TIME="-benchtime=10s"
        ;;
    *)
        echo "Invalid benchmark type: $BENCHMARK_TYPE"
        echo "Valid types: small, medium, large, all"
        exit 1
        ;;
esac

echo

# Function to run specific benchmarks
run_benchmark() {
    local pattern=$1
    local description=$2
    
    echo "--- $description ---"
    echo "Pattern: $pattern"
    
    go test $VERBOSE $BENCH_FLAGS $BENCH_TIME $MEMORY_PROFILE $CPU_PROFILE \
        -run='^$' -bench="$pattern" ./pkg/
    
    echo
}

# Run benchmarks based on type
case $BENCHMARK_TYPE in
    small)
        run_benchmark "BenchmarkDirectoryScanSmall" "Small Dataset Directory Scan"
        run_benchmark "BenchmarkIndexOperationsSmall" "Small Dataset Index Operations"
        ;;
    medium)
        run_benchmark "BenchmarkDirectoryScanMedium" "Medium Dataset Directory Scan"
        run_benchmark "BenchmarkIndexOperationsMedium" "Medium Dataset Index Operations"
        ;;
    large)
        run_benchmark "BenchmarkDirectoryScanLarge" "Large Dataset Directory Scan"
        ;;
    all)
        echo "=== SMALL BENCHMARKS ==="
        run_benchmark "BenchmarkDirectoryScanSmall" "Small Dataset Directory Scan"
        run_benchmark "BenchmarkIndexOperationsSmall" "Small Dataset Index Operations"
        
        echo "=== MEDIUM BENCHMARKS ==="
        run_benchmark "BenchmarkDirectoryScanMedium" "Medium Dataset Directory Scan"  
        run_benchmark "BenchmarkIndexOperationsMedium" "Medium Dataset Index Operations"
        run_benchmark "BenchmarkMemoryUsage" "Memory Usage Analysis"
        
        echo "=== LARGE BENCHMARKS ==="
        run_benchmark "BenchmarkDirectoryScanLarge" "Large Dataset Directory Scan"
        ;;
esac

# Show profile information if generated
if [ -n "$MEMORY_PROFILE" ] && [ -f "mem.prof" ]; then
    echo "Memory profile generated: mem.prof"
    echo "View with: go tool pprof mem.prof"
fi

if [ -n "$CPU_PROFILE" ] && [ -f "cpu.prof" ]; then
    echo "CPU profile generated: cpu.prof"  
    echo "View with: go tool pprof cpu.prof"
fi

echo "=== Benchmark Complete ==="