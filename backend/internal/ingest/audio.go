package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/procexec"
)

// The programs of FFmpeg that read audio files. Variables, so that a test can
// put a program in their place that misbehaves on purpose.
var (
	ffprobe = "ffprobe"
	ffmpeg  = "ffmpeg"
)

// audioLimits bound one run of ffprobe or ffmpeg on one file. Probing reads
// the container's index, not the sound, so even a book of forty hours is
// probed in seconds.
var audioLimits = procexec.Limits{
	Timeout:   2 * time.Minute,
	MaxOutput: 32 << 20,
	MaxMemory: 1 << 30,
	MaxCPU:    2 * time.Minute,
}

// untrusted keeps FFmpeg to reading the one local file as one of the audio
// formats a library holds. A file named .mp3 may be a playlist, and a playlist
// may point at any address, on the network or on the server.
var untrusted = []string{"-protocol_whitelist", "file", "-format_whitelist", "mov,mp3,flac,ogg"}

// wholeBookFormats hold a whole audiobook in one file as a rule, so their
// title tag names the book. In the others it names a track.
var wholeBookFormats = map[string]bool{".m4b": true, ".m4a": true}

// Chapter is a chapter an audio file marks.
type Chapter struct {
	Title          string
	StartMS, EndMS int64
}

type probe struct {
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		Tags        map[string]string `json:"tags"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Chapters []struct {
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

func extractAudio(ctx context.Context, path string) (Extracted, error) {
	args := append([]string{"-v", "error"}, untrusted...)
	out, err := runFFmpeg(ctx, ffprobe, append(args, "-print_format", "json",
		"-show_format", "-show_streams", "-show_chapters", path)...)
	if err != nil {
		return Extracted{}, err
	}
	var p probe
	if err := json.Unmarshal(out, &p); err != nil {
		return Extracted{}, fmt.Errorf("%w: ffprobe's answer: %v", ErrUnreadable, err)
	}
	audio := false
	for _, s := range p.Streams {
		audio = audio || s.CodecType == "audio"
	}
	if !audio {
		return Extracted{}, fmt.Errorf("%w: no sound in the file", ErrUnreadable)
	}

	tags := p.tags()
	got := Extracted{Metadata: audioMetadata(tags, wholeBookFormats[strings.ToLower(extOf(path))])}
	if ms, ok := millis(p.Format.Duration); ok {
		got.DurationMS = &ms
	}
	got.Track, got.Disc = position(tags["track"], tags["tracknumber"]), position(tags["disc"], tags["discnumber"])
	for i, c := range p.Chapters {
		start, okStart := millis(c.StartTime)
		end, okEnd := millis(c.EndTime)
		if !okStart || !okEnd || end < start {
			continue
		}
		title := strings.TrimSpace(c.Tags["title"])
		if title == "" {
			title = "Chapter " + strconv.Itoa(i+1)
		}
		got.Chapters = append(got.Chapters, Chapter{Title: title, StartMS: start, EndMS: end})
	}

	for i, s := range p.Streams {
		if s.CodecType != "video" || s.Disposition.AttachedPic != 1 {
			continue
		}
		args := append([]string{"-v", "error"}, untrusted...)
		cover, err := runFFmpeg(ctx, ffmpeg, append(args, "-i", path,
			"-map", "0:"+strconv.Itoa(i), "-c", "copy", "-frames:v", "1", "-f", "image2pipe", "-")...)
		if err != nil {
			// The sound is what matters; a picture ffmpeg cannot copy out is
			// a book without a cover.
			if errors.Is(err, ErrUnreadable) {
				break
			}
			return Extracted{}, err
		}
		got.Cover = cover
		break
	}
	return got, nil
}

// tags merges the file's tags with those of its first sound, where Ogg keeps
// them, under names in lower case: Vorbis comments are written in upper case.
func (p *probe) tags() map[string]string {
	tags := map[string]string{}
	add := func(from map[string]string) {
		for k, v := range from {
			k = strings.ToLower(k)
			if v = strings.TrimSpace(v); v != "" && tags[k] == "" {
				tags[k] = v
			}
		}
	}
	add(p.Format.Tags)
	for _, s := range p.Streams {
		if s.CodecType == "audio" {
			add(s.Tags)
			break
		}
	}
	return tags
}

func audioMetadata(tags map[string]string, wholeBook bool) catalog.FileMetadata {
	m := catalog.FileMetadata{
		Title:       tags["album"],
		Description: firstOf(tags["description"], tags["comment"], tags["synopsis"]),
		Publisher:   tags["publisher"],
		Language:    tags["language"],
		Published:   firstOf(tags["date"], tags["year"]),
	}
	if m.Title == "" && wholeBook {
		m.Title = tags["title"]
	}
	if len(m.Published) > 4 {
		// Only the year is certain: tag dates are as often the day the file
		// was made as the day the book came out.
		m.Published = m.Published[:4]
	}
	for _, name := range names(firstOf(tags["album_artist"], tags["artist"])) {
		m.Contributors = append(m.Contributors, catalog.NewContributor{Name: name, Role: catalog.RoleAuthor})
	}
	// Audiobook tools write the narrator into the composer tag, which a
	// book has no other use for.
	for _, name := range names(firstOf(tags["narrator"], tags["composer"])) {
		m.Contributors = append(m.Contributors, catalog.NewContributor{Name: name, Role: catalog.RoleNarrator})
	}
	for _, genre := range names(tags["genre"]) {
		m.Tags = append(m.Tags, genre)
	}
	if id, ok := catalog.NormalizeIdentifier("asin", firstOf(tags["asin"], tags["audible_asin"])); ok {
		m.Identifiers = append(m.Identifiers, id)
	}
	return m
}

// names splits a tag that lists several names.
func names(s string) []string {
	var out []string
	for name := range strings.FieldsFuncSeq(s, func(r rune) bool { return r == ';' || r == '/' }) {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// position reads a track or disc tag, "3" or "3/12", and nil when there is
// none.
func position(values ...string) *int32 {
	for _, v := range values {
		n, _, _ := strings.Cut(v, "/")
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil && i >= 0 && i <= math.MaxInt32 {
			p := int32(i)
			return &p
		}
	}
	return nil
}

// millis reads seconds as ffprobe writes them.
func millis(s string) (int64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) || f > math.MaxInt64/1000 {
		return 0, false
	}
	return int64(math.Round(f * 1000)), true
}

func extOf(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 && !strings.ContainsRune(path[i:], '/') {
		return path[i:]
	}
	return ""
}

// runFFmpeg runs ffprobe or ffmpeg. What goes wrong with them is the file's
// doing, as with poppler.
func runFFmpeg(ctx context.Context, program string, args ...string) ([]byte, error) {
	out, err := procexec.Run(ctx, audioLimits, program, args...)
	var exit *procexec.ExitError
	switch {
	case err == nil:
		return out, nil
	case errors.As(err, &exit), errors.Is(err, procexec.ErrTimeout), errors.Is(err, procexec.ErrOutputTooLarge):
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	return nil, err
}
