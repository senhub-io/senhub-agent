package host

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/data_store"
)

func newWifiProbeForTest(t *testing.T) *wifiSignalStrengthProbe {
	t.Helper()
	baseLogger := zerolog.New(os.Stderr)
	probe, err := NewWifiSignalStrengthProbe(map[string]interface{}{}, &baseLogger)
	if err != nil {
		t.Fatalf("NewWifiSignalStrengthProbe: %v", err)
	}
	wifi, ok := probe.(*wifiSignalStrengthProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	return wifi
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake executable tests require a POSIX shell")
	}
}

func wifiTagMap(t *testing.T, dp data_store.DataPoint) map[string]string {
	t.Helper()
	tags := map[string]string{}
	for _, tg := range dp.Tags {
		tags[tg.Key] = tg.Value
	}
	return tags
}

func writeFakeTool(t *testing.T, dir, name, output string) {
	t.Helper()
	script := "#!/bin/sh\ncat <<'EOF'\n" + output
	if !strings.HasSuffix(output, "\n") {
		script += "\n"
	}
	script += "EOF\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake %s: %v", name, err)
	}
}

func writeFailingTool(t *testing.T, dir, name string) {
	t.Helper()
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("writing failing fake %s: %v", name, err)
	}
}

func writeFakeIw(t *testing.T, dir, devOut string, links map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("if [ \"$1\" = \"dev\" ] && [ \"$#\" -eq 1 ]; then\n")
	b.WriteString("cat <<'EOF'\n")
	b.WriteString(devOut)
	if !strings.HasSuffix(devOut, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("EOF\n")
	b.WriteString("exit 0\n")
	b.WriteString("fi\n")
	if len(links) > 0 {
		b.WriteString("if [ \"$1\" = \"dev\" ] && [ \"$3\" = \"link\" ]; then\n")
		b.WriteString("case \"$2\" in\n")
		for ifc, out := range links {
			b.WriteString(ifc)
			b.WriteString(") cat <<'EOF'\n")
			b.WriteString(out)
			if !strings.HasSuffix(out, "\n") {
				b.WriteByte('\n')
			}
			b.WriteString("EOF\n")
			b.WriteString("exit 0\n;;\n")
		}
		b.WriteString("esac\n")
		b.WriteString("fi\n")
	}
	b.WriteString("exit 1\n")
	if err := os.WriteFile(filepath.Join(dir, "iw"), []byte(b.String()), 0o755); err != nil {
		t.Fatalf("writing fake iw: %v", err)
	}
}

const iwconfigConnectedOutput = `wlan0     IEEE 802.11  ESSID:"Freebox-20BC32"
          Mode:Managed  Frequency:2.462 GHz  Access Point: AA:BB:CC:DD:EE:FF
          Bit Rate=72.2 Mb/s   Tx-Power=31 dBm
          Retry short limit:7   RTS thr:off   Fragment thr:off
          Power Management:on
          Link Quality=48/70  Signal level=-62 dBm
          Rx invalid nwid:0  Rx invalid crypt:0  Rx invalid frag:0
          Tx excessive retries:0  Invalid misc:0   Missed beacon:0
`

const iwconfigNotAssociatedOutput = `wlan0     IEEE 802.11  ESSID:off/any
          Mode:Managed  Access Point: Not-Associated
`

const iwconfigNoWirelessOutput = `lo        no wireless extensions.

eth0      no wireless extensions.
`

const iwconfigSignalOnlyOutput = `wlan0     IEEE 802.11  ESSID:"SignalOnly"
          Mode:Managed  Access Point: 11:22:33:44:55:66
          Bit Rate=72.2 Mb/s   Tx-Power=31 dBm
          Signal level=-70 dBm
`

const iwDevTwoInterfacesOutput = `phy#0
    Unnamed/non-netdev interface
        wdev 0x3
        addr xx:xx:xx:xx:xx:ff
        type P2P-device
    Interface wlan0
        ifindex 3
        wdev 0x1
        addr xx:xx:xx:xx:xx:00
        ssid Freebox-20BC32
        type managed
    Interface wlan1
        ifindex 4
        wdev 0x2
        addr xx:xx:xx:xx:xx:01
        ssid OfficeNet
        type managed
`

const iwLinkNotConnectedOutput = "Not connected.\n"

const iwLinkOfficeOutput = `Connected to aa:bb:cc:dd:ee:ff (on wlan1)
    SSID: OfficeNet
    freq: 2462.0
    RX: 769140575 bytes (540505 packets)
    TX: 30714204 bytes (173891 packets)
    signal: -55 dBm
    rx bitrate: 65.0 MBit/s
    tx bitrate: 72.2 MBit/s
    bss flags: short-slot-time
    dtim period: 2
    beacon int: 120
`

const iwDevOneInterfaceOutput = `phy#0
    Interface wlan0
        ifindex 3
        wdev 0x1
        addr xx:xx:xx:xx:xx:00
        ssid TestNet
        type managed
`

const iwLinkTestNetConnectedOutput = `Connected to aa:bb:cc:dd:ee:ff (on wlan0)
    SSID: TestNet
    signal: -50 dBm
`

const iwconfigESSIDOutput = `wlan0     IEEE 802.11  ESSID:"TestNet"
`

const iwconfigNoESSIDOutput = `wlan0     no wireless extensions.
`

const netshEnglishConnectedOutput = `There is 1 interface on the system:

    Name                   : Wi-Fi
    Description            : Intel(R) Wi-Fi 6 AX201
    GUID                   : 00000000-0000-0000-0000-000000000000
    Physical address       : aa:bb:cc:dd:ee:ff
    State                  : connected
    SSID                   : MyWiFi
    BSSID                  : aa:bb:cc:dd:ee:ff
    Network type           : Infrastructure
    Radio type             : 802.11ac
    Authentication         : WPA2-Personal
    Cipher                 : CCMP
    Connection mode        : Profile
    Channel                : 36
    Receive rate (Mbps)    : 866.7
    Transmit rate (Mbps)   : 866.7
    Signal                 : 84%
    Profile                : MyWiFi
`

const netshFrenchConnectedOutput = `There is 1 interface on the system:

    Nom                    : Wi-Fi
    Description            : Intel(R) Wi-Fi 6 AX201
    GUID                   : 00000000-0000-0000-0000-000000000000
    Adresse physique       : aa:bb:cc:dd:ee:ff
    État                   : connecté
    SSID                   : Freebox
    Point d'accès d'identificateur SSID : aa:bb:cc:dd:ee:ff
    Type de réseau         : Infrastructure
    Type de radio          : 802.11ac
    Authentification       : WPA2-Personal
    Chiffrement            : CCMP
    Mode de connexion      : Profile
    Canal                  : 36
    Taux de réception (Mbps) : 866.7
    Taux de transmission (Mbps): 866.7
    Signal                 : 84%
    Profil                 : Freebox
`

const netshNoSignalOutput = `State                  : connected
    SSID                   : MyWiFi
    BSSID                  : aa:bb:cc:dd:ee:ff
`

const netshBadSignalOutput = `State                  : connected
    SSID                   : MyWiFi
    BSSID                  : aa:bb:cc:dd:ee:ff
    Signal                 : strong
`

func TestCollectLinuxIwconfigConnectedEmitsSignalAndQuality(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	writeFakeTool(t, dir, "iwconfig", iwconfigConnectedOutput)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

	probe := newWifiProbeForTest(t)
	points, err := probe.collectLinux()
	if err != nil {
		t.Fatalf("collectLinux: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 datapoints, got %d", len(points))
	}
	if points[0].Name != "wifi_signal_strength" {
		t.Errorf("first datapoint name = %q, want wifi_signal_strength", points[0].Name)
	}
	if points[0].Value != -62 {
		t.Errorf("signal = %v, want -62", points[0].Value)
	}
	if points[1].Name != "wifi_quality" {
		t.Errorf("second datapoint name = %q, want wifi_quality", points[1].Name)
	}
	if q := points[1].Value; q < 68.57 || q > 68.58 {
		t.Errorf("quality = %v, want 48/70 = 68.57%%", q)
	}
	for i, dp := range points {
		tags := wifiTagMap(t, dp)
		if tags["bssid"] != "AA:BB:CC:DD:EE:FF" {
			t.Errorf("point %d bssid = %q, want AA:BB:CC:DD:EE:FF", i, tags["bssid"])
		}
		if tags["ssid"] != "Freebox-20BC32" {
			t.Errorf("point %d ssid = %q, want Freebox-20BC32", i, tags["ssid"])
		}
	}
}

func TestCollectLinuxIwconfigNotAssociatedReturnsNil(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	writeFakeTool(t, dir, "iwconfig", iwconfigNotAssociatedOutput)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

	probe := newWifiProbeForTest(t)
	points, err := probe.collectLinux()
	if err != nil {
		t.Fatalf("collectLinux: %v", err)
	}
	if points != nil {
		t.Fatalf("expected nil points, got %v", points)
	}
}

func TestCollectLinuxIwconfigNoWirelessExtensionsReturnsNoPoints(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	writeFakeTool(t, dir, "iwconfig", iwconfigNoWirelessOutput)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

	probe := newWifiProbeForTest(t)
	points, err := probe.collectLinux()
	if err != nil {
		t.Fatalf("collectLinux: %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("expected no datapoints, got %v", points)
	}
}

func TestCollectLinuxSignalWithoutLinkQualityEmitsOnlySignal(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	writeFakeTool(t, dir, "iwconfig", iwconfigSignalOnlyOutput)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

	probe := newWifiProbeForTest(t)
	points, err := probe.collectLinux()
	if err != nil {
		t.Fatalf("collectLinux: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected 1 datapoint, got %d", len(points))
	}
	dp := points[0]
	if dp.Name != "wifi_signal_strength" {
		t.Errorf("datapoint name = %q, want wifi_signal_strength", dp.Name)
	}
	if dp.Value != -70 {
		t.Errorf("signal = %v, want -70", dp.Value)
	}
	tags := wifiTagMap(t, dp)
	if tags["bssid"] != "11:22:33:44:55:66" {
		t.Errorf("bssid = %q, want 11:22:33:44:55:66", tags["bssid"])
	}
	if tags["ssid"] != "SignalOnly" {
		t.Errorf("ssid = %q, want SignalOnly", tags["ssid"])
	}
}

func TestCollectLinuxIwconfigFailsFallsBackToIw(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	writeFailingTool(t, dir, "iwconfig")
	writeFakeIw(t, dir, iwDevTwoInterfacesOutput, map[string]string{
		"wlan0": iwLinkNotConnectedOutput,
		"wlan1": iwLinkOfficeOutput,
	})
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

	probe := newWifiProbeForTest(t)
	points, err := probe.collectLinux()
	if err != nil {
		t.Fatalf("collectLinux: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 datapoints, got %d", len(points))
	}
	if points[0].Name != "wifi_signal_strength" {
		t.Errorf("first datapoint name = %q, want wifi_signal_strength", points[0].Name)
	}
	if points[0].Value != -55 {
		t.Errorf("signal = %v, want -55", points[0].Value)
	}
	if q := points[1].Value; q < 78.57 || q > 78.58 {
		t.Errorf("quality = %v, want 55/70 = 78.57%%", q)
	}
	tags := wifiTagMap(t, points[0])
	if tags["bssid"] != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("bssid = %q, want aa:bb:cc:dd:ee:ff", tags["bssid"])
	}
	if tags["ssid"] != "OfficeNet" {
		t.Errorf("ssid = %q, want OfficeNet", tags["ssid"])
	}
}

func TestCollectLinuxIwNotConnectedEverywhereReturnsError(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	writeFailingTool(t, dir, "iwconfig")
	writeFakeIw(t, dir, iwDevOneInterfaceOutput, map[string]string{
		"wlan0": iwLinkNotConnectedOutput,
	})
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

	probe := newWifiProbeForTest(t)
	points, err := probe.collectLinux()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "error retrieving Wi-Fi information") {
		t.Fatalf("error = %v, want wrapped 'error retrieving Wi-Fi information'", err)
	}
	if points != nil {
		t.Fatalf("expected nil points, got %v", points)
	}
}

