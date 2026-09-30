package version

import "testing"

func TestCurrentFallsBackToDev(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	Version = ""
	if got := Current().Version; got != "dev" {
		t.Errorf("Version = %q, want dev", got)
	}

	Version = "1.2.3"
	if got := Current().Version; got != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", got)
	}
}

func TestInfoString(t *testing.T) {
	cases := []struct {
		info Info
		want string
	}{
		{Info{Version: "1.2.3"}, "1.2.3"},
		{Info{Version: "1.2.3", Commit: "abc123"}, "1.2.3 (abc123)"},
	}
	for _, c := range cases {
		if got := c.info.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}
