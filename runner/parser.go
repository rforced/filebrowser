package runner

import (
	"strings"

	"github.com/rforced/filebrowser/v2/settings"
)

// ParseCommand parses the command taking in account if the current
// instance uses a shell to run the commands or just calls the binary
// directly.
func ParseCommand(s *settings.Settings, raw string) (command []string, name string, err error) {
	name, args, err := SplitCommandAndArgs(raw)
	if err != nil {
		return
	}

	if len(s.Shell) == 0 || s.Shell[0] == "" {
		command = append(command, name)
		command = append(command, args...)
	} else {
		// Always use parsed arguments instead of the raw string to prevent
		// shell operator injection (e.g., "git status; rm -rf /").
		command = append(command, s.Shell...)
		quoted := shellQuote(name, args)
		command = append(command, quoted)
	}

	return command, name, nil
}

// shellQuote builds a shell-safe string from a command name and its arguments
// by individually quoting each token with single quotes.
func shellQuote(name string, args []string) string {
	out := singleQuote(name)
	for _, a := range args {
		out += " " + singleQuote(a)
	}
	return out
}

// singleQuote wraps a string in single quotes, escaping any embedded single quotes.
func singleQuote(s string) string {
	// Replace each ' with '\'' (end quote, escaped quote, start quote)
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}
