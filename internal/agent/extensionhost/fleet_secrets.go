package extensionhost

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/extension"
)

type fleetSecretStore struct {
	p secret.Provider
}

func (f fleetSecretStore) Get(name string) (string, error) {
	if !extension.ValidFleetSecretName(name) {
		return "", fmt.Errorf("invalid fleet secret name %q", name)
	}
	v, err := f.p.Get(extension.FleetSecretPrefix + name)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", extension.ErrFleetSecretNotFound, name)
		}
		return "", fmt.Errorf("get fleet secret %q: %w", name, err)
	}
	return string(v), nil
}

func (f fleetSecretStore) Set(name, value string) error {
	if !extension.ValidFleetSecretName(name) {
		return fmt.Errorf("invalid fleet secret name %q", name)
	}
	if err := f.p.Set(extension.FleetSecretPrefix+name, secret.New(value)); err != nil {
		return fmt.Errorf("set fleet secret %q: %w", name, err)
	}
	return nil
}

func (f fleetSecretStore) Delete(name string) error {
	if !extension.ValidFleetSecretName(name) {
		return fmt.Errorf("invalid fleet secret name %q", name)
	}
	if err := f.p.Delete(extension.FleetSecretPrefix + name); err != nil {
		return fmt.Errorf("delete fleet secret %q: %w", name, err)
	}
	return nil
}

func (f fleetSecretStore) List() ([]string, error) {
	all, err := f.p.List()
	if err != nil {
		return nil, fmt.Errorf("list fleet secrets: %w", err)
	}
	var names []string
	for _, full := range all {
		if strings.HasPrefix(full, extension.FleetSecretPrefix) {
			names = append(names, strings.TrimPrefix(full, extension.FleetSecretPrefix))
		}
	}
	sort.Strings(names)
	return names, nil
}

// FleetSecrets opens the active secret backend restricted to fleet names.
func (h *host) FleetSecrets() (extension.FleetSecretStore, error) {
	p, err := secret.Backend()
	if err != nil {
		return nil, fmt.Errorf("fleet secret store: %w", err)
	}
	return fleetSecretStore{p: p}, nil
}
