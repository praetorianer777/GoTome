package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

const audioReference = "reference.json"

// audioFacts is what GOtome takes from an audio file through FFmpeg.
type audioFacts struct {
	Metadata    catalog.FileMetadata
	DurationMS  *int64
	Track, Disc *int32
	Chapters    []Chapter
	CoverBytes  int
	CoverSHA256 string
}

// readAudioFacts reads every audio file of the folders, by file name.
func readAudioFacts(ctx context.Context, dirs ...string) (map[string]audioFacts, error) {
	facts := map[string]audioFacts{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if kind, ok := catalog.KindOf(strings.TrimPrefix(filepath.Ext(name), ".")); !ok || kind != catalog.KindAudio {
				continue
			}
			got, err := extractAudio(ctx, filepath.Join(dir, name))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			f := audioFacts{Metadata: got.Metadata, DurationMS: got.DurationMS, Track: got.Track, Disc: got.Disc, Chapters: got.Chapters}
			if got.Cover != nil {
				sum := sha256.Sum256(got.Cover)
				f.CoverBytes, f.CoverSHA256 = len(got.Cover), hex.EncodeToString(sum[:])
			}
			facts[name] = f
		}
	}
	return facts, nil
}

// CheckAudio reads the audio files of the folders with the FFmpeg installed
// here and compares what comes of them with the first folder's reference
// file: GOtome's own build has no decoders, so a demuxer or parser left out of
// it shows here and in no error. A full FFmpeg of Debian's, 5.1, wrote the
// file. Only the MP3s' durations have changed since: 5.1 counted the encoder's
// padding, up to 71 ms here, which 9.0 leaves out.
func CheckAudio(ctx context.Context, dirs ...string) error {
	if len(dirs) == 0 {
		return errors.New("no folder of audio files given")
	}
	raw, err := os.ReadFile(filepath.Join(dirs[0], audioReference))
	if err != nil {
		return err
	}
	var want map[string]audioFacts
	if err := json.Unmarshal(raw, &want); err != nil {
		return fmt.Errorf("%s: %w", audioReference, err)
	}
	got, err := readAudioFacts(ctx, dirs...)
	if err != nil {
		return err
	}
	var differ []error
	for name, facts := range want {
		found, ok := got[name]
		if !ok {
			differ = append(differ, fmt.Errorf("%s: not found", name))
		} else if !reflect.DeepEqual(found, facts) {
			gotJSON, _ := json.Marshal(found)
			wantJSON, _ := json.Marshal(facts)
			differ = append(differ, fmt.Errorf("%s: got %s, want %s", name, gotJSON, wantJSON))
		}
	}
	return errors.Join(differ...)
}
