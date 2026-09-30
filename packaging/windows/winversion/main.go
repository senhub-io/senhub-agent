// Command winversion prepares and checks the Windows version resource of
// the agent's executables.
//
// Without a VERSIONINFO resource an exe has no file version, and Windows
// Installer treats it as an unversioned file: an MSI of a newer build
// installed over the same product version keeps the exe already on disk.
//
//	winversion json   -version 0.6.1-beta2 -build 2864 -name senhub-agent.exe \
//	                  -description "SenHub Agent" -icon senhub.ico > winres.json
//	winversion verify -exe dist/windows-amd64/senhub-agent.exe \
//	                  -version 0.6.1-beta2 -build 2864
//
// The json output is the input of go-winres, which writes the .syso the Go
// linker embeds; see packaging/windows/embed-version-resource.sh.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "winversion:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: winversion json|verify [flags]")
	}
	switch args[0] {
	case "json":
		return runJSON(args[1:], stdout)
	case "verify":
		return runVerify(args[1:], stdout)
	default:
		return fmt.Errorf("unknown subcommand %q (want json or verify)", args[0])
	}
}

func runJSON(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("json", flag.ContinueOnError)
	spec := resourceSpec{Year: time.Now().Year()}
	fs.StringVar(&spec.Version, "version", "", "release version, e.g. 0.6.1-beta2")
	fs.IntVar(&spec.Build, "build", 0, "build number, the fourth field of the numeric version")
	fs.StringVar(&spec.OriginalFilename, "name", "", "original file name, e.g. senhub-agent.exe")
	fs.StringVar(&spec.Description, "description", "", "file description shown by Explorer")
	fs.StringVar(&spec.Icon, "icon", "", "icon file, relative to where the json is written")
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := winresJSON(spec)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", out)
	return err
}

func runVerify(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	exe := fs.String("exe", "", "executable to check")
	version := fs.String("version", "", "expected release version")
	build := fs.Int("build", 0, "expected build number")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *exe == "" || *version == "" {
		return errors.New("verify needs -exe and -version")
	}
	want, err := numericVersion(*version, *build)
	if err != nil {
		return err
	}
	vi, err := readVersionInfo(*exe)
	if err != nil {
		return fmt.Errorf("%s: %w", *exe, err)
	}
	fmt.Fprintf(stdout, "%s: FileVersion=%s ProductVersion=%s FileVersion(string)=%q ProductVersion(string)=%q CompanyName=%q ProductName=%q OriginalFilename=%q\n",
		*exe, formatNumeric(vi.FileVersion), formatNumeric(vi.ProductVersion),
		vi.Strings["FileVersion"], vi.Strings["ProductVersion"],
		vi.Strings["CompanyName"], vi.Strings["ProductName"], vi.Strings["OriginalFilename"])
	return checkVersionInfo(vi, want, *version)
}

func checkVersionInfo(vi *versionInfo, want [4]uint16, version string) error {
	if vi.FileVersion != want {
		return fmt.Errorf("numeric FileVersion is %s, want %s", formatNumeric(vi.FileVersion), formatNumeric(want))
	}
	if vi.ProductVersion != want {
		return fmt.Errorf("numeric ProductVersion is %s, want %s", formatNumeric(vi.ProductVersion), formatNumeric(want))
	}
	for _, key := range []string{"FileVersion", "ProductVersion"} {
		if vi.Strings[key] != version {
			return fmt.Errorf("string %s is %q, want %q", key, vi.Strings[key], version)
		}
	}
	for key, want := range map[string]string{"CompanyName": companyName, "ProductName": productName} {
		if vi.Strings[key] != want {
			return fmt.Errorf("string %s is %q, want %q", key, vi.Strings[key], want)
		}
	}
	return nil
}