func TestCollectLinuxNoIwBinaryAnywhereReturnsError(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if iw := iwBinary(); iw != "" && !strings.HasPrefix(iw, dir+string(os.PathSeparator)) {
		t.Skipf("real iw binary %q exists outside the temp dir, cannot simulate no iw", iw)
	}

	probe := newWifiProbeForTest(t)
	points, err := probe.collectLinux()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "error retrieving Wi-Fi information") {
		t.Fatalf("error = %v, want wrapped 'error retrieving Wi-Fi information'", err)
	}
	if points != nil {
		t.Fatalf("expected nil points, got %v", points)
	}
}

func TestCollectWindowsNetshEnglishAndFrenchSignal(t *testing.T) {
	skipOnWindows(t)
	tests := []struct {
		name      string
		output    string
		wantSSID  string
		wantBSSID string
	}{
		{
			name:      "english connected",
			output:    netshEnglishConnectedOutput,
			wantSSID:  "MyWiFi",
			wantBSSID: "aa:bb:cc:dd:ee:ff",
		},
		{
			name:      "french layout",
			output:    netshFrenchConnectedOutput,
			wantSSID:  "Freebox",
			wantBSSID: "aa:bb:cc:dd:ee:ff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFakeTool(t, dir, "netsh", tt.output)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

			probe := newWifiProbeForTest(t)
			points, err := probe.collectWindows()
			if err != nil {
				t.Fatalf("collectWindows: %v", err)
			}
			if len(points) != 1 {
				t.Fatalf("expected 1 datapoint, got %d", len(points))
			}
			dp := points[0]
			if dp.Name != "wifi_signal_strength" {
				t.Errorf("datapoint name = %q, want wifi_signal_strength", dp.Name)
			}
			if dp.Value != -58 {
				t.Errorf("signal = %v, want -58 from 84%%", dp.Value)
			}
			tags := wifiTagMap(t, dp)
			if tags["ssid"] != tt.wantSSID {
				t.Errorf("ssid = %q, want %q", tags["ssid"], tt.wantSSID)
			}
			if tags["bssid"] != tt.wantBSSID {
				t.Errorf("bssid = %q, want %q", tags["bssid"], tt.wantBSSID)
			}
		})
	}
}

