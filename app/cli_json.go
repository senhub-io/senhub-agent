package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/cliexit"
)

// jsonFlag is the switch that turns a command's output into one JSON
// object on stdout.
const jsonFlag = "--json"

// schemaID names the JSON document a command prints. The version suffix
// moves only when a field is removed or changes meaning; adding a field
// keeps v1.
func schemaID(command string) string {
	return "senhub.cli." + command + "/v1"
}

// jsonHeader is the part every JSON document shares, so a consumer can
// branch on the outcome before it reads the command-specific fields.
type jsonHeader struct {
	Schema   string `json:"schema"`
	OK       bool   `json:"ok"`
	Status   string `json:"status"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// newJSONHeader builds the header for a command that ends with code. OK
// is false only for a failure: a warning or an unchanged state is a
// result the caller can use.
func newJSONHeader(command string, code int) jsonHeader {
	return jsonHeader{
		Schema:   schemaID(command),
		OK:       code != cliexit.Failure,
		Status:   cliexit.Name(code),
		ExitCode: code,
	}
}

// writeJSON prints v as one indented JSON object followed by a newline.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encoding JSON output: %w", err)
	}
	return nil
}

// extractJSONFlag removes every --json from args and reports whether one
// was there.
func extractJSONFlag(args []string) (rest []string, jsonMode bool) {
	rest = make([]string, 0, len(args))
	for _, a := range args {
		if a == jsonFlag {
			jsonMode = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, jsonMode
}

// reportFailure ends a command on err. In JSON mode the error is a JSON
// document on out (stdout stays machine-readable), otherwise a plain
// "Error:" line on stderr like fatalf. It returns the exit code to use.
func reportFailure(command string, jsonMode bool, out io.Writer, err error) int {
	if !jsonMode {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return cliexit.Failure
	}
	doc := newJSONHeader(command, cliexit.Failure)
	doc.Error = err.Error()
	if werr := writeJSON(out, doc); werr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", werr)
	}
	return cliexit.Failure
}

// captureOutput runs fn with os.Stdout redirected to memory and returns
// what it printed. The legacy text commands print straight to os.Stdout;
// this lets their output feed the JSON view without a second code path
// that could drift from the text one. The CLI is single-threaded at this
// point, so swapping the process-wide file is safe.
func captureOutput(fn func()) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("creating capture pipe: %w", err)
	}
	original := os.Stdout
	os.Stdout = w
	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- data
	}()
	defer func() {
		os.Stdout = original
	}()
	fn()
	os.Stdout = original
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("closing capture pipe: %w", err)
	}
	return string(<-done), nil
}

type versionReport struct {
	jsonHeader
	Version       string                `json:"version"`
	Commit        string                `json:"commit"`
	BuildTime     string                `json:"build_time,omitempty"`
	GoVersion     string                `json:"go_version,omitempty"`
	Environment   string                `json:"environment,omitempty"`
	ServiceBinary *versionServiceBinary `json:"service_binary,omitempty"`
}

// versionServiceBinary is the systemd service's own copy of the agent
// when it runs a different build than the CLI binary.
type versionServiceBinary struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Skew    bool   `json:"skew"`
}

// runVersion prints the version, as text or as one JSON document. It always
// exits OK: a service binary on another build is reported (a note, the
// "skew" field) but is not a failure of the version query itself.
func runVersion(jsonMode bool, out io.Writer) int {
	serviceBinary := installedServiceBinary()
	serviceVersion := binaryVersion(serviceBinary)
	note := serviceBinarySkewNote(cliArgs.Version, serviceVersion, serviceBinary)
	const code = cliexit.OK

	if !jsonMode {
		cliArgs.PrintVersion()
		if note != "" {
			fmt.Fprint(out, note)
		}
		return code
	}

	report := versionReport{
		jsonHeader:  newJSONHeader("version", code),
		Version:     cliArgs.Version,
		Commit:      cliArgs.CommitHash,
		BuildTime:   cliArgs.BuildTime,
		GoVersion:   cliArgs.GoVersion,
		Environment: cliArgs.Env,
	}
	if serviceVersion != "" && serviceBinary != "" {
		report.ServiceBinary = &versionServiceBinary{
			Path:    serviceBinary,
			Version: serviceVersion,
			Skew:    note != "",
		}
	}
	if err := writeJSON(out, report); err != nil {
		return reportFailure("version", false, out, err)
	}
	return code
}
