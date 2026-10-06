package embed

import (
	"context"
	"math"
	"os"
	"testing"
)

func TestFakeIsStableAndNormalised(t *testing.T) {
	f := &Fake{}
	vecs, err := f.Embed(context.Background(), []string{"a", "b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs[0]) != E5Small.Model.Dim {
		t.Errorf("dim = %d", len(vecs[0]))
	}
	if c := Cosine(vecs[0], vecs[2]); math.Abs(c-1) > 1e-6 {
		t.Errorf("the same text gave vectors of cosine %f", c)
	}
	if c := Cosine(vecs[0], vecs[1]); c > 0.3 {
		t.Errorf("two texts gave vectors of cosine %f", c)
	}
}

func fakeReference(t *testing.T, texts ...string) Reference {
	f := &Fake{}
	vecs, err := f.Embed(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	ref := Reference{Model: f.Model().Name, Revision: f.Model().Revision}
	for i, text := range texts {
		ref.Texts = append(ref.Texts, ReferenceText{Text: text, Vector: vecs[i]})
	}
	return ref
}

func TestCheckComparesWithTheReference(t *testing.T) {
	texts := []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
	ref := fakeReference(t, texts...)
	res, err := Check(context.Background(), &Fake{}, ref)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() || res.Texts != 9 || res.MinCosine < 0.999 {
		t.Errorf("an embedder against its own vectors: %+v", res)
	}

	other, _ := (&Fake{}).Embed(context.Background(), []string{"other"})
	ref.Texts[8].Vector = other[0]
	res, err = Check(context.Background(), &Fake{}, ref)
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed() || res.Worst != "nine" {
		t.Errorf("a wrong vector passed: %+v", res)
	}

	ref.Revision = "2"
	if _, err := Check(context.Background(), &Fake{}, ref); err == nil {
		t.Error("a reference of another revision was used")
	}
}

// TestOnnxMatchesTheReference runs the real model where it is at hand:
// GOTOME_TEST_EMBED_MODEL names a folder holding the model's files, as
// spikes/embed/fetch.sh leaves them, and GOTOME_TEST_ONNXRUNTIME the
// library. The gate has neither; the full gate checks the image instead
// (make embed-check).
func TestOnnxMatchesTheReference(t *testing.T) {
	dir, lib := os.Getenv("GOTOME_TEST_EMBED_MODEL"), os.Getenv("GOTOME_TEST_ONNXRUNTIME")
	if dir == "" || lib == "" {
		t.Skip("GOTOME_TEST_EMBED_MODEL and GOTOME_TEST_ONNXRUNTIME are not set")
	}
	spec := E5Small
	spec.Dir = "."
	o, err := OpenOnnx(context.Background(), spec, OnnxOptions{
		Runtime: lib, ModelDir: dir, Fetch: FetchOptions{Offline: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	ref, err := LoadReference("testdata/reference.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Check(context.Background(), o, ref)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d texts, lowest cosine %.4f", res.Texts, res.MinCosine)
	if !res.Passed() {
		t.Errorf("tokens differ for %q; lowest cosine %.4f for %.40q", res.TokenMismatches, res.MinCosine, res.Worst)
	}
}

func TestMeanWeighsAndNormalises(t *testing.T) {
	got := Mean([][]float32{{1, 0}, {0, 1}}, []float64{3, 1})
	want := []float32{0.9486833, 0.31622776}
	for k := range want {
		if math.Abs(float64(got[k]-want[k])) > 1e-6 {
			t.Fatalf("Mean = %v, want %v", got, want)
		}
	}
}
