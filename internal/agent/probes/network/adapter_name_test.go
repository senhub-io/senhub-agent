package network

import "testing"

func TestNormalizeAdapterName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain adapter name",
			input:    "Realtek PCIe GBE Family Controller",
			expected: "realtek pcie gbe family controller",
		},
		{
			name:     "wmi hash suffix",
			input:    "Red Hat VirtIO Ethernet Adapter #2",
			expected: "red hat virtio ethernet adapter 2",
		},
		{
			name:     "pdh underscore suffix",
			input:    "Red Hat VirtIO Ethernet Adapter _2",
			expected: "red hat virtio ethernet adapter 2",
		},
		{
			name:     "wmi parentheses and slash",
			input:    "Intel(R) PRO/1000 MT Network Connection",
			expected: "intel pro 1000 mt network connection",
		},
		{
			name:     "pdh brackets and underscore",
			input:    "Intel[R] PRO_1000 MT Network Connection",
			expected: "intel pro 1000 mt network connection",
		},
		{
			name:     "wmi numbered parentheses",
			input:    "Intel(R) Ethernet Connection (2) I219-V",
			expected: "intel ethernet connection 2 i219 v",
		},
		{
			name:     "pdh numbered brackets",
			input:    "Intel[R] Ethernet Connection [2] I219-V",
			expected: "intel ethernet connection 2 i219 v",
		},
		{
			name:     "hyphenated with hash suffix",
			input:    "Microsoft Hyper-V Network Adapter #3",
			expected: "microsoft hyper v network adapter 3",
		},
		{
			name:     "trademark symbols",
			input:    "Killer(TM) E2600 Gigabit Ethernet Controller",
			expected: "killer e2600 gigabit ethernet controller",
		},
		{
			name:     "backslash",
			input:    "Vendor Adapter\\Port 1",
			expected: "vendor adapter port 1",
		},
		{
			name:     "unicode trademark glyphs",
			input:    "Intel® Ethernet™ I225-LM",
			expected: "intel ethernet i225 lm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAdapterName(tt.input); got != tt.expected {
				t.Errorf("normalizeAdapterName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeAdapterNameMatchesWMIToPDH(t *testing.T) {
	tests := []struct {
		name string
		pdh  string
		wmi  string
	}{
		{
			name: "virtio hash-suffixed adapter",
			pdh:  "Red Hat VirtIO Ethernet Adapter _2",
			wmi:  "Red Hat VirtIO Ethernet Adapter #2",
		},
		{
			name: "intel pro with slash",
			pdh:  "Intel[R] PRO_1000 MT Network Connection",
			wmi:  "Intel(R) PRO/1000 MT Network Connection",
		},
		{
			name: "intel numbered connection",
			pdh:  "Intel[R] Ethernet Connection [2] I219-V",
			wmi:  "Intel(R) Ethernet Connection (2) I219-V",
		},
		{
			name: "broadcom hash-suffixed adapter",
			pdh:  "Broadcom NetXtreme Gigabit Ethernet _3",
			wmi:  "Broadcom NetXtreme Gigabit Ethernet #3",
		},
		{
			name: "hyperv adapter without suffix",
			pdh:  "Microsoft Hyper-V Network Adapter",
			wmi:  "Microsoft Hyper-V Network Adapter",
		},
		{
			name: "wan miniport with parentheses and hash suffix",
			pdh:  "WAN Miniport [IP] _2",
			wmi:  "WAN Miniport (IP) #2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normPDH := normalizeAdapterName(tt.pdh)
			normWMI := normalizeAdapterName(tt.wmi)
			if normPDH != normWMI {
				t.Errorf("normalized forms diverge: pdh %q -> %q, wmi %q -> %q", tt.pdh, normPDH, tt.wmi, normWMI)
			}
			if got, found := matchPDHInstance([]string{tt.pdh}, tt.wmi); !found || got != tt.pdh {
				t.Errorf("probe match predicate fails: matchPDHInstance([%q], %q) = (%q, %v)", tt.pdh, tt.wmi, got, found)
			}
		})
	}
}
