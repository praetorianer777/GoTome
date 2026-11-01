package main

import (
	"context"
	"fmt"

	"github.com/praetorianer777/gotome/backend/internal/ingest"
)

func audioCheck(dirs []string) error {
	if err := ingest.CheckAudio(context.Background(), dirs...); err != nil {
		return err
	}
	fmt.Println("FFmpeg reads the audio files as the reference says")
	return nil
}
