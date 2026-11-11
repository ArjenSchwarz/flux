// Command flux reads the Flux API and writes its JSON responses to stdout.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ArjenSchwarz/flux/internal/client"
)

const usage = `Usage:
  flux status [options]
  flux history [options] [--days 1..31 | --from YYYY-MM-DD --to YYYY-MM-DD]
  flux day [options] YYYY-MM-DD

Options (after the command, before any positional date):
  --url URL          HTTPS API base URL; defaults to FLUX_API_URL
  --token-file PATH  Read the bearer token from a file instead of FLUX_API_TOKEN
  --help             Show this help without making a request

History defaults to 7 days including Sydney today. Explicit ranges are inclusive,
at most 31 days, and must end before Sydney today (validated by the server).
Output is the original API JSON, with a final newline if needed. Errors go to
stderr; exit codes: 0 success/help, 1 request/output failure, 2 invalid usage/config.
`

type options struct {
	path   string
	query  url.Values
	config client.Config
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, &http.Client{Timeout: 30 * time.Second}, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, httpClient *http.Client, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args, getenv)
	if errors.Is(err, flag.ErrHelp) {
		if _, err := io.WriteString(stdout, usage); err != nil {
			return fail(stderr, 1, errors.New("could not write output"))
		}
		return 0
	}
	if err != nil {
		return fail(stderr, 2, err)
	}
	data, err := client.New(opts.config, httpClient).Get(ctx, opts.path, opts.query)
	if err != nil {
		return fail(stderr, 1, err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	if _, err := stdout.Write(data); err != nil {
		return fail(stderr, 1, errors.New("could not write output"))
	}
	return 0
}

func fail(stderr io.Writer, code int, err error) int {
	_, _ = fmt.Fprintf(stderr, "flux: %s\n", err)
	return code
}

func parseOptions(args []string, getenv func(string) string) (options, error) {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		return options{}, flag.ErrHelp
	}
	command := args[0]
	if command != "status" && command != "history" && command != "day" {
		return options{}, errors.New("unknown command; run flux --help")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	// Flag errors can quote arbitrary argument values. Keep diagnostics bounded
	// and never echo potential credentials from malformed invocations.
	flags.SetOutput(io.Discard)
	base := flags.String("url", getenv(client.EnvURL), "API base URL")
	tokenFile := flags.String("token-file", "", "token file")
	var days int
	var from, to string
	if command == "history" {
		flags.IntVar(&days, "days", 7, "day count")
		flags.StringVar(&from, "from", "", "inclusive first date")
		flags.StringVar(&to, "to", "", "inclusive last date")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options{}, flag.ErrHelp
		}
		return options{}, errors.New("invalid options; run flux --help")
	}
	query := url.Values{}
	switch command {
	case "day":
		if flags.NArg() != 1 || !validDate(flags.Arg(0)) {
			return options{}, errors.New("day requires one YYYY-MM-DD date after all options")
		}
		query.Set("date", flags.Arg(0))
	case "history":
		if flags.NArg() != 0 {
			return options{}, errors.New("history does not accept positional arguments")
		}
		seen := make(map[string]bool)
		flags.Visit(func(f *flag.Flag) { seen[f.Name] = true })
		if seen["from"] || seen["to"] {
			if seen["days"] || !validDate(from) || !validDate(to) {
				return options{}, errors.New("use --days or both --from and --to with YYYY-MM-DD dates")
			}
			start, _ := time.Parse(time.DateOnly, from)
			end, _ := time.Parse(time.DateOnly, to)
			if end.Before(start) || end.After(start.AddDate(0, 0, 30)) {
				return options{}, errors.New("history range must be ordered and span at most 31 inclusive days")
			}
			query.Set("start", from)
			query.Set("end", to)
		} else {
			if days < 1 || days > 31 {
				return options{}, errors.New("--days must be between 1 and 31")
			}
			query.Set("days", fmt.Sprint(days))
		}
	case "status":
		if flags.NArg() != 0 {
			return options{}, errors.New("status does not accept positional arguments")
		}
	}
	config, err := client.LoadConfig(*base, getenv(client.EnvToken), *tokenFile)
	if err != nil {
		return options{}, err
	}
	return options{path: command, query: query, config: config}, nil
}

func validDate(value string) bool {
	parsed, err := time.Parse(time.DateOnly, value)
	return err == nil && parsed.Format(time.DateOnly) == value
}
