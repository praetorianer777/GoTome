package kosync

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
)

// PartialMD5 is how KOReader names a document ("binary" matching): the MD5
// of 1 KB samples at 0, 1 KB, 4 KB, 16 KB and so on, each four times the
// one before, up to 1 GB, as far as the file goes. KOReader's first
// offset is 1024 shifted by -2 in LuaJIT, whose shift counts wrap at 32:
// 0.
func PartialMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := md5.New()
	buf := make([]byte, 1024)
	for i := -1; i <= 10; i++ {
		var offset int64
		if i >= 0 {
			offset = 1024 << (2 * i)
		}
		n, err := f.ReadAt(buf, offset)
		if n == 0 {
			if err != nil && err != io.EOF {
				return "", err
			}
			break
		}
		sum.Write(buf[:n])
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
