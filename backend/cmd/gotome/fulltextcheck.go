package main

import (
	"fmt"

	"github.com/praetorianer777/gotome/backend/internal/config"
	"github.com/praetorianer777/gotome/backend/internal/fulltext"
)

// fulltextCheck loads the full-text engine's library and prints its version.
func fulltextCheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	version, err := fulltext.Load(cfg.Fulltext)
	if err != nil {
		return err
	}
	fmt.Printf("%s from %s\n", version, cfg.Fulltext)
	return nil
}
