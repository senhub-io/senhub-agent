package filetail

import "testing"

func TestHiddenByProtectHome(t *testing.T) {
	cases := []struct {
		path string
		dir  string
		want bool
	}{
		{"/home/app/logs/app.log", "/home", true},
		{"/home/*/logs/*.log", "/home", true},
		{"/home", "/home", true},
		{"/root/.pm2/logs/out.log", "/root", true},
		{"/run/user/1000/app.log", "/run/user", true},
		{"/home/../var/log/syslog", "", false},
		{"/homework/app.log", "", false},
		{"/var/log/app/*.log", "", false},
		{"/run/app.log", "", false},
		{"relative/home/app.log", "", false},
	}
	for _, c := range cases {
		dir, got := HiddenByProtectHome(c.path)
		if got != c.want || dir != c.dir {
			t.Errorf("HiddenByProtectHome(%q) = %q, %v; want %q, %v", c.path, dir, got, c.dir, c.want)
		}
	}
}
