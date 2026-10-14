package podlogs

import (
	"testing"
	"time"
)

func TestParseLineCRI(t *testing.T) {
	t1 := time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)

	tests := []struct {
		name string
		line string
		want Entry
		ok   bool
	}{
		{
			name: "full",
			line: "2024-01-02T03:04:05.123456789Z stdout F hello world",
			want: Entry{Time: t1, Stream: "stdout", Message: "hello world"},
			ok:   true,
		},
		{
			name: "partial",
			line: "2024-01-02T03:04:05.123456789Z stderr P chunk",
			want: Entry{Time: t1, Stream: "stderr", Partial: true, Message: "chunk"},
			ok:   true,
		},
		{
			name: "multi-field partial tag",
			line: "2024-01-02T03:04:05.123456789Z stdout P:foo:bar hello",
			want: Entry{Time: t1, Stream: "stdout", Partial: true, Message: "hello"},
			ok:   true,
		},
		{
			name: "multi-field full tag",
			line: "2024-01-02T03:04:05.123456789Z stdout F:foo x",
			want: Entry{Time: t1, Stream: "stdout", Message: "x"},
			ok:   true,
		},
		{
			name: "empty message",
			line: "2024-01-02T03:04:05.123456789Z stdout F ",
			want: Entry{Time: t1, Stream: "stdout", Message: ""},
			ok:   true,
		},
		{
			name: "inner spaces",
			line: "2024-01-02T03:04:05.123456789Z stdout F a  b",
			want: Entry{Time: t1, Stream: "stdout", Message: "a  b"},
			ok:   true,
		},
		{
			name: "bad timestamp",
			line: "not-time stdout F msg",
			ok:   false,
		},
		{
			name: "bad stream",
			line: "2024-01-02T03:04:05.123456789Z stdou F msg",
			ok:   false,
		},
		{
			name: "bad tag",
			line: "2024-01-02T03:04:05.123456789Z stdout X msg",
			ok:   false,
		},
		{
			name: "missing message delimiter",
			line: "2024-01-02T03:04:05.123456789Z stdout F",
			ok:   false,
		},
		{
			name: "missing fields",
			line: "2024-01-02T03:04:05.123456789Z stdout",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseLine(tt.line)
			if ok != tt.ok {
				t.Fatalf("ParseLine(%q) ok = %v, want %v", tt.line, ok, tt.ok)
			}
			if !ok {
				return
			}
			if !got.Time.Equal(tt.want.Time) {
				t.Errorf("Time = %v, want %v", got.Time, tt.want.Time)
			}
			if got.Stream != tt.want.Stream || got.Partial != tt.want.Partial || got.Message != tt.want.Message || got.Truncated != tt.want.Truncated {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseLineDockerJSON(t *testing.T) {
	t1 := time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)

	tests := []struct {
		name string
		line string
		want Entry
		ok   bool
	}{
		{
			name: "full",
			line: `{"log":"hello\n","stream":"stdout","time":"2024-01-02T03:04:05.123456789Z"}`,
			want: Entry{Time: t1, Stream: "stdout", Message: "hello"},
			ok:   true,
		},
		{
			name: "partial",
			line: `{"log":"hello","stream":"stderr","time":"2024-01-02T03:04:05.123456789Z"}`,
			want: Entry{Time: t1, Stream: "stderr", Partial: true, Message: "hello"},
			ok:   true,
		},
		{
			name: "full empty log",
			line: `{"log":"\n","stream":"stdout","time":"2024-01-02T03:04:05.123456789Z"}`,
			want: Entry{Time: t1, Stream: "stdout", Message: ""},
			ok:   true,
		},
		{
			name: "preserves inner newline",
			line: `{"log":"a\nb\n","stream":"stdout","time":"2024-01-02T03:04:05.123456789Z"}`,
			want: Entry{Time: t1, Stream: "stdout", Message: "a\nb"},
			ok:   true,
		},
		{
			name: "malformed json",
			line: `{"log":"x\n","stream":"stdout"`,
			ok:   false,
		},
		{
			name: "missing stream",
			line: `{"log":"x\n","time":"2024-01-02T03:04:05.123456789Z"}`,
			ok:   false,
		},
		{
			name: "bad stream",
			line: `{"log":"x\n","stream":"stdoutt","time":"2024-01-02T03:04:05.123456789Z"}`,
			ok:   false,
		},
		{
			name: "bad time",
			line: `{"log":"x\n","stream":"stdout","time":"bad"}`,
			ok:   false,
		},
		{
			name: "missing time",
			line: `{"log":"x\n","stream":"stdout"}`,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseLine(tt.line)
			if ok != tt.ok {
				t.Fatalf("ParseLine(%q) ok = %v, want %v", tt.line, ok, tt.ok)
			}
			if !ok {
				return
			}
			if !got.Time.Equal(tt.want.Time) {
				t.Errorf("Time = %v, want %v", got.Time, tt.want.Time)
			}
			if got.Stream != tt.want.Stream || got.Partial != tt.want.Partial || got.Message != tt.want.Message || got.Truncated != tt.want.Truncated {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
