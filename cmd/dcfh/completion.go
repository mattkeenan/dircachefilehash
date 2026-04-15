package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh]",
	Short: "Generate shell completion script",
	Long: `Generate a shell completion script for bash or zsh.

If no shell is specified, the shell is auto-detected by walking
ancestor processes via /proc.

To load completions:

  bash:
    source <(dcfh completion bash)

  zsh:
    dcfh completion zsh > "${fpath[1]}/_dcfh"
    # Then restart your shell or run: compinit
`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"bash", "zsh"},
	RunE: func(cmd *cobra.Command, args []string) error {
		var shell string
		if len(args) > 0 {
			shell = args[0]
		} else {
			shell = detectShell()
			if shell == "" {
				// No shell detected — output nothing
				return nil
			}
		}

		switch shell {
		case "bash":
			return rootCmd.GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		default:
			return fmt.Errorf("unsupported shell: %s (supported: bash, zsh)", shell)
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}

// detectShell walks ancestor PIDs via /proc/{pid}/status to find the
// nearest ancestor shell process (bash, zsh, or fish).
// Returns "" if no shell is found before reaching PID 1.
func detectShell() string {
	pid := os.Getpid()

	for pid > 1 {
		// Read parent PID from /proc/{pid}/status
		ppid, err := getParentPID(pid)
		if err != nil || ppid <= 1 {
			break
		}

		// Read process name from /proc/{ppid}/comm
		name, err := getProcessName(ppid)
		if err != nil {
			break
		}

		switch name {
		case "bash", "zsh":
			return name
		}

		pid = ppid
	}

	return ""
}

// getParentPID reads the PPid field from /proc/{pid}/status.
func getParentPID(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, fmt.Errorf("malformed PPid line")
			}
			return strconv.Atoi(fields[1])
		}
	}

	return 0, fmt.Errorf("PPid not found in /proc/%d/status", pid)
}

// getProcessName reads the process name from /proc/{pid}/comm.
func getProcessName(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
