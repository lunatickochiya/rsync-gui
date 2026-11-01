package config

import (
	"errors"
	"strings"
)

// SplitArgs splits a shell-like command line the same way rsync-gui does:
// single quotes, double quotes and backslash escapes, without variable expansion.
func SplitArgs(text string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune
	hasArg := false

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch quote {
		case '\'':
			if c == '\'' {
				quote = 0
			} else {
				current.WriteRune(c)
			}
		case '"':
			if c == '"' {
				quote = 0
			} else if c == '\\' && i+1 < len(runes) && strings.ContainsRune("\"\\$`", runes[i+1]) {
				i++
				current.WriteRune(runes[i])
			} else {
				current.WriteRune(c)
			}
		default:
			switch {
			case c == '\'' || c == '"':
				quote = c
				hasArg = true
			case c == '\\':
				if i+1 < len(runes) {
					i++
					current.WriteRune(runes[i])
				}
				hasArg = true
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
				if hasArg {
					args = append(args, current.String())
					current.Reset()
					hasArg = false
				}
			default:
				current.WriteRune(c)
				hasArg = true
			}
		}
	}
	if quote != 0 {
		return nil, errors.New("unbalanced quotes")
	}
	if hasArg {
		args = append(args, current.String())
	}
	return args, nil
}

// ShellQuote quotes one argument for a POSIX shell.
func ShellQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	safe := true
	for _, r := range arg {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') &&
			!strings.ContainsRune("_@%+=:,./-", r) {
			safe = false
			break
		}
	}
	if safe {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// JoinCommand renders a command line for previews and logs.
func JoinCommand(program string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, ShellQuote(program))
	for _, arg := range args {
		parts = append(parts, ShellQuote(arg))
	}
	return strings.Join(parts, " ")
}
