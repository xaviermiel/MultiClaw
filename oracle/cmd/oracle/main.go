// Command oracle runs the MultiClaw off-chain oracle (Go port of oracle/src).
//
// Usage:
//
//	oracle <command> [flags]
//
// Commands:
//
//	run         run both oracles (Safe value and spending)
//	safe-value  run the Safe value oracle
//	spending    run the spending oracle
//	config      print the resolved configuration and check it
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// command is one subcommand of the oracle binary.
type command struct {
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

var commands = map[string]command{
	"config":     {summary: "print the resolved configuration and check it", run: runConfig},
	"run":        {summary: "run both oracles (Safe value and spending)", run: runAll},
	"safe-value": {summary: "run the Safe value oracle", run: runSafeValue},
	"spending":   {summary: "run the spending oracle", run: runSpending},
}

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

func dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage(stderr)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
	return cmd.run(args[1:], stdout, stderr)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: oracle <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "  %-12s %s\n", name, commands[name].summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'oracle <command> -h' for command flags.")
}

// envFileFlag registers the --env-file flag shared by every command.
func envFileFlag(fs *flag.FlagSet) *string {
	return fs.String("env-file", ".env", "dotenv file to load (missing default file is ignored; set variables take precedence)")
}

// loadEnvFile mirrors dotenv.config(): the default .env may be absent, an
// explicitly chosen file may not.
func loadEnvFile(fs *flag.FlagSet, path string) error {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "env-file" {
			explicit = true
		}
	})
	return loadEnvFileFn(path, explicit)
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("oracle "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func joinOrNone(xs []string) string {
	if len(xs) == 0 {
		return "(none)"
	}
	return strings.Join(xs, ", ")
}
