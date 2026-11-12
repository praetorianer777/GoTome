package reading

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

var (
	text = []Landmark{{"Chapter 1", 0.05}, {"Chapter 2", 0.30}, {"Chapter 3", 0.60}, {"Afterword", 0.95}}
	// The recording opens with a credits track, and chapter 2 is longer.
	audio = []Landmark{{"Opening credits", 0}, {"Chapter 1", 0.02}, {"Chapter 2", 0.20}, {"Chapter 3", 0.70}}
)

func TestMapKeepsTheShareInsideAMatchedChapter(t *testing.T) {
	// Halfway through chapter 2 of the text is halfway through chapter 2 of
	// the recording, whatever their lengths.
	got, refined := Map(0.45, text, audio)
	if !refined || !near(got, 0.45) {
		t.Fatalf("mapped to %v (refined %v), want 0.45", got, refined)
	}
	got, _ = Map(0.30, text, audio)
	if !near(got, 0.20) {
		t.Errorf("the start of chapter 2 mapped to %v, want 0.20", got)
	}
	got, _ = Map(0.375, text, audio)
	if !near(got, 0.20+0.25*0.5) {
		t.Errorf("a quarter into chapter 2 mapped to %v", got)
	}
}

func TestMapFallsBackToTheShareOfTheWhole(t *testing.T) {
	cases := map[string][]Landmark{
		"no chapters":      nil,
		"one match":        {{"Chapter 2", 0.5}},
		"other titles":     {{"Part one", 0.2}, {"Part two", 0.7}},
		"a title twice":    {{"Chapter 1", 0.1}, {"Chapter 1", 0.4}, {"Chapter 2", 0.5}},
		"another sequence": {{"Chapter 2", 0.1}, {"Chapter 1", 0.5}},
	}
	one := []Landmark{{"Chapter 1", 0.1}, {"Chapter 2", 0.4}}
	for name, other := range cases {
		got, refined := Map(0.33, one, other)
		if refined || !near(got, 0.33) {
			t.Errorf("%s: mapped to %v (refined %v)", name, got, refined)
		}
	}
}

func TestMapComparesTitlesWithoutCaseOrPunctuation(t *testing.T) {
	a := []Landmark{{"Chapter 1: The Storm", 0.1}, {"Chapter 2 - Calm", 0.5}}
	b := []Landmark{{"chapter 1 the storm", 0.2}, {"Chapter 2: Calm", 0.8}}
	if got, refined := Map(0.5, a, b); !refined || !near(got, 0.8) {
		t.Errorf("mapped to %v (refined %v)", got, refined)
	}
}

func TestMapOfOutOfRangeSharesStaysInTheBook(t *testing.T) {
	for _, f := range []float64{-1, 0, 1, 2} {
		if got, _ := Map(f, text, audio); got < 0 || got > 1 {
			t.Errorf("%v mapped to %v", f, got)
		}
	}
}

func ms(v int64) *int64 { return &v }

func TestContinueOffersTheMediumBehind(t *testing.T) {
	listened := &Progress{Medium: MediumAudio, Fraction: 0.45, PositionMS: ms(45_000)}
	read := &Progress{Medium: MediumEbook, Fraction: 0.30}
	sides := Sides{Audio: listened, Ebook: read, AudioMS: 100_000, HasEbook: true, EbookChapters: text, AudioChapters: audio}

	got := Continue(sides)
	if got.Audio != nil {
		t.Errorf("the audiobook is ahead and is offered %v", got.Audio)
	}
	if got.Ebook == nil || !got.Ebook.Refined || got.Ebook.Chapter != "Chapter 2" || !near(got.Ebook.Fraction, 0.45) {
		t.Errorf("the text is offered %+v", got.Ebook)
	}

	// Reading on past the recording turns the offer round.
	sides.Ebook = &Progress{Medium: MediumEbook, Fraction: 0.65}
	got = Continue(sides)
	if got.Ebook != nil || got.Audio == nil || got.Audio.Chapter != "Chapter 3" || !near(got.Audio.Fraction, 0.7375) {
		t.Errorf("after reading ahead: ebook %v, audio %v", got.Ebook, got.Audio)
	}
}

func TestContinueIgnoresSmallGainsFinishedBooksAndMissingMedia(t *testing.T) {
	base := Sides{AudioMS: 100_000, HasEbook: true}
	at := func(medium string, f float64) *Progress {
		p := &Progress{Medium: medium, Fraction: f}
		if medium == MediumAudio {
			p.PositionMS = ms(int64(f * 100_000))
		}
		return p
	}

	s := base
	s.Audio, s.Ebook = at(MediumAudio, 0.205), at(MediumEbook, 0.2)
	if o := Continue(s); o.Ebook != nil || o.Audio != nil {
		t.Errorf("half a percent is no reason to switch: %+v %+v", o.Ebook, o.Audio)
	}
	s.Audio, s.Ebook = at(MediumAudio, 0.995), at(MediumEbook, 0.2)
	if o := Continue(s); o.Ebook != nil {
		t.Errorf("a finished audiobook is offered as %+v", o.Ebook)
	}
	s.Audio, s.Ebook = at(MediumAudio, 0.4), nil
	if o := Continue(s); o.Ebook == nil || o.Ebook.Refined || !near(o.Ebook.Fraction, 0.4) {
		t.Errorf("a book never read is offered %+v", o.Ebook)
	}
	s.HasEbook = false
	if o := Continue(s); o.Ebook != nil {
		t.Errorf("with no file to read: %+v", o.Ebook)
	}
	s = base
	s.AudioMS = 0
	s.Ebook = at(MediumEbook, 0.5)
	if o := Continue(s); o.Audio != nil {
		t.Errorf("with no audio to play: %+v", o.Audio)
	}
	s = base
	s.Ebook, s.Audio = nil, nil
	if o := Continue(s); o.Ebook != nil || o.Audio != nil {
		t.Errorf("with nothing read: %+v %+v", o.Ebook, o.Audio)
	}
}
