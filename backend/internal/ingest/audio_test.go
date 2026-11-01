package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func needFFmpeg(t *testing.T) {
	t.Helper()
	for _, program := range []string{ffprobe, ffmpeg} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("%s is not installed here; the toolchain image has it", program)
		}
	}
}

const audioFiles = "testdata/audio"

func TestAudioReadsAsTheReferenceSays(t *testing.T) {
	needFFmpeg(t)
	dirs := []string{audioFiles, "../../../e2e/fixtures/audio"}
	if os.Getenv("GOTOME_WRITE_AUDIO_REFERENCE") != "" {
		facts, err := readAudioFacts(context.Background(), dirs...)
		if err != nil {
			t.Fatal(err)
		}
		out, err := json.MarshalIndent(facts, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(audioFiles, audioReference), append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := CheckAudio(context.Background(), dirs...); err != nil {
		t.Error(err)
	}
}

func TestCheckAudioSaysWhatDiffers(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	raw, err := os.ReadFile(filepath.Join(audioFiles, "emma.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "emma.mp3"), raw, 0o644)
	os.WriteFile(filepath.Join(dir, audioReference), []byte(`{"emma.mp3": {"DurationMS": 1}, "gone.mp3": {}}`), 0o644)
	err = CheckAudio(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "emma.mp3: got") || !strings.Contains(err.Error(), "gone.mp3: not found") {
		t.Errorf("err = %v", err)
	}
}

func TestM4BWithChapters(t *testing.T) {
	needFFmpeg(t)
	got, err := extractAudio(context.Background(), filepath.Join(audioFiles, "dune.m4b"))
	if err != nil {
		t.Fatal(err)
	}
	m := got.Metadata
	if m.Title != "Dune" || m.Published != "2007" || m.Description != "The desert planet." || strings.Join(m.Tags, "|") != "Science Fiction" {
		t.Errorf("metadata = %+v", m)
	}
	var credits []string
	for _, c := range m.Contributors {
		credits = append(credits, c.Role+": "+c.Name)
	}
	if want := "author: Frank Herbert|narrator: Scott Brick|narrator: Orlagh Cassidy"; strings.Join(credits, "|") != want {
		t.Errorf("credits = %v, want %s", credits, want)
	}
	if got.DurationMS == nil || *got.DurationMS < 2900 || *got.DurationMS > 3100 {
		t.Errorf("duration = %v ms, want about 3000", got.DurationMS)
	}
	want := []Chapter{{"Opening Credits", 0, 500}, {"Book One", 500, 2000}, {"Book Two", 2000, 3000}}
	if fmt.Sprint(got.Chapters) != fmt.Sprint(want) {
		t.Errorf("chapters = %v, want %v", got.Chapters, want)
	}
	if cover, err := png.Decode(bytes.NewReader(got.Cover)); err != nil || cover.Bounds().Dx() != 60 {
		t.Errorf("cover: %v", err)
	}
	if got.HasText {
		t.Error("an audiobook has text")
	}
}

func TestEveryAudioFormatIsRead(t *testing.T) {
	needFFmpeg(t)
	for _, ext := range []string{".mp3", ".flac", ".ogg", ".opus", ".m4b"} {
		t.Run(ext, func(t *testing.T) {
			got, err := extractAudio(context.Background(), filepath.Join(audioFiles, "emma"+ext))
			if err != nil {
				t.Fatal(err)
			}
			if got.Metadata.Title != "Emma" || len(got.Metadata.Contributors) != 1 || got.Metadata.Contributors[0].Name != "Jane Austen" {
				t.Errorf("metadata = %+v; want the album as the title", got.Metadata)
			}
			if got.Track == nil || *got.Track != 3 || got.Disc == nil || *got.Disc != 2 {
				t.Errorf("track %v, disc %v; want 3 and 2", got.Track, got.Disc)
			}
			if got.DurationMS == nil || *got.DurationMS < 1400 || *got.DurationMS > 1700 {
				t.Errorf("duration = %v ms, want about 1500", got.DurationMS)
			}
			if hasCover := ext != ".ogg" && ext != ".opus"; (got.Cover != nil) != hasCover {
				t.Errorf("cover of %d bytes", len(got.Cover))
			}
		})
	}
}

func TestATrackTitleDoesNotNameTheBook(t *testing.T) {
	needFFmpeg(t)
	got, err := extractAudio(context.Background(), filepath.Join(audioFiles, "chapter-seven.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata.Title != "" {
		t.Errorf("the book is called %q after one of its tracks", got.Metadata.Title)
	}
}

func TestAudioThatIsNone(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	for name, content := range map[string]string{
		"page.mp3": "<html>a download that went wrong</html>",
		// A playlist that points elsewhere: FFmpeg must not follow it.
		"list.mp3": "#EXTM3U\n#EXTINF:10,\nhttp://example.org/x.mp3\n#EXTINF:10,\n/etc/hostname\n",
	} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, []byte(content), 0o644)
		if _, err := extractAudio(context.Background(), path); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: err = %v, want ErrUnreadable", name, err)
		}
	}
}
