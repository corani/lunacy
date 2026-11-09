package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/corani/lunacy/internal/config"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "lunacy-cli:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	global := flag.NewFlagSet("lunacy-cli", flag.ContinueOnError)
	global.SetOutput(stderr)
	cfgPath := global.String("config", "", "config.yaml path (default: standard search locations)")
	global.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: lunacy-cli [--config PATH] sync [--full | --since RFC3339] [--output FILE]\n       lunacy-cli [--config PATH] book --filename PATH [--output FILE]\n       lunacy-cli [--config PATH] cover --filename PATH --output FILE")
	}
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid global flags (use --help)")
	}
	args = global.Args()
	if len(args) == 0 {
		global.Usage()
		return errors.New("command required")
	}
	command := args[0]
	if command != "sync" && command != "book" && command != "cover" {
		return errors.New("unknown command (use sync, book, or cover)")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	// Permit config selection before or after the command.
	flags.StringVar(cfgPath, "config", *cfgPath, "config.yaml path")
	output := flags.String("output", "", "output file (JSON defaults to stdout)")
	filename := flags.String("filename", "", "canonical book filename")
	full := flags.Bool("full", false, "replay all eligible books and annotations")
	since := flags.String("since", "", "replay from RFC3339 timestamp")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid command flags")
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	sinceSet, fullSet := false, false
	flags.Visit(func(f *flag.Flag) {
		sinceSet = sinceSet || f.Name == "since"
		fullSet = fullSet || f.Name == "full"
	})
	if command == "sync" {
		if *filename != "" || (*full && sinceSet) {
			return errors.New("sync requires no filename and at most one of --full and --since")
		}
	} else if *filename == "" || sinceSet || fullSet {
		return errors.New("book/cover require --filename and do not accept replay flags")
	}
	if command == "cover" && *output == "" {
		return errors.New("cover requires --output")
	}
	cfg, selected, err := config.LoadClient(*cfgPath)
	if err != nil {
		return errors.New("cannot load selected config")
	}
	if *output != "" {
		dest, err := filepath.Abs(*output)
		if err != nil {
			return errors.New("invalid output path")
		}
		if resolved, err := filepath.EvalSymlinks(dest); err == nil {
			dest = resolved
		}
		if dest == selected {
			return errors.New("output must not overwrite the config")
		}
	}
	query := url.Values{}
	if command == "sync" {
		if !*full && !sinceSet {
			*since = cfg.Client.Watermark
		}
		if sinceSet || *since != "" {
			if _, err := timestamp(*since); err != nil {
				return errors.New("invalid sync timestamp; use RFC3339 with at most millisecond precision")
			}
			query.Set("since", *since)
		}
	} else {
		query.Set("filename", *filename)
	}
	endpoint := "api/v1/" + command
	if command == "cover" {
		endpoint = "api/v1/book/cover"
	}
	body, err := request(cfg.Client, endpoint, query, command == "cover")
	if err != nil {
		return err
	}
	watermark := ""
	if command != "cover" {
		watermark, err = validateResponse(body, command, *filename, query.Get("since"))
		if err != nil {
			return fmt.Errorf("invalid API response: %w", err)
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err != nil {
			return errors.New("invalid JSON response")
		}
		pretty.WriteByte('\n')
		body = pretty.Bytes()
	}
	if *output != "" {
		if err := config.AtomicWrite(*output, body); err != nil {
			return errors.New("cannot write output file")
		}
	} else if n, err := stdout.Write(body); err != nil || n != len(body) {
		return errors.New("cannot write stdout")
	}
	if command == "sync" {
		if err := config.SaveWatermark(selected, watermark); err != nil {
			return errors.New("output succeeded but watermark save failed; next sync will replay")
		}
	}
	return nil
}

func request(cfg config.ClientConfig, endpoint string, query url.Values, cover bool) ([]byte, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") ||
		base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("client.base_url must be an HTTP(S) URL without credentials, query, or fragment")
	}
	if cfg.Timeout <= 0 || strings.Contains(cfg.Username, ":") {
		return nil, errors.New("client.timeout must be positive and username must not contain a colon")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	base.RawPath = ""
	target := base.ResolveReference(&url.URL{Path: endpoint, RawQuery: query.Encode()})
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, errors.New("cannot create HTTP request")
	}
	req.Header.Set("Accept", "application/json")
	if cover {
		req.Header.Set("Accept", "image/*")
	}
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}
	client := &http.Client{
		Timeout: cfg.Timeout,
		// Refuse every redirect, including cross-host and HTTPS downgrades.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("HTTP request failed (connection, TLS, or timeout)")
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4097))
		truncated := len(detail) > 4096
		if truncated {
			detail = detail[:4096]
		}
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		message := "invalid or non-JSON error body"
		if json.Unmarshal(detail, &envelope) == nil && envelope.Error.Message != "" {
			message = envelope.Error.Code + ": " + envelope.Error.Message
			for _, secret := range []string{cfg.Password, cfg.Username, req.Header.Get("Authorization")} {
				if secret != "" {
					message = strings.ReplaceAll(message, secret, "[redacted]")
				}
			}
		}
		if truncated {
			message += " (truncated)"
		}
		return nil, fmt.Errorf("HTTP %d: %q", resp.StatusCode, message)
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, errors.New("invalid response Content-Type")
	}
	if !cover && contentType != "application/json" {
		return nil, errors.New("invalid response Content-Type (expected application/json)")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.New("cannot read complete HTTP response")
	}
	if cover && (!strings.HasPrefix(contentType, "image/") || len(body) == 0) {
		return nil, errors.New("invalid cover response (expected non-empty image)")
	}
	return body, nil
}
