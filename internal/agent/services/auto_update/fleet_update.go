package auto_update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"

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
	}
	if kind == KindMSI && IsBetaVersion(target) {
		return refuse(StateFailed, target, CodeBetaMSI, nil)
	}
	if !d.Retry {
		if rec, _ := readRolledBack(osRollbackFS{}, stateDir); rec != nil {
			same := canonicalVersion(rec.FailedVersion) == canonicalVersion(target)
			if same || (!explicit && !isNewer(target, rec.FailedVersion)) {
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
		if err := privileged.Request(target); err != nil {
			setStatus(Status{State: StateFailed, Target: target, Reason: ReasonInstall})
			return false, fail(ReasonInstall, err)
		}
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

// fetchSignedList downloads a version list and its detached minisign
// signature and decodes the list only once the signature verifies.
func (a *autoUpdate) fetchSignedList(registry, path string) ([]VersionMetadata, error) {
	listURL, err := url.JoinPath(registry, path)
	if err != nil {
		return nil, fmt.Errorf("building the list URL: %w", err)
	}
	resp, err := getVersionList(a.httpClient, listURL)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", listURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fetching %s: HTTP %d", listURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVersionListBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", listURL, err)
	}
	if len(body) > maxVersionListBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", listURL, maxVersionListBytes)
	}
	sig, err := a.fetchSignature(listURL + versionListSignatureSufx)
	if err != nil {
		return nil, err
	}
	if err := verifyListSignature(body, sig); err != nil {
		return nil, fmt.Errorf("%s: %w", listURL, err)
	}
	var list []VersionMetadata
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", listURL, err)
	}
	return list, nil
}

// verifyListSignature checks a version list against the embedded key. The
// release tool signs it prehashed (mode ED); a legacy signature is accepted
// as the archives accept it.
func verifyListSignature(list, signature []byte) error {
	pub, err := embeddedSigningKey()
	if err != nil {
		return err
	}
	var sig minisign.Signature
	if err := sig.UnmarshalText(signature); err != nil {
		return fmt.Errorf("signature is unreadable: %w", err)
	}
	if !minisign.Verify(pub, list, signature) {
		return fmt.Errorf("minisign signature verification failed")
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
