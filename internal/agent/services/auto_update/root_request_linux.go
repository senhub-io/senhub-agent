//go:build linux

package auto_update

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"

	"golang.org/x/sys/unix"

	"senhub-agent.go/internal/agent/services/logger"
)

const (
	RootUpdateRequestDir      = "/var/lib/senhub-agent/update-request"
	RootUpdateRequestFile     = "request"
	rootUpdateServiceUser     = "senhub"
	rootUpdateMarkerPath      = "/usr/lib/senhub-agent/root-update"
	rootUpdatePathUnit        = "/usr/lib/systemd/system/senhub-agent-update.path"
	rootUpdateWantsGlob       = "/etc/systemd/system/*.wants/senhub-agent-update.path"
	rootUpdateMaskPath        = "/etc/systemd/system/senhub-agent-update.path"
	rootUpdateMaxRequestBytes = 64
)

var (
	ErrRequestRefused = errors.New("update request refused")

	requestVersionRegexp = regexp.MustCompile(`^(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})(-beta\.(0|[1-9][0-9]{0,3}))?$`)
)

// ValidRequestVersion reports whether version matches the strict request format.
func ValidRequestVersion(version string) bool {
	return requestVersionRegexp.MatchString(version)
}

// parseRequestContent validates that b contains a version followed by exactly one newline.
// On malformed input it returns an error with a limited, quoted copy of the content.
func parseRequestContent(b []byte) (string, error) {
	if len(b) == 0 {
		return "", fmt.Errorf("malformed request content %q (empty)", limitedContent(b))
	}
	if b[len(b)-1] != '\n' {
		return "", fmt.Errorf("malformed request content %q (missing trailing newline)", limitedContent(b))
	}
	if bytes.Count(b, []byte{'\n'}) != 1 {
		return "", fmt.Errorf("malformed request content %q (multiple newlines)", limitedContent(b))
	}
	if bytes.ContainsAny(b, "\r\x00") {
		return "", fmt.Errorf("malformed request content %q (invalid control character)", limitedContent(b))
	}
	version := string(b[:len(b)-1])
	if !ValidRequestVersion(version) {
		return "", fmt.Errorf("malformed request content %q (version does not match format)", limitedContent(b))
	}
	return version, nil
}

func limitedContent(b []byte) string {
	if len(b) > 32 {
		b = b[:32]
	}
	return string(b)
}

type packageInstaller struct {
	requestDir  string
	requestFile string
	marker      string
	pathUnit    string
	wantsGlob   string
	maskPath    string
}

func newPackageInstaller() *packageInstaller {
	return &packageInstaller{
		requestDir:  RootUpdateRequestDir,
		requestFile: RootUpdateRequestFile,
		marker:      rootUpdateMarkerPath,
		pathUnit:    rootUpdatePathUnit,
		wantsGlob:   rootUpdateWantsGlob,
		maskPath:    rootUpdateMaskPath,
	}
}

// Available reports whether the package installation prerequisites are met.
func (p *packageInstaller) Available() bool {
	// marker must be a regular file
	if fi, err := os.Stat(p.marker); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	// path unit must exist
	if _, err := os.Stat(p.pathUnit); err != nil {
		return false
	}
	// at least one wants symlink must exist
	matches, err := filepath.Glob(p.wantsGlob)
	if err != nil || len(matches) == 0 {
		return false
	}
	// maskPath must not be a symlink to /dev/null
	if fi, err := os.Lstat(p.maskPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p.maskPath)
			if err != nil || target == "/dev/null" {
				return false
			}
		}
	} else if !os.IsNotExist(err) {
		return false
	}
	// request directory must be writable and searchable by the caller
	return unix.Access(p.requestDir, unix.W_OK|unix.X_OK) == nil
}

// Request writes an atomic version request file.
func (p *packageInstaller) Request(version string) error {
	if !ValidRequestVersion(version) {
		return fmt.Errorf("invalid version %q", version)
	}
	dirfd, err := unix.Open(p.requestDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open request directory: %w", err)
	}
	defer func() { _ = unix.Close(dirfd) }()

	// generate unique temp file name
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Errorf("generate random temp name: %w", err)
	}
	tmpName := ".request-" + hex.EncodeToString(randomBytes)

	fd, err := unix.Openat(dirfd, tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o640)
	if err != nil {
		return fmt.Errorf("create temp request file %s: %w", tmpName, err)
	}
	cleanup := func() {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(dirfd, tmpName, 0)
	}

	data := []byte(version + "\n")
	if _, err := unix.Write(fd, data); err != nil {
		cleanup()
		return fmt.Errorf("write temp request: %w", err)
	}
	if err := unix.Fchmod(fd, 0o640); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp request: %w", err)
	}
	if err := unix.Fsync(fd); err != nil {
		cleanup()
		return fmt.Errorf("fsync temp request: %w", err)
	}
	if err := unix.Close(fd); err != nil {
		_ = unix.Unlinkat(dirfd, tmpName, 0)
		return fmt.Errorf("close temp request: %w", err)
	}
	if err := unix.Renameat(dirfd, tmpName, dirfd, p.requestFile); err != nil {
		_ = unix.Unlinkat(dirfd, tmpName, 0)
		return fmt.Errorf("rename temp request to %s: %w", p.requestFile, err)
	}
	return nil
}

