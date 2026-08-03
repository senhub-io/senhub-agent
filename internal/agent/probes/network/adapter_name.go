package network

import "strings"

// normalizeAdapterName collapses a WMI adapter name and a PDH instance name
// to the same comparable form. PDH escapes reserved characters when deriving
// "Network Interface" instance names from the adapter description — '(' and
// ')' become '[' and ']', while '#', '/' and '\' become '_' — whereas WMI
// keeps the raw name. Both spellings must normalize identically or a
// '#N'-suffixed adapter never matches its '_N' PDH instance and its counters
// are silently dropped (#640).
func normalizeAdapterName(name string) string {
	name = strings.ToLower(name)

	replacements := [][2]string{
		{"(r)", ""},
		{"[r]", ""},
		{"(tm)", ""},
		{"[tm]", ""},
		{"®", ""},
		{"™", ""},
		{"(", ""},
		{")", ""},
		{"[", ""},
		{"]", ""},
		{"-", " "},
		{"_", " "},
		{"#", " "},
		{"/", " "},
		{"\\", " "},
	}

	for _, r := range replacements {
		name = strings.ReplaceAll(name, r[0], r[1])
	}

	return strings.Join(strings.Fields(name), " ")
}
