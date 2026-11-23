package auto_update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"aead.dev/minisign"

	"senhub-agent.go/internal/agent/cliArgs"
)

// Refusal codes of an update a server drives.
const (
	CodeTargetNotPublished   = "target_not_published"
	CodeDowngradeNotAllowed  = "downgrade_not_allowed"
	CodeDowngradeMSI         = "downgrade_msi"
	CodeBetaMSI              = "beta_msi"
	codeRolledBackPrefix     = "rolled_back:"
	maxVersionListBytes      = 1 << 20
	versionListSignatureSufx = ".minisig"
)

func (a *autoUpdate) goosName() string {
	if a.goos != "" {
		return a.goos
	}
	return runtime.GOOS
}

// effectiveSettings is auto_update.enabled and auto_update.version as in
// force: a server directive replaces both, the local file gives them
// otherwise.
func (a *autoUpdate) effectiveSettings() (enabled bool, version string) {
	cfg := a.configSource.GetConfiguration().Agent
	if d := CurrentDirective(); d != nil {
		version = d.TargetVersion
		if version == "" {
			version = "latest"
		}
		return d.Auto, version
	}
	return cfg.UpdateEnabled, cfg.Version
}

// checkAfterDirective is the directive hook: the setting in force changed, so
// the update check runs now.
func (a *autoUpdate) checkAfterDirective() {
	enabled, _ := a.effectiveSettings()
	a.noteEnabled(enabled)
	a.logger.Info().Bool("enabled", enabled).Msg("Update directive changed; checking now")
	if err := a.PeriodicalCheckForUpdate(); err != nil {
		a.logger.Error().Err(err).Msg("Failed to check for update after a directive change")
	}
}

// updateForDirective runs one update cycle under a server directive. A
// refusal is an outcome, reported in the status and returned as no error; an
// error is a download or install that failed and may be tried again.
func (a *autoUpdate) updateForDirective(d Directive, registryUrl string) (bool, error) {
	a.msiUpgradeLaunched.Store(false)
	a.resolved = ""
	stateDir := a.rollbackEnvFor("").stateDir

	if !d.Auto {
		a.logger.Debug().Msg("Update check skipped: the server turned the automatic update off")
		settleStatus(stateDir)
		return false, nil
	}

	registry := a.GetRegistryUrl(registryUrl)
	kind := InstallKind()
	current := cliArgs.Version
	setStatus(Status{State: StateChecking, Target: d.TargetVersion})

	refuse := func(state, target, reason string, err error) (bool, error) {
		setStatus(Status{State: state, Target: target, Reason: reason})
		ev := a.logger.Warn().Str("reason", reason).Str("target", target)
		if err != nil {
			ev = ev.Err(err)
		}
		ev.Msg("Update refused")
		return false, nil
	}

	explicit := d.TargetVersion != ""
	target := d.TargetVersion
	if explicit {
		if _, _, err := parseSemver(target); err != nil {
			return refuse(StateFailed, target, CodeTargetNotPublished, err)
		}
	}

	list, err := a.admissibleVersions(registry, target)
	if err != nil {
		return refuse(StateFailed, target, ReasonSignature, err)
	}

	if explicit {
		if !publishes(list, target) {
			return refuse(StateFailed, target, CodeTargetNotPublished, nil)
		}
	} else {
		latest := GetLatestVersion(list)
		if latest == nil {
			return refuse(StateFailed, "", CodeTargetNotPublished, fmt.Errorf("the signed list holds no release"))
		}
		target = latest.Version
	}
	a.resolved = target

	cmp, err := compareSemver(target, current)
	if err != nil {
		return refuse(StateFailed, target, ReasonInstall, fmt.Errorf("comparing %s with the running %s: %w", target, current, err))
	}
	if cmp == 0 || (cmp < 0 && !explicit) {
		a.logger.Debug().Str("current_version", current).Str("target", target).Msg("No update required")
		settleStatus(stateDir)
		return false, nil
	}
	if cmp < 0 {
		if !d.AllowDowngrade {
			return refuse(StateFailed, target, CodeDowngradeNotAllowed, nil)
		}
		if kind == KindMSI {
			return refuse(StateFailed, target, CodeDowngradeMSI, nil)
		}
		if kind == KindDeb || kind == KindRPM {
			return refuse(StateFailed, target, ReasonNotReplace, errors.New("downgrade needs a package install"))
		}
	}
	if kind == KindMSI && IsBetaVersion(target) {
		return refuse(StateFailed, target, CodeBetaMSI, nil)
	}
	if rec, _ := readRolledBack(osRollbackFS{}, stateDir); rec != nil {
		same := canonicalVersion(rec.FailedVersion) == canonicalVersion(target)
		if same || (!explicit && !isNewer(target, rec.FailedVersion)) {
			if !a.consumeRetry(stateDir, d, target) {
				return refuse(StateRolledBack, target, codeRolledBackPrefix+canonicalVersion(rec.FailedVersion), nil)
			}
		}
	}

	privileged := registeredPrivilegedInstaller()
	if !canReplaceSelf(a.goosName(), kind) {
		return refuse(StateFailed, target, ReasonNotReplace, nil)
	}
	if privileged == nil && a.rollbackEnvFor("").stagedAwaitingRestart(target) {
		a.logger.Info().Str("version", target).Msg("Update skipped: this version is already installed and waits for a service restart")
		return false, nil
	}

	a.logger.Info().Str("current_version", current).Str("expected_version", target).Msg("Update required")
	setStatus(Status{State: StateDownloading, Target: target})

	switch {
	case kind == KindMSI:
		msiURL, err := a.GetMsiUrl(registry, target)
		if err != nil {
			setStatus(Status{State: StateFailed, Target: target, Reason: ReasonInstall})
			return false, fail(ReasonInstall, err)
		}
		if err := a.msiInstaller()(msiURL, target); err != nil {
			setStatus(Status{State: StateFailed, Target: target, Reason: failureReason(err)})
			return false, err
		}
		setStatus(Status{State: StateInstalling, Target: target})
		return true, nil
	case a.goosName() == "linux" && privileged != nil:
		setStatus(Status{State: StateInstalling, Target: target})
		requestedAt := time.Now()
		if err := privileged.Request(target); err != nil {
			setStatus(Status{State: StateFailed, Target: target, Reason: ReasonInstall})
			return false, fail(ReasonInstall, err)
		}
		a.watchRootResult(target, requestedAt)
		return false, nil
	}

	binaryURL, err := a.GetBinaryUrl(registry, target)
	if err != nil {
		setStatus(Status{State: StateFailed, Target: target, Reason: ReasonInstall})
		return false, fail(ReasonInstall, err)
	}
	if err := a.doUpdate(binaryURL, target); err != nil {
		setStatus(Status{State: StateFailed, Target: target, Reason: failureReason(err)})
		return false, err
	}
	return true, nil
}

