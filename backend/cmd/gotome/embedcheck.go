package main

import (
	"context"
	"flag"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/praetorianer777/gotome/backend/internal/config"
	"github.com/praetorianer777/gotome/backend/internal/embed"
)

// embedCheck loads ONNX Runtime and prints its version. With -reference it
// also fetches the model where it is missing or not the pinned one, embeds
// the reference's texts and fails unless tokens and vectors match.
func embedCheck(args []string) error {
	flags := flag.NewFlagSet("embed-check", flag.ContinueOnError)
	refPath := flags.String("reference", "", "a reference file of texts with token IDs and vectors (backend/internal/embed/testdata/reference.json)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	version, err := embed.LoadRuntime(cfg.OnnxRuntime)
	if err != nil {
		return err
	}
	fmt.Printf("ONNX Runtime %s from %s\n", version, cfg.OnnxRuntime)
	if *refPath == "" {
		return nil
	}
	ref, err := embed.LoadReference(*refPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	o, err := embed.OpenOnnx(ctx, embed.E5Small, embed.OnnxOptions{
		Runtime:  cfg.OnnxRuntime,
		ModelDir: cfg.ModelDir,
		Fetch:    embed.FetchOptions{Offline: cfg.Offline},
	})
	if err != nil {
		return err
	}
	defer o.Close()
	res, err := embed.Check(ctx, o, ref)
	if err != nil {
		return err
	}
	m := o.Model()
	fmt.Printf("%s (%s weights): %d texts, tokens differ for %d, lowest cosine %.4f\n",
		m.Name, m.Weights, res.Texts, len(res.TokenMismatches), res.MinCosine)
	if !res.Passed() {
		return fmt.Errorf("the model does not match the reference: tokens differ for %q, lowest cosine %.4f (want %.2f) for %.60q",
			res.TokenMismatches, res.MinCosine, embed.MinCosine, res.Worst)
	}
	return nil
}
