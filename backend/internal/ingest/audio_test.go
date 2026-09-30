package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
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

// audioFile is an audio file for writeAudio to make: tags, chapters and a
// cover, of a length in seconds.
type audioFile struct {
	seconds  float64
	tags     map[string]string
	chapters []Chapter
	cover    bool
}

// writeAudio makes the file with ffmpeg, in the format its name says.
func writeAudio(t testing.TB, path string, f audioFile) {
	t.Helper()
	dir := t.TempDir()
	var meta strings.Builder
	meta.WriteString(";FFMETADATA1\n")
	for k, v := range f.tags {
		fmt.Fprintf(&meta, "%s=%s\n", k, v)
	}
	for _, c := range f.chapters {
		fmt.Fprintf(&meta, "[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\ntitle=%s\n", c.StartMS, c.EndMS, c.Title)
	}
	metaPath := filepath.Join(dir, "meta.txt")
	if err := os.WriteFile(metaPath, []byte(meta.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%g", f.seconds), "-i", metaPath}
	maps := []string{"-map", "0:a", "-map_metadata", "1", "-map_chapters", "1"}
	if f.cover {
		img := image.NewRGBA(image.Rect(0, 0, 60, 90))
		for i := range img.Pix {
			img.Pix[i] = 0x90
		}
		img.Set(0, 0, color.Black)
		var buf bytes.Buffer
		png.Encode(&buf, img)
		coverPath := filepath.Join(dir, "cover.png")
		os.WriteFile(coverPath, buf.Bytes(), 0o644)
		args = append(args, "-i", coverPath)
		maps = append(maps, "-map", "2:v", "-c:v", "png", "-disposition:v", "attached_pic")
	}
	args = append(args, maps...)
	switch filepath.Ext(path) {
	case ".m4b":
		args = append(args, "-c:a", "aac", "-f", "ipod")
	case ".mp3":
		args = append(args, "-c:a", "libmp3lame", "-id3v2_version", "3")
	case ".ogg":
		args = append(args, "-c:a", "libvorbis")
	}
	args = append(args, path)
	if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestM4BWithChapters(t *testing.T) {
	needFFmpeg(t)
	path := filepath.Join(t.TempDir(), "dune.m4b")
	writeAudio(t, path, audioFile{
		seconds: 3,
		tags: map[string]string{
			"title": "Dune", "artist": "Frank Herbert", "composer": "Scott Brick; Orlagh Cassidy",
			"genre": "Science Fiction", "date": "2007-01-01", "comment": "The desert planet.",
		},
		chapters: []Chapter{{"Opening Credits", 0, 500}, {"Book One", 500, 2000}, {"Book Two", 2000, 3000}},
		cover:    true,
	})
	got, err := extractAudio(context.Background(), path)
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
	for _, ext := range []string{".mp3", ".flac", ".ogg", ".m4b"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "part"+ext)
			writeAudio(t, path, audioFile{seconds: 1.5, tags: map[string]string{
				"title": "Chapter Three", "album": "Emma", "artist": "Jane Austen", "track": "3/10", "disc": "2/2",
			}, cover: ext != ".ogg"})
			got, err := extractAudio(context.Background(), path)
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
			if (got.Cover != nil) != (ext != ".ogg") {
				t.Errorf("cover of %d bytes", len(got.Cover))
			}
		})
	}
}

func TestATrackTitleDoesNotNameTheBook(t *testing.T) {
	needFFmpeg(t)
	path := filepath.Join(t.TempDir(), "07.mp3")
	writeAudio(t, path, audioFile{seconds: 1, tags: map[string]string{"title": "Chapter Seven", "track": "7"}})
	got, err := extractAudio(context.Background(), path)
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
