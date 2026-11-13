package autodupe

import (
	"bytes"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestSampleKeyIsAStableHashOfTheID(t *testing.T) {
	id := uuid.MustParse("6f1c2a3e-0b45-4d7e-9a10-2c3d4e5f6a7b")
	if sampleKey(id) != sampleKey(id) {
		t.Error("the key changes between calls")
	}
	if sampleKey(id) == sampleKey(uuid.MustParse("6f1c2a3e-0b45-4d7e-9a10-2c3d4e5f6a7c")) {
		t.Error("two IDs have one key")
	}
	if sampleKey(uuid.Nil) == ([32]byte{}) {
		t.Error("the key is the ID padded, not a hash")
	}
}

func TestSamplingByTheKeyDoesNotFollowTheOrderOfIDs(t *testing.T) {
	var ids []uuid.UUID
	for i := range 64 {
		ids = append(ids, uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(i)}))
	}
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	bySample := slices.Clone(ids)
	slices.SortFunc(bySample, func(a, b uuid.UUID) int {
		ka, kb := sampleKey(a), sampleKey(b)
		return bytes.Compare(ka[:], kb[:])
	})
	if slices.Equal(sorted, bySample) {
		t.Error("the sample is the first IDs, not a spread")
	}
	again := slices.Clone(sorted)
	slices.SortFunc(again, func(a, b uuid.UUID) int {
		ka, kb := sampleKey(a), sampleKey(b)
		return bytes.Compare(ka[:], kb[:])
	})
	if !slices.Equal(bySample, again) {
		t.Error("the sample depends on the order the pairs arrive in")
	}
}
