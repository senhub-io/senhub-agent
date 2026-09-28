package secret

import (
	"errors"
	"sort"
)

// layeredProvider writes to primary and reads primary first, then
// fallback for a name primary does not hold. It exists for one moment of
// an install's life: a host whose secrets live in the age store, where an
// operator stores a first secret with the systemd-creds backend. That
// single credential used to switch the whole host to systemd-creds, every
// age secret became "not found", the agent key among them, and the next
// start failed. Reading through both keeps every secret reachable while
// they are moved one by one.
type layeredProvider struct {
	primary  Provider
	fallback Provider
}

func (l *layeredProvider) Get(name string) (string, error) {
	v, err := l.primary.Get(name)
	if errors.Is(err, ErrNotFound) {
		return l.fallback.Get(name)
	}
	return v, err
}

func (l *layeredProvider) Set(name string, value Secret) error {
	return l.primary.Set(name, value)
}

// Delete removes the name from both stores, so a secret deleted while the
// move is under way does not reappear from the other one.
func (l *layeredProvider) Delete(name string) error {
	if err := l.primary.Delete(name); err != nil {
		return err
	}
	return l.fallback.Delete(name)
}

func (l *layeredProvider) List() ([]string, error) {
	seen := map[string]bool{}
	for _, p := range []Provider{l.primary, l.fallback} {
		names, err := p.List()
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func (l *layeredProvider) Name() string {
	return l.primary.Name() + " (reading " + l.fallback.Name() + " too)"
}
