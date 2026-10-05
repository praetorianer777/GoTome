package dedupe

import (
	"encoding/binary"
	"hash/fnv"
	"strings"
	"unicode"
)

// SignatureVersion names how texts are shingled and hashed. A signature of
// another version is made again from the chunks (EnqueueUnsigned).
const SignatureVersion = 1

const (
	// shingleWords is how many words a shingle is.
	shingleWords = 5
	// permutations is how many minimum hashes a file's signature holds.
	permutations = 256
	// bands of rows each make the whole-text buckets: two texts share one
	// with a likelihood that rises steeply around a Jaccard of 0.35.
	bands = 64
	rows  = 4
	// A text is also cut into segments where a shingle's hash says so, so
	// that the same text cuts the same way in any file that holds it: the
	// inner segments of a novel are those of the omnibus it is in, which
	// the whole-text buckets miss when the novel is a small part of it.
	// segmentAverage is about how many shingles lie between two cuts after
	// the first segmentMin; none is longer than segmentMax. Short enough
	// that a short novel holds several, as the cuts of a text set in
	// another place fall into step only after one or two.
	segmentAverage = 5000
	segmentMin     = segmentAverage / 4
	segmentMax     = segmentAverage * 4
	// A segment's buckets come from its first segmentPermutations minimum
	// hashes, in segmentBands bands of rows. They are there to find the same
	// text set in another book, whose segments are equal; texts that are
	// only alike are found by the whole-text buckets.
	segmentPermutations = segmentBands * rows
	segmentBands        = 2
	// minShingles is the fewest shingles a text needs to be signed: shorter
	// ones pair by chance with anything sharing a few phrases.
	minShingles = 500
)

// seeds turn one shingle hash into each permutation's.
var seeds = func() [permutations]uint64 {
	var out [permutations]uint64
	x := uint64(0x9e3779b97f4a7c15)
	for i := range out {
		x = splitmix(x)
		out[i] = x
	}
	return out
}()

func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// Bucket is one band's key: files that share one are compared.
type Bucket struct {
	Band int16
	Key  int64
}

// Signature is a text's minimum hashes and the buckets they fall in.
type Signature struct {
	// Shingles is how many different shingles the text has, which the
	// containment estimate needs.
	Shingles int
	Values   []uint64
	Buckets  []Bucket
}

// words are the text's words, lower case, without punctuation.
func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

// shingles hashes every run of shingleWords words of the texts, read as one,
// in order.
func shingles(texts []string) []uint64 {
	var all []string
	for _, t := range texts {
		all = append(all, words(t)...)
	}
	if len(all) < shingleWords {
		return nil
	}
	out := make([]uint64, 0, len(all)-shingleWords+1)
	h := fnv.New64a()
	for i := 0; i+shingleWords <= len(all); i++ {
		h.Reset()
		for j, w := range all[i : i+shingleWords] {
			if j > 0 {
				h.Write([]byte{' '})
			}
			h.Write([]byte(w))
		}
		out = append(out, h.Sum64())
	}
	return out
}

// minimums is the n minimum hashes of the set under the first n
// permutations.
func minimums(set map[uint64]struct{}, n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = ^uint64(0)
	}
	for h := range set {
		for i := range out {
			if v := splitmix(h ^ seeds[i]); v < out[i] {
				out[i] = v
			}
		}
	}
	return out
}

func bucketKey(band int, values []uint64) int64 {
	h := fnv.New64a()
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(band))
	h.Write(b[:])
	for _, v := range values {
		binary.LittleEndian.PutUint64(b[:], v)
		h.Write(b[:])
	}
	return int64(h.Sum64())
}

// Sign makes the signature of the texts, read in order as one. It is false
// for a text too short to sign.
func Sign(texts []string) (Signature, bool) {
	all := shingles(texts)
	set := make(map[uint64]struct{}, len(all))
	for _, h := range all {
		set[h] = struct{}{}
	}
	if len(set) < minShingles {
		return Signature{}, false
	}
	sig := Signature{Shingles: len(set), Values: minimums(set, permutations)}
	seen := map[Bucket]bool{}
	add := func(b Bucket) {
		if !seen[b] {
			seen[b] = true
			sig.Buckets = append(sig.Buckets, b)
		}
	}
	for b := range bands {
		add(Bucket{Band: int16(b), Key: bucketKey(b, sig.Values[b*rows:(b+1)*rows])})
	}
	for _, seg := range segments(all) {
		values := minimums(seg, segmentPermutations)
		for b := range segmentBands {
			band := bands + b
			add(Bucket{Band: int16(band), Key: bucketKey(band, values[b*rows:(b+1)*rows])})
		}
	}
	return sig, true
}

// segments cuts the shingles where their own hashes say, so that the same
// run of text is cut the same way wherever it stands.
func segments(all []uint64) []map[uint64]struct{} {
	var out []map[uint64]struct{}
	cur := map[uint64]struct{}{}
	n := 0
	for _, h := range all {
		cur[h] = struct{}{}
		n++
		if (n >= segmentMin && (h>>32)%segmentAverage == 0) || n >= segmentMax {
			out = append(out, cur)
			cur, n = map[uint64]struct{}{}, 0
		}
	}
	if n >= segmentMin {
		out = append(out, cur)
	}
	return out
}

// Jaccard estimates how much of the two texts' shingles they share, of all
// they have: the share of equal minimum hashes.
func Jaccard(a, b []uint64) float64 {
	n := min(len(a), len(b))
	if n == 0 {
		return 0
	}
	same := 0
	for i := range n {
		if a[i] == b[i] {
			same++
		}
	}
	return float64(same) / float64(n)
}

// Containment estimates how much of each text's shingles the other holds,
// from their Jaccard and sizes: a novel in an omnibus is mostly contained
// in it, the omnibus little in the novel.
func Containment(jaccard float64, a, b int) (aInB, bInA float64) {
	if a == 0 || b == 0 {
		return 0, 0
	}
	shared := jaccard / (1 + jaccard) * float64(a+b)
	return min(shared/float64(a), 1), min(shared/float64(b), 1)
}

// encode and decode store the values as bytes.
func encode(values []uint64) []byte {
	out := make([]byte, 8*len(values))
	for i, v := range values {
		binary.LittleEndian.PutUint64(out[8*i:], v)
	}
	return out
}

func decode(b []byte) []uint64 {
	out := make([]uint64, len(b)/8)
	for i := range out {
		out[i] = binary.LittleEndian.Uint64(b[8*i:])
	}
	return out
}
