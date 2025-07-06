package main

// This file contains the same option parsing system as cmd/dcfh/options.go
// It's duplicated here to maintain consistency across commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// OptionType defines the type of value an option expects
type OptionType int

const (
	OptionTypeBool OptionType = iota
	OptionTypeString
	OptionTypeInt
)

// OptionDef defines a command-line option
type OptionDef struct {
	Long        string     // Long option name (without --)
	Short       string     // Short option name (without -)
	Type        OptionType // Type of value expected
	Description string     // Help description
	Default     string     // Default value
}

// ParsedOptions holds the parsed command-line options
type ParsedOptions struct {
	values        map[string]string
	args          []string
	defs          map[string]*OptionDef
	shortMap      map[string]string // Maps short options to long options
	explicitlySet map[string]bool   // Tracks which options were explicitly set
}

// NewParsedOptions creates a new options parser
func NewParsedOptions() *ParsedOptions {
	return &ParsedOptions{
		values:        make(map[string]string),
		args:          []string{},
		defs:          make(map[string]*OptionDef),
		shortMap:      make(map[string]string),
		explicitlySet: make(map[string]bool),
	}
}

// DefineOption defines a command-line option
func (p *ParsedOptions) DefineOption(long, short string, optType OptionType, defaultValue, description string) {
	def := &OptionDef{
		Long:        long,
		Short:       short,
		Type:        optType,
		Description: description,
		Default:     defaultValue,
	}
	p.defs[long] = def
	if short != "" {
		p.shortMap[short] = long
	}

	// Set default value
	if defaultValue != "" {
		p.values[long] = defaultValue
	}
}