func TestCollectWindowsNetshErrors(t *testing.T) {
	skipOnWindows(t)
	tests := []struct {
		name            string
		output          string
		wantErrContains string
	}{
		{
			name:            "signal line missing",
			output:          netshNoSignalOutput,
			wantErrContains: "could not find WiFi signal strength",
		},
		{
			name:            "signal is not numeric",
			output:          netshBadSignalOutput,
			wantErrContains: "error parsing signal strength",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFakeTool(t, dir, "netsh", tt.output)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

			probe := newWifiProbeForTest(t)
			points, err := probe.collectWindows()
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("error = %v, want contains %q", err, tt.wantErrContains)
			}
			if points != nil {
				t.Fatalf("expected nil points, got %v", points)
			}
		})
	}
}

func TestCheckWifiWindowsStateDetection(t *testing.T) {
	skipOnWindows(t)
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{"english connected", "State          : connected\nBSSID          : 00:11:22:33:44:55\n", true},
		{"english disconnected", "State          : disconnected\n", false},
		{"french connected", "État          : connecté\nBSSID          : 00:11:22:33:44:55\n", true},
		{"windows 11 connected", "State          : connected\nAP BSSID       : 00:11:22:33:44:55\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFakeTool(t, dir, "netsh", tt.output)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

			probe := newWifiProbeForTest(t)
			if got := probe.checkWifiWindows(); got != tt.want {
				t.Errorf("checkWifiWindows() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckWifiLinuxFallsThroughIwIwconfigNmcli(t *testing.T) {
	skipOnWindows(t)
	tests := []struct {
		name           string
		iwConnected    bool
		iwconfigOutput string
		iwconfigFail   bool
		nmcliOutput    string
		nmcliFail      bool
		want           bool
	}{
		{
			name:        "iw directly sees a connected interface",
			iwConnected: true,
			want:        true,
		},
		{
			name:           "iw disconnected but iwconfig sees an associated ESSID",
			iwconfigOutput: iwconfigESSIDOutput,
			want:           true,
		},
		{
			name:           "iw and iwconfig miss then nmcli radio is enabled",
			iwconfigOutput: iwconfigNoESSIDOutput,
			nmcliOutput:    "enabled\n",
			want:           true,
		},
		{
			name:           "iw and iwconfig miss then nmcli radio is disabled",
			iwconfigOutput: iwconfigNoESSIDOutput,
			nmcliOutput:    "disabled\n",
			want:           false,
		},
		{
			name:         "iw and iwconfig miss then nmcli fails",
			iwconfigFail: true,
			nmcliFail:    true,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			linkOutput := iwLinkNotConnectedOutput
			if tt.iwConnected {
				linkOutput = iwLinkTestNetConnectedOutput
			}
			writeFakeIw(t, dir, iwDevOneInterfaceOutput, map[string]string{"wlan0": linkOutput})
			if tt.iwconfigFail {
				writeFailingTool(t, dir, "iwconfig")
			} else if tt.iwconfigOutput != "" {
				writeFakeTool(t, dir, "iwconfig", tt.iwconfigOutput)
			}
			if tt.nmcliFail {
				writeFailingTool(t, dir, "nmcli")
			} else if tt.nmcliOutput != "" {
				writeFakeTool(t, dir, "nmcli", tt.nmcliOutput)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")

			probe := newWifiProbeForTest(t)
			if got := probe.checkWifiLinux(); got != tt.want {
				t.Errorf("checkWifiLinux() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQualityFromDBMClamps(t *testing.T) {
	tests := []struct {
		name string
		dbm  int
		want float64
	}{
		{"below zero clamps to zero", -111, 0},
		{"above one hundred clamps to one hundred", -30, 100},
		{"mid scale", -75, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qualityFromDBm(tt.dbm); got != tt.want {
				t.Errorf("qualityFromDBm(%d) = %v, want %v", tt.dbm, got, tt.want)
			}
		})
	}
}

func TestParseIwDevInterfacesEmptyInput(t *testing.T) {
	if got := parseIwDevInterfaces(""); len(got) != 0 {
		t.Errorf("parseIwDevInterfaces(\"\") = %v, want no interfaces", got)
	}
}

func TestParseIwLinkRejectsIncompleteLinks(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantSSID  string
		wantBSSID string
		wantDBM   int
		wantOK    bool
	}{
		{
			name:      "bssid without signal",
			in:        "Connected to aa:bb:cc:dd:ee:ff\n\tSSID: TestNet\n",
			wantSSID:  "TestNet",
			wantBSSID: "aa:bb:cc:dd:ee:ff",
			wantDBM:   0,
			wantOK:    false,
		},
		{
			name:      "signal without bssid",
			in:        "SSID: TestNet\n\tsignal: -55 dBm\n",
			wantSSID:  "TestNet",
			wantBSSID: "",
			wantDBM:   -55,
			wantOK:    false,
		},
		{
			name:      "malformed signal",
			in:        "Connected to aa:bb:cc:dd:ee:ff\n\tsignal: strong dBm\n",
			wantSSID:  "",
			wantBSSID: "aa:bb:cc:dd:ee:ff",
			wantDBM:   0,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssid, bssid, dbm, ok := parseIwLink(tt.in)
			if ssid != tt.wantSSID || bssid != tt.wantBSSID || dbm != tt.wantDBM || ok != tt.wantOK {
				t.Errorf("parseIwLink(%q) = (%q, %q, %d, %v), want (%q, %q, %d, %v)",
					tt.in, ssid, bssid, dbm, ok, tt.wantSSID, tt.wantBSSID, tt.wantDBM, tt.wantOK)
			}
		})
	}
}

func TestWifiProbeIntervalAndTargetStrategies(t *testing.T) {
	probe := newWifiProbeForTest(t)

	if got := probe.GetInterval(); got != 60*time.Second {
		t.Errorf("GetInterval() = %v, want 60s", got)
	}

	got := probe.GetTargetStrategies()
	want := []string{"senhub", "prtg", "http", "otlp"}
	if len(got) != len(want) {
		t.Fatalf("GetTargetStrategies() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GetTargetStrategies()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
