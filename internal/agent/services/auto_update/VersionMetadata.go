package auto_update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"

	"github.com/hashicorp/go-version"
)

type VersionMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func fetchVersionMetadata(
	httpClient *http.Client,
	registryUrl string,
	wantedVersion string,
) (*VersionMetadata, error) {
	formattedVersion := FormatVersionForUrl(wantedVersion)

	// All versions (including beta) use the same path format
	metadataPath := fmt.Sprintf(VERSION_METADATA_PATH, formattedVersion)

	metadataUrl, err := url.JoinPath(registryUrl, metadataPath)
	if err != nil {
		return nil, err
	}
	response, err := httpClient.Get(metadataUrl)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// A 404 error page must not decode into an empty-but-valid
	// metadata struct (#266 adjacent).
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching version metadata from %s: HTTP %d %s",
			metadataUrl, response.StatusCode, response.Status)
	}

	var versionMetadata VersionMetadata
	if err := json.NewDecoder(response.Body).Decode(&versionMetadata); err != nil {
		return nil, err
	}

	return &versionMetadata, nil
}

// IsBetaVersion checks if the version string contains the beta suffix
func IsBetaVersion(versionStr string) bool {
	return len(versionStr) >= 5 && versionStr[len(versionStr)-5:] == "-beta"
}

func FormatVersionForUrl(versionStr string) string {
	_, err := version.NewVersion(versionStr)
	if err != nil {
		return versionStr
	}
	// Used to require a `v` prefix for versions, but this is no longer required
	// return fmt.Sprintf("v%s", versionStr)
	return versionStr
}

// getVersionList issues a GET that asks intermediaries to revalidate instead of
// serving a cached body. A freshly published release must be seen immediately:
// a stale, edge-cached version list made an include_beta agent resolve a
// same-core beta over the new stable during the post-release propagation window
// (#599).
func getVersionList(httpClient *http.Client, listUrl string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, listUrl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	return httpClient.Do(req)
}

func fetchVersionList(
	httpClient *http.Client,
	registryUrl string,
) ([]VersionMetadata, error) {
	listUrl, err := url.JoinPath(registryUrl, VERSION_METADATA_LIST_PATH)
	if err != nil {
		return nil, err
	}
	listResponse, err := getVersionList(httpClient, listUrl)
	if err != nil {
		return nil, err
	}
	defer listResponse.Body.Close()

	// A non-200 (e.g. a 404 page from a mis-built registry URL) must not be
	// decoded as JSON: the "404 page not found" body's leading token parses as
	// a number and surfaces a cryptic "cannot unmarshal number into
	// []VersionMetadata" instead of the real HTTP error. fetchVersionMetadata
	// and fetchVersionListBeta already guard this; the stable list did not.
	if listResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching releases list from %s: HTTP %d %s",
			listUrl, listResponse.StatusCode, listResponse.Status)
	}

	var versionList []VersionMetadata
	if err = json.NewDecoder(listResponse.Body).Decode(&versionList); err != nil {
		return nil, err
	}

	return versionList, nil
}

func getBestMatchingVersion(
	wantedVersion version.Constraints,
	metadataList []VersionMetadata,
) (*VersionMetadata, error) {
	var matches []*version.Version
	for _, metadata := range metadataList {
		version, err := version.NewVersion(metadata.Version)
		if err != nil {
			continue
		}

		if wantedVersion.Check(version) {
			matches = append(matches, version)
		}
	}

	if len(matches) == 0 {
		return nil, nil
	}

	sort.Sort(version.Collection(matches))
	bestMatch := matches[len(matches)-1]
	for _, metadata := range metadataList {
		if metadata.Version == bestMatch.String() {
			return &metadata, nil
		}
	}

	return nil, fmt.Errorf("No matching version found")
}

func FetchBestMatchingVersion(
	httpClient *http.Client,
	registryUrl string,
	wantedVersion version.Constraints,
) (*VersionMetadata, error) {
	metadataList, err := fetchVersionList(httpClient, registryUrl)
	if err != nil {
		return nil, err
	}

	return getBestMatchingVersion(wantedVersion, metadataList)
}

// fetchVersionListBeta fetches the beta releases list
func fetchVersionListBeta(httpClient *http.Client, registryUrl string) ([]VersionMetadata, error) {
	listUrl, err := url.JoinPath(registryUrl, VERSION_METADATA_LIST_BETA_PATH)
	if err != nil {
		return nil, err
	}
	listResponse, err := getVersionList(httpClient, listUrl)
	if err != nil {
		return nil, err
	}
	defer listResponse.Body.Close()

	if listResponse.StatusCode != 200 {
		return nil, fmt.Errorf("beta releases endpoint returned HTTP %d", listResponse.StatusCode)
	}

	var versionList []VersionMetadata
	if err = json.NewDecoder(listResponse.Body).Decode(&versionList); err != nil {
		return nil, err
	}

	return versionList, nil
}

// FetchAllVersions returns all available versions (stable + beta if includeBeta)
func FetchAllVersions(httpClient *http.Client, registryUrl string, includeBeta bool) ([]VersionMetadata, error) {
	stable, err := fetchVersionList(httpClient, registryUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stable releases: %w", err)
	}

	if !includeBeta {
		return stable, nil
	}

	beta, err := fetchVersionListBeta(httpClient, registryUrl)
	if err != nil {
		// Beta fetch failure is non-fatal
		return stable, nil
	}

	// Merge, dedup by version
	seen := make(map[string]bool)
	var all []VersionMetadata
	for _, v := range stable {
		if !seen[v.Version] {
			seen[v.Version] = true
			all = append(all, v)
		}
	}
	for _, v := range beta {
		if !seen[v.Version] {
			seen[v.Version] = true
			all = append(all, v)
		}
	}

	return all, nil
}

// GetLatestVersion returns the highest version from a list
func GetLatestVersion(versions []VersionMetadata) *VersionMetadata {
	var best *version.Version
	var bestMeta *VersionMetadata

	for i, v := range versions {
		if v.Name == "latest" {
			continue
		}
		parsed, err := version.NewVersion(v.Version)
		if err != nil {
			continue
		}
		if best == nil || parsed.GreaterThan(best) {
			best = parsed
			bestMeta = &versions[i]
		}
	}

	return bestMeta
}
