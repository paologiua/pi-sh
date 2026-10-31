package cliargs

import (
	"flag"
	"fmt"
	"strings"
)

// PrintOptions describes the registered flags using the spelling advertised by
// the clients: single-letter OpenSSH options and double-dash Connect options.
// The inventory comes from the parser, so a newly registered flag cannot be
// silently omitted from help.
func PrintOptions(f *flag.FlagSet) {
	metavars := map[string]string{
		"B": "buffer_size", "b": "batchfile", "c": "cipher",
		"D": "sftp_server_command", "F": "configfile", "i": "identity_file",
		"J": "destination", "l": "limit", "o": "option", "P": "port",
		"R": "num_requests", "S": "program", "s": "server",
		"sftp-server": "path", "X": "sftp_option",
	}
	shortVersion, longVersion := f.Lookup("V"), f.Lookup("version")
	combinedVersion := shortVersion != nil && longVersion != nil && !takesValue(shortVersion) && !takesValue(longVersion)
	fmt.Fprintln(f.Output(), "\nOptions:")
	f.VisitAll(func(option *flag.Flag) {
		if combinedVersion && (option.Name == "V" || option.Name == "version") {
			return // Both spellings are shown together immediately before help.
		}
		prefix := "-"
		if len(option.Name) > 1 {
			prefix = "--"
		}
		label := prefix + option.Name
		_, description := flag.UnquoteUsage(option)
		if takesValue(option) {
			metavar := metavars[option.Name]
			if metavar == "" {
				metavar = "value"
			}
			label += " " + metavar
		}
		if option.DefValue != "" && option.DefValue != "false" && option.DefValue != "0" {
			description += " (default " + option.DefValue + ")"
		}
		// Long labels get their own line; descriptions wrap at 96 columns.
		indent := strings.Repeat(" ", 28)
		line := fmt.Sprintf("  %-24s  ", label)
		if len(label) > 24 {
			fmt.Fprintln(f.Output(), "  "+label)
			line = indent
		}
		for _, word := range strings.Fields(description) {
			if len(line)+len(word)+1 > 96 && strings.TrimSpace(line) != "" {
				fmt.Fprintln(f.Output(), strings.TrimRight(line, " "))
				line = indent
			}
			line += word + " "
		}
		fmt.Fprintln(f.Output(), strings.TrimRight(line, " "))
	})
	if combinedVersion {
		_, description := flag.UnquoteUsage(shortVersion)
		fmt.Fprintf(f.Output(), "  %-24s  %s\n", "-V, --version", description)
	}
	fmt.Fprintf(f.Output(), "  %-24s  %s\n", "-h, --help", "Show this help without login or network access")
}
