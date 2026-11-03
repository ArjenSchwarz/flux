// Command flux reads the Flux API and writes its JSON responses to stdout.
package main

import (
	"context"
	"encoding/json"
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

const maxResponseBytes = 8 << 20

type options struct {
	endpoint *url.URL
	token    string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, &http.Client{Timeout: 30 * time.Second}, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, client *http.Client, stdout, stderr io.Writer) int {
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
	data, err := fetch(ctx, opts, client)
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
	base := flags.String("url", getenv("FLUX_API_URL"), "API base URL")
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
	endpoint, err := url.Parse(*base)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return options{}, errors.New("set --url or FLUX_API_URL to an HTTPS base URL without credentials, query or fragment")
	}
	endpoint = endpoint.JoinPath(command)
	endpoint.RawQuery = query.Encode()
	token := getenv("FLUX_API_TOKEN")
	if *tokenFile != "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			return options{}, errors.New("could not read token file")
		}
		token = string(data)
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return options{}, errors.New("supply a nonempty single-line token via --token-file or FLUX_API_TOKEN")
	}
	return options{endpoint: endpoint, token: token}, nil
}

func validDate(value string) bool {
	parsed, err := time.Parse(time.DateOnly, value)
	return err == nil && parsed.Format(time.DateOnly) == value
}

func fetch(ctx context.Context, opts options, client *http.Client) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("could not create request")
	}
	req.Header.Set("Authorization", "Bearer "+opts.token)
	req.Header.Set("Accept", "application/json")
	// Do not follow redirects, even to related hosts: the token belongs only
	// to the explicitly configured API endpoint.
	requestClient := *client
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := requestClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, errors.New("request canceled")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("request timed out")
		}
		// Transport errors can include the URL; never print raw errors or headers.
		return nil, errors.New("request failed (network or TLS error)")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errors.New("could not read API response")
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("API response exceeds 8 MiB limit")
	}
	if !json.Valid(data) {
		return nil, errors.New("API returned invalid JSON")
	}
	return data, nil
}
