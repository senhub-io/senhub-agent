package network

import (
	"reflect"
	"testing"
)

func TestMatchPDHInstance_ExactNotSubstring(t *testing.T) {
	tests := []struct {
		name         string
		pdhInstances []string
		wmiAdapter   string
		want         string
		wantFound    bool
	}{
		{
			name:         "short adapter must not claim longer sibling listed first",
			pdhInstances: []string{"Ethernet 2", "Ethernet"},
			wmiAdapter:   "Ethernet",
			want:         "Ethernet",
			wantFound:    true,
		},
		{
			name:         "suffixed adapter claims its own instance",
			pdhInstances: []string{"Ethernet", "Ethernet 2"},
			wmiAdapter:   "Ethernet 2",
			want:         "Ethernet 2",
			wantFound:    true,
		},
		{
			name:         "wmi hash suffix reconciles with pdh underscore suffix",
			pdhInstances: []string{"Red Hat VirtIO Ethernet Adapter _2"},
			wmiAdapter:   "Red Hat VirtIO Ethernet Adapter #2",
			want:         "Red Hat VirtIO Ethernet Adapter _2",
			wantFound:    true,
		},
		{
			name:         "base name must not claim the suffixed instance",
			pdhInstances: []string{"Red Hat VirtIO Ethernet Adapter _2"},
			wmiAdapter:   "Red Hat VirtIO Ethernet Adapter",
			want:         "",
			wantFound:    false,
		},
		{
			name: "multi-nic same model picks the exact ordinal",
			pdhInstances: []string{
				"Intel[R] I350 Gigabit Network Connection",
				"Intel[R] I350 Gigabit Network Connection _2",
			},
			wmiAdapter: "Intel(R) I350 Gigabit Network Connection #2",
			want:       "Intel[R] I350 Gigabit Network Connection _2",
			wantFound:  true,
		},
		{
			name: "multi-nic same model picks the base instance",
			pdhInstances: []string{
				"Intel[R] I350 Gigabit Network Connection _2",
				"Intel[R] I350 Gigabit Network Connection",
			},
			wmiAdapter: "Intel(R) I350 Gigabit Network Connection",
			want:       "Intel[R] I350 Gigabit Network Connection",
			wantFound:  true,
		},
		{
			name:         "no instances enumerated",
			pdhInstances: nil,
			wmiAdapter:   "Ethernet",
			want:         "",
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := matchPDHInstance(tt.pdhInstances, tt.wmiAdapter)
			if got != tt.want || found != tt.wantFound {
				t.Errorf("matchPDHInstance(%v, %q) = (%q, %v), want (%q, %v)",
					tt.pdhInstances, tt.wmiAdapter, got, found, tt.want, tt.wantFound)
			}
		})
	}
}

func TestUnmatchedPDHInstances(t *testing.T) {
	instances := []string{
		"Red Hat VirtIO Ethernet Adapter",
		"Red Hat VirtIO Ethernet Adapter _2",
		"Microsoft Kernel Debug Network Adapter",
	}

	tests := []struct {
		name    string
		matched map[string]struct{}
		want    []string
	}{
		{
			name:    "nothing matched keeps enumeration order",
			matched: map[string]struct{}{},
			want:    instances,
		},
		{
			name: "partially matched returns the rest in order",
			matched: map[string]struct{}{
				"Red Hat VirtIO Ethernet Adapter _2": {},
			},
			want: []string{
				"Red Hat VirtIO Ethernet Adapter",
				"Microsoft Kernel Debug Network Adapter",
			},
		},
		{
			name: "all matched returns empty",
			matched: map[string]struct{}{
				"Red Hat VirtIO Ethernet Adapter":        {},
				"Red Hat VirtIO Ethernet Adapter _2":     {},
				"Microsoft Kernel Debug Network Adapter": {},
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unmatchedPDHInstances(instances, tt.matched)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unmatchedPDHInstances() = %v, want %v", got, tt.want)
			}
		})
	}
}
