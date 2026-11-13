package genres

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// The table a default ships as (#526): a few bytes of header, the words
// front-coded, their document frequencies, and per genre its key, positives,
// scale, bias and 16-bit weights. 30,000 words of 25 genres are 1.5 MB of
// weights and about 0.3 MB of words. It holds no book, title or path.

const tableMagic = "GWT1"

// Encode writes the table.
func (t *WordTable) Encode() []byte {
	var b bytes.Buffer
	b.WriteString(tableMagic)
	uv := func(v uint64) {
		var tmp [binary.MaxVarintLen64]byte
		b.Write(tmp[:binary.PutUvarint(tmp[:], v)])
	}
	str := func(s string) { uv(uint64(len(s))); b.WriteString(s) }
	str(t.Lang)
	uv(uint64(t.Books))
	uv(uint64(len(t.Words)))
	prev := ""
	for i, w := range t.Words {
		k := 0
		for k < len(prev) && k < len(w) && prev[k] == w[k] {
			k++
		}
		uv(uint64(k))
		str(w[k:])
		uv(uint64(t.DF[i]))
		prev = w
	}
	uv(uint64(len(t.Genres)))
	for _, g := range t.Genres {
		str(g.Key)
		uv(uint64(g.Positives))
		if !g.Trained() {
			b.WriteByte(0)
			continue
		}
		b.WriteByte(1)
		binary.Write(&b, binary.LittleEndian, g.Scale)
		binary.Write(&b, binary.LittleEndian, g.Bias)
		binary.Write(&b, binary.LittleEndian, g.Weights)
	}
	return b.Bytes()
}

// DecodeWordTable reads what Encode wrote.
func DecodeWordTable(data []byte) (*WordTable, error) {
	r := bufio.NewReader(bytes.NewReader(data))
	magic := make([]byte, len(tableMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != tableMagic {
		return nil, errors.New("genres: not a word table")
	}
	var err error
	uv := func() uint64 {
		if err != nil {
			return 0
		}
		var v uint64
		v, err = binary.ReadUvarint(r)
		return v
	}
	str := func() string {
		n := uv()
		if err != nil || n > uint64(len(data)) {
			err = cmpErr(err)
			return ""
		}
		buf := make([]byte, n)
		_, err = io.ReadFull(r, buf)
		return string(buf)
	}
	t := &WordTable{}
	t.Lang = str()
	t.Books = int(uv())
	n := uv()
	if err == nil && n > uint64(len(data)) {
		err = errBadTable
	}
	prev := ""
	for range n {
		k := uv()
		rest := str()
		df := uv()
		if err != nil || k > uint64(len(prev)) {
			return nil, cmpErr(err)
		}
		w := prev[:k] + rest
		t.Words = append(t.Words, w)
		t.DF = append(t.DF, uint32(df))
		prev = w
	}
	ng := uv()
	if err == nil && ng > uint64(len(data)) {
		err = errBadTable
	}
	for range ng {
		g := WordGenre{Key: str(), Positives: int(uv())}
		if err != nil {
			return nil, err
		}
		flag, e := r.ReadByte()
		if e != nil {
			return nil, e
		}
		if flag == 1 {
			g.Weights = make([]int16, len(t.Words))
			for _, v := range []any{&g.Scale, &g.Bias, g.Weights} {
				if e := binary.Read(r, binary.LittleEndian, v); e != nil {
					return nil, e
				}
			}
			if math.IsNaN(float64(g.Scale)) || math.IsNaN(float64(g.Bias)) {
				return nil, errBadTable
			}
		}
		t.Genres = append(t.Genres, g)
	}
	if err != nil {
		return nil, err
	}
	t.prepare()
	return t, nil
}

var errBadTable = fmt.Errorf("genres: damaged word table")

func cmpErr(err error) error {
	if err != nil {
		return err
	}
	return errBadTable
}
