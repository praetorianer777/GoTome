package kosync

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestPartialMD5SamplesAsKOReaderDoes(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 70_000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	path := filepath.Join(dir, "book.epub")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// 70,000 bytes hold the samples at 0, 1024, 4096, 16384 and 65536,
	// the last one short.
	want := md5.New()
	for _, at := range []int{0, 1024, 4096, 16384, 65536} {
		want.Write(data[at:min(at+1024, len(data))])
	}
	got, err := PartialMD5(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(want.Sum(nil)) {
		t.Errorf("got %s", got)
	}

	small := filepath.Join(dir, "small.epub")
	if err := os.WriteFile(small, []byte("tiny"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum([]byte("tiny"))
	if got, _ := PartialMD5(small); got != hex.EncodeToString(sum[:]) {
		t.Errorf("a file shorter than a sample: %s", got)
	}
}