// msiInstaller is the MSI hand-over; tests replace it.
func (a *autoUpdate) msiInstaller() func(msiURL, version string) error {
	if a.installMSI != nil {
		return a.installMSI
	}
	return a.applyMsiUpdate
}

const retryConsumedFileName = "update-retry-consumed.json"

type retryConsumed struct {
	Revision uint64 `json:"revision"`
	Version  string `json:"version"`
}

// consumeRetry reports whether the retry of the directive may lift the
// rolled-back memory now. It may once per revision and version: the
// directive is applied again after every restart, and a target that keeps
// failing to start would otherwise be tried again and again until the
// service manager gives up. The use is recorded before the install starts.
func (a *autoUpdate) consumeRetry(stateDir string, d Directive, target string) bool {
	if !d.Retry {
		return false
	}
	path := filepath.Join(stateDir, retryConsumedFileName)
	fs := osRollbackFS{}
	want := retryConsumed{Revision: d.Revision, Version: canonicalVersion(target)}
	if data, err := fs.ReadFile(path); err == nil {
		var prev retryConsumed
		if json.Unmarshal(data, &prev) == nil && prev == want {
			a.logger.Info().Uint64("revision", d.Revision).Str("target", target).
				Msg("Retry already used for this revision; the rolled-back version stays refused")
			return false
		}
	}
	data, err := json.Marshal(want)
	if err == nil {
		err = fs.WriteFileAtomic(path, data)
	}
	if err != nil {
		// Without the record a restart would retry again: refuse instead.
		a.logger.Warn().Err(err).Str("path", path).Msg("Could not record the use of retry; not retrying")
		return false
	}
	return true
}

func isNewer(candidate, base string) bool {
	c, err := compareSemver(candidate, base)
	return err == nil && c > 0
}

func publishes(list []VersionMetadata, target string) bool {
	for _, v := range list {
		if canonicalVersion(v.Version) == canonicalVersion(target) {
			return true
		}
	}
	return false
}

