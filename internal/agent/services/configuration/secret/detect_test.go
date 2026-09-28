package secret

import "testing"

func TestIsSensitiveKey(t *testing.T) {
	for name, want := range map[string]bool{
		"password":  true,
		"api_key":   true,
		"admin_key": true, // opens the console and the configuration API
		"admin-key": true,
		"key_file":  false, // a path, not a secret
		"user":      false,
		"endpoint":  false,
	} {
		if got := IsSensitiveKey(name); got != want {
			t.Errorf("IsSensitiveKey(%q) = %v, want %v", name, got, want)
		}
	}
}