// Parse parses command-line arguments
func (p *ParsedOptions) Parse(args []string) error {
	consumed := make([]bool, len(args)) // Track which arguments are consumed

	// First pass: identify options and mark consumed arguments
	for i := 0; i < len(args); i++ {
		if consumed[i] {
			continue
		}

		arg := args[i]

		if strings.HasPrefix(arg, "--") {
			// Long option
			consumed[i] = true
			if err := p.parseLongOption(arg, args, &i, consumed); err != nil {
				return err
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			// Short option(s)
			consumed[i] = true
			if err := p.parseShortOptions(arg, args, &i, consumed); err != nil {
				return err
			}
		}
	}

	// Second pass: collect non-consumed arguments
	for i := 0; i < len(args); i++ {
		if !consumed[i] {
			p.args = append(p.args, args[i])
		}
	}

	return nil
}

// parseLongOption parses a long option (--option or --option=value)
func (p *ParsedOptions) parseLongOption(arg string, args []string, i *int, consumed []bool) error {
	optName := strings.TrimPrefix(arg, "--")
	var optValue string

	// Check for --option=value format
	if equalPos := strings.Index(optName, "="); equalPos != -1 {
		optValue = optName[equalPos+1:]
		optName = optName[:equalPos]
	}

	def, exists := p.defs[optName]
	if !exists {
		return fmt.Errorf("unknown option: --%s", optName)
	}

	switch def.Type {
	case OptionTypeBool:
		if optValue != "" {
			// --option=value format with boolean
			if optValue == "true" || optValue == "1" {
				p.values[optName] = "true"
				p.explicitlySet[optName] = true
			} else if optValue == "false" || optValue == "0" {
				p.values[optName] = "false"
				p.explicitlySet[optName] = true
			} else {
				return fmt.Errorf("invalid boolean value for --%s: %s", optName, optValue)
			}
		} else {
			// --option format (sets to true)
			p.values[optName] = "true"
			p.explicitlySet[optName] = true
		}
	case OptionTypeString, OptionTypeInt:
		if optValue != "" {
			// --option=value format (bound with =)
			p.values[optName] = optValue
			p.explicitlySet[optName] = true
		} else {
			// --option without value - this is an error for string/int options
			return fmt.Errorf("option --%s requires a value (use --%s=value)", optName, optName)
		}

		// Validate integer type
		if def.Type == OptionTypeInt {
			if _, err := strconv.Atoi(p.values[optName]); err != nil {
				return fmt.Errorf("invalid integer value for --%s: %s", optName, p.values[optName])
			}
		}
	}

	return nil
}

// parseShortOptions parses short option(s) (-o or -abc)
func (p *ParsedOptions) parseShortOptions(arg string, args []string, i *int, consumed []bool) error {
	shortOpts := strings.TrimPrefix(arg, "-")

	// First, count occurrences of each option for repetition handling
	optCounts := make(map[string]int)
	for _, r := range shortOpts {
		short := string(r)
		if _, exists := p.shortMap[short]; !exists {
			return fmt.Errorf("unknown option: -%s", short)
		}
		optCounts[short]++
	}

	// Process each unique option
	for short, count := range optCounts {
		longOpt := p.shortMap[short]
		def := p.defs[longOpt]

		switch def.Type {
		case OptionTypeBool:
			// For boolean options, just set to true
			p.values[longOpt] = "true"
			p.explicitlySet[longOpt] = true

		case OptionTypeInt:
			// For integer options, check if count > 1 (repetition)
			if count > 1 {
				// Use repetition count as value (e.g., -vvv = verbose level 3)
				p.values[longOpt] = strconv.Itoa(count)
				p.explicitlySet[longOpt] = true
			} else {
				// Single occurrence, look for next available integer argument
				if nextArg := p.findNextAvailableIntArg(args, *i, consumed); nextArg != "" {
					p.values[longOpt] = nextArg
					p.explicitlySet[longOpt] = true
				} else {
					// No available integer argument, default to 1
					p.values[longOpt] = "1"
					p.explicitlySet[longOpt] = true
				}
			}

		case OptionTypeString:
			// String options must consume next available argument
			if nextArg := p.findNextAvailableArg(args, *i, consumed); nextArg != "" {
				p.values[longOpt] = nextArg
				p.explicitlySet[longOpt] = true
			} else {
				return fmt.Errorf("option -%s requires a value", short)
			}
		}
	}

	return nil
}

// findNextAvailableIntArg finds the next available integer argument and marks it consumed
func (p *ParsedOptions) findNextAvailableIntArg(args []string, startIdx int, consumed []bool) string {
	for i := startIdx + 1; i < len(args); i++ {
		if !consumed[i] && !strings.HasPrefix(args[i], "-") {
			if _, err := strconv.Atoi(args[i]); err == nil {
				consumed[i] = true
				return args[i]
			}
		}
	}
	return ""
}

// findNextAvailableArg finds the next available argument and marks it consumed
func (p *ParsedOptions) findNextAvailableArg(args []string, startIdx int, consumed []bool) string {
	for i := startIdx + 1; i < len(args); i++ {
		if !consumed[i] && !strings.HasPrefix(args[i], "-") {
			consumed[i] = true
			return args[i]
		}
	}
	return ""
}

// GetString returns a string option value
func (p *ParsedOptions) GetString(option string) string {
	return p.values[option]
}

// GetInt returns an integer option value
func (p *ParsedOptions) GetInt(option string) int {
	if val, exists := p.values[option]; exists {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return 0
}

// GetBool returns a boolean option value
func (p *ParsedOptions) GetBool(option string) bool {
	if val, exists := p.values[option]; exists {
		return val == "true"
	}
	return false
}

// IsSet returns true if an option was explicitly set
func (p *ParsedOptions) IsSet(option string) bool {
	return p.explicitlySet[option]
}

// GetArgs returns non-option arguments
func (p *ParsedOptions) GetArgs() []string {
	return p.args
}

// ShowUsage displays usage information
func (p *ParsedOptions) ShowUsage(programName string) {
	fmt.Fprintf(os.Stderr, "Usage: %s [GLOBAL_OPTIONS] <command> [COMMAND_OPTIONS]\n\n", programName)
	fmt.Fprintf(os.Stderr, "Global Options:\n")

	for _, def := range p.defs {
		var shortOpt string
		if def.Short != "" {
			shortOpt = fmt.Sprintf("-%s, ", def.Short)
		}

		var valueDesc string
		switch def.Type {
		case OptionTypeString:
			valueDesc = "=VALUE"
		case OptionTypeInt:
			valueDesc = "=N"
		case OptionTypeBool:
			valueDesc = ""
		}

		fmt.Fprintf(os.Stderr, "  %s--%s%s%s\n", shortOpt, def.Long, valueDesc,
			strings.Repeat(" ", max(0, 20-len(def.Long)-len(valueDesc))))
		fmt.Fprintf(os.Stderr, "        %s\n", def.Description)
	}
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