// admissibleVersions returns the versions the signed lists publish. A beta
// target is looked up in the beta list whatever include_beta says; with no
// target, the beta list joins the stable one only when include_beta is set.
// A list that cannot be fetched or whose signature does not verify against
// the key compiled into the agent is never used.
func (a *autoUpdate) admissibleVersions(registry, target string) ([]VersionMetadata, error) {
	if target != "" && IsBetaVersion(target) {
		return a.fetchSignedList(registry, VERSION_METADATA_LIST_BETA_PATH)
	}
	stable, err := a.fetchSignedList(registry, VERSION_METADATA_LIST_PATH)
	if err != nil {
		return nil, err
	}
	if target != "" || a.configSource == nil || !a.configSource.GetConfiguration().Agent.IncludeBeta {
		return stable, nil
	}
	beta, err := a.fetchSignedList(registry, VERSION_METADATA_LIST_BETA_PATH)
	if err != nil {
		a.logger.Warn().Err(err).Msg("Beta list not usable; resolving the latest version on the stable channel only")
		return DedupeVersions(stable), nil
	}
	return DedupeVersions(append(append([]VersionMetadata{}, stable...), beta...)), nil
}

const (
	manifestSeqHeader   = "X-Senhub-Manifest-Seq"
	listFetchAttempts   = 3
	listFetchRetryDelay = 3 * time.Second
)

