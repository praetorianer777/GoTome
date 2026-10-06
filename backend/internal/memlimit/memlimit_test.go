package memlimit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheContainerLimitIsRead(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for name, c := range map[string]struct {
		files []string
		want  int64
	}{
		"cgroup v2":          {[]string{write("v2", "2147483648\n")}, 2 << 30},
		"no limit":           {[]string{write("max", "max\n")}, 0},
		"cgroup v1 no limit": {[]string{write("v1", "9223372036854771712\n")}, 0},
		"the second file":    {[]string{filepath.Join(dir, "missing"), write("v1b", "1073741824")}, 1 << 30},
		"nothing":            {[]string{filepath.Join(dir, "missing")}, 0},
		"garbage":            {[]string{write("bad", "lots")}, 0},
	} {
		if got := containerLimit(c.files); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
}