// RegisterPackageInstaller registers the privileged installer when the package kind supports it.
func RegisterPackageInstaller() {
	if InstallKind() == KindDeb || InstallKind() == KindRPM {
		RegisterPrivilegedInstaller(newPackageInstaller())
	}
}

// lookupServiceUID returns the numeric UID of the service user.
func lookupServiceUID() (uint32, error) {
	u, err := user.Lookup(rootUpdateServiceUser)
	if err != nil {
		return 0, fmt.Errorf("lookup user %s: %w", rootUpdateServiceUser, err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse uid %s: %w", u.Uid, err)
	}
	return uint32(uid), nil
}

// consumeRequest securely reads and removes a root update request.
// It returns the requested version or an error wrapping ErrRequestRefused.
func consumeRequest(dir, file string, serviceUID uint32, log *logger.ModuleLogger) (string, error) {
	dirfd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", fmt.Errorf("%w: open request directory: %w", ErrRequestRefused, err)
	}
	defer func() { _ = unix.Close(dirfd) }()

	// Schedule deletion regardless of outcome.
	defer func() {
		err := unix.Unlinkat(dirfd, file, 0)
		if err != nil && !errors.Is(err, unix.ENOENT) {
			log.Warn().Err(err).Str("file", file).Msg("Failed to remove request file")
		}
	}()

	var st unix.Stat_t
	if err := unix.Fstat(dirfd, &st); err != nil {
		return "", fmt.Errorf("%w: fstat request directory: %w", ErrRequestRefused, err)
	}
	if st.Uid != serviceUID && st.Uid != 0 {
		return "", fmt.Errorf("%w: request directory owner %d not allowed (expected %d or root)", ErrRequestRefused, st.Uid, serviceUID)
	}
	if st.Mode&0o022 != 0 {
		return "", fmt.Errorf("%w: request directory permissions too permissive (mode %o)", ErrRequestRefused, st.Mode&0o777)
	}

	fd, err := unix.Openat(dirfd, file, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return "", fmt.Errorf("%w: request file is a symlink", ErrRequestRefused)
		}
		return "", fmt.Errorf("%w: open request file: %w", ErrRequestRefused, err)
	}
	defer func() { _ = unix.Close(fd) }()

	if err := unix.Fstat(fd, &st); err != nil {
		return "", fmt.Errorf("%w: fstat request file: %w", ErrRequestRefused, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return "", fmt.Errorf("%w: request file is not a regular file", ErrRequestRefused)
	}
	if st.Uid != serviceUID {
		return "", fmt.Errorf("%w: request file owner %d not service user %d", ErrRequestRefused, st.Uid, serviceUID)
	}
	if st.Mode&0o022 != 0 {
		return "", fmt.Errorf("%w: request file permissions too permissive (mode %o)", ErrRequestRefused, st.Mode&0o777)
	}
	if st.Nlink != 1 {
		return "", fmt.Errorf("%w: request file has multiple hard links", ErrRequestRefused)
	}

	buf := new(bytes.Buffer)
	tmp := make([]byte, 64)
	for {
		n, err := unix.Read(fd, tmp)
		if err == unix.EINTR {
			continue
		}
		if n > 0 {
			buf.Write(tmp[:n])
			if buf.Len() > rootUpdateMaxRequestBytes {
				return "", fmt.Errorf("%w: request content too long (max %d bytes)", ErrRequestRefused, rootUpdateMaxRequestBytes)
			}
		}
		if err == nil && n == 0 {
			break
		}
		if err != nil {
			return "", fmt.Errorf("%w: reading request file: %w", ErrRequestRefused, err)
		}
	}
	content := buf.Bytes()

	version, err := parseRequestContent(content)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrRequestRefused, err)
	}
	return version, nil
}