// fetchSignedList downloads a version list and its detached minisign
// signature and decodes the list only once the signature verifies. The two
// come from two requests, so a pair published in between can give the body
// of one publication and the signature of the next: when the manifest
// sequence headers differ, or the signature fails, the pair is fetched again
// a few seconds later, twice at most, before the list is refused.
func (a *autoUpdate) fetchSignedList(registry, path string) ([]VersionMetadata, error) {
	wait := listFetchRetryDelay
	if a.listRetryWait != nil {
		wait = *a.listRetryWait
	}
	var lastErr error
	for attempt := 1; attempt <= listFetchAttempts; attempt++ {
		if attempt > 1 {
			a.logger.Warn().Err(lastErr).Int("attempt", attempt).Int("of", listFetchAttempts).
				Msg("Version list and signature do not match; fetching the pair again")
			time.Sleep(wait)
		}
		list, err := a.fetchSignedListOnce(registry, path)
		if err == nil {
			return list, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (a *autoUpdate) fetchSignedListOnce(registry, path string) ([]VersionMetadata, error) {
	listURL, err := url.JoinPath(registry, path)
	if err != nil {
		return nil, fmt.Errorf("building the list URL: %w", err)
	}
	body, listSeq, err := a.getBounded(listURL, maxVersionListBytes)
	if err != nil {
		return nil, err
	}
	sig, sigSeq, err := a.getBounded(listURL+versionListSignatureSufx, 64*1024)
	if err != nil {
		return nil, err
	}
	if listSeq != sigSeq {
		return nil, fmt.Errorf("%s: list is manifest seq %q but its signature is %q", listURL, listSeq, sigSeq)
	}
	channel := listChannel(path)
	seq, err := verifyListSignature(body, sig, channel)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", listURL, err)
	}
	var list []VersionMetadata
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", listURL, err)
	}
	if err := a.acceptListSeq(channel, seq); err != nil {
		return nil, fmt.Errorf("%s: %w", listURL, err)
	}
	return list, nil
}

// getBounded reads a response of at most limit bytes and returns it with its
// manifest sequence header.
func (a *autoUpdate) getBounded(u string, limit int) ([]byte, string, error) {
	resp, err := getVersionList(a.httpClient, u)
	if err != nil {
		return nil, "", fmt.Errorf("fetching %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetching %s: HTTP %d", u, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading %s: %w", u, err)
	}
	if len(data) > limit {
		return nil, "", fmt.Errorf("%s is larger than %d bytes", u, limit)
	}
	return data, resp.Header.Get(manifestSeqHeader), nil
}

func listChannel(path string) string {
	if path == VERSION_METADATA_LIST_BETA_PATH {
		return "beta"
	}
	return "stable"
}

// listCommentRe is the trusted comment of a signed list: the channel it
// belongs to and the publication time, which grows with each publication.
var listCommentRe = regexp.MustCompile(`^senhub-releases channel=(stable|beta) seq=(\d{1,18})$`)

// verifyListSignature checks a version list against the embedded key and
// returns the seq of its trusted comment. The release tool signs it
// prehashed (mode ED); a legacy signature is accepted as the archives accept
// it. minisign's verification covers the trusted comment, so the channel and
// the seq cannot be changed without the key. The comment must be exactly
// "senhub-releases channel=<stable|beta> seq=<integer>" and name the channel
// of the path the list was fetched from.
func verifyListSignature(list, signature []byte, channel string) (int64, error) {
	pub, err := embeddedSigningKey()
	if err != nil {
		return 0, err
	}
	var sig minisign.Signature
	if err := sig.UnmarshalText(signature); err != nil {
		return 0, fmt.Errorf("signature is unreadable: %w", err)
	}
	if !minisign.Verify(pub, list, signature) {
		return 0, fmt.Errorf("minisign signature verification failed")
	}
	m := listCommentRe.FindStringSubmatch(sig.TrustedComment)
	if m == nil {
		return 0, fmt.Errorf("trusted comment %q is missing or malformed", sig.TrustedComment)
	}
	if m[1] != channel {
		return 0, fmt.Errorf("list is signed for the %s channel, fetched as %s", m[1], channel)
	}
	seq, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("trusted comment seq: %w", err)
	}
	return seq, nil
}

const listSeqFileName = "update-list-seq.json"

// acceptListSeq refuses a list older than the newest one accepted for its
// channel and records a newer one durably in the state directory. A fresh
// state accepts the first valid list.
func (a *autoUpdate) acceptListSeq(channel string, seq int64) error {
	env := a.rollbackEnvFor("")
	if a.seqDir != "" {
		env.stateDir = a.seqDir
	}
	path := filepath.Join(env.stateDir, listSeqFileName)
	floors := map[string]int64{}
	if env.stateDir != "" {
		data, err := env.fs.ReadFile(path)
		switch {
		case err == nil:
			if jerr := json.Unmarshal(data, &floors); jerr != nil {
				a.logger.Warn().Err(jerr).Str("path", path).Msg("List sequence record unreadable; treating the state as fresh")
				floors = map[string]int64{}
			}
		case !errors.Is(err, os.ErrNotExist):
			a.logger.Warn().Err(err).Str("path", path).Msg("List sequence record unreadable; treating the state as fresh")
		}
	}
	if seq < floors[channel] {
		return fmt.Errorf("list seq %d is older than the %d already accepted for the %s channel", seq, floors[channel], channel)
	}
	if seq == floors[channel] || env.stateDir == "" {
		return nil
	}
	floors[channel] = seq
	data, err := json.Marshal(floors)
	if err == nil {
		err = env.fs.WriteFileAtomic(path, data)
	}
	if err != nil {
		a.logger.Warn().Err(err).Str("path", path).Msg("Could not record the list sequence; a replayed older list would not be noticed after a restart")
	}
	return nil
}

var semverRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func parseSemver(v string) (core [3]int, pre []string, err error) {
	m := semverRe.FindStringSubmatch(canonicalVersion(v))
	if m == nil {
		return core, nil, fmt.Errorf("%q is not a semantic version", v)
	}
	for i := 0; i < 3; i++ {
		n, convErr := strconv.Atoi(m[i+1])
		if convErr != nil {
			return core, nil, fmt.Errorf("%q: %w", v, convErr)
		}
		core[i] = n
	}
	if m[4] != "" {
		pre = strings.Split(m[4], ".")
	}
	return core, pre, nil
}

// compareSemver orders two versions by semantic versioning 2.0, section 11:
// numeric identifiers of a pre-release compare as numbers and rank below
// alphanumeric ones, a longer pre-release outranks its own prefix, and a
// release outranks its pre-releases. Build metadata is ignored.
func compareSemver(a, b string) (int, error) {
	ac, apre, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	bc, bpre, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if ac[i] != bc[i] {
			if ac[i] < bc[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	switch {
	case len(apre) == 0 && len(bpre) == 0:
		return 0, nil
	case len(apre) == 0:
		return 1, nil
	case len(bpre) == 0:
		return -1, nil
	}
	for i := 0; i < len(apre) && i < len(bpre); i++ {
		if c := comparePreIdent(apre[i], bpre[i]); c != 0 {
			return c, nil
		}
	}
	switch {
	case len(apre) < len(bpre):
		return -1, nil
	case len(apre) > len(bpre):
		return 1, nil
	}
	return 0, nil
}

func comparePreIdent(a, b string) int {
	an, aErr := strconv.ParseUint(a, 10, 64)
	bn, bErr := strconv.ParseUint(b, 10, 64)
	switch {
	case aErr == nil && bErr == nil:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		}
		return 0
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}
