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
	Version     string `json:"version"`
	ProjectName string `json:"project_name"`
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

	var versionMetadata VersionMetadata
	if err := json.NewDecoder(response.Body).Decode(&versionMetadata); err != nil {
		return nil, err
	}

	return &versionMetadata, nil
}

// isBetaVersion checks if the version string contains the beta suffix
func isBetaVersion(version string) bool {
	return len(version) >= 5 && version[len(version)-5:] == "-beta"
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

// Fetch the list of versions available in the registry
func fetchVersionList(
	httpClient *http.Client,
	registryUrl string,
) ([]VersionMetadata, error) {
	listUrl, err := url.JoinPath(registryUrl, VERSION_METADATA_LIST_PATH)
	if err != nil {
		return nil, err
	}
	listResponse, err := httpClient.Get(listUrl)
	if err != nil {
		return nil, err
	}
	defer listResponse.Body.Close()

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
