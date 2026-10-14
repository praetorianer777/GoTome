package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/config"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// embedCheck loads ONNX Runtime and prints its version. With -reference it
// also fetches the model where it is missing or not the pinned one, embeds
// the reference's texts and fails unless tokens and vectors match. With
// -against-stored it runs the model -model names on the CPU and compares
// its metadata vectors of that many books with those stored for it, which
// another runtime may have made (#228).
func embedCheck(args []string) error {
	flags := flag.NewFlagSet("embed-check", flag.ContinueOnError)
	refPath := flags.String("reference", "", "a reference file of texts with token IDs and vectors (backend/internal/embed/testdata/reference.json)")
	model := flags.String("model", embed.DefaultSpec, "the model to run, a name embedding.model takes")
	stored := flags.Int("against-stored", 0, "compare the metadata vectors of this many books with those stored for the model")
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
	if *refPath == "" && *stored == 0 {
		return nil
	}
	spec, ok := embed.Specs[*model]
	if !ok {
		return fmt.Errorf("there is no model %q", *model)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if *stored > 0 {
		return checkStored(ctx, cfg, spec, *stored)
	}
	ref, err := embed.LoadReference(*refPath)
	if err != nil {
		return err
	}
	o, err := embed.OpenOnnx(ctx, spec, embed.OnnxOptions{
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

// checkStored runs the model on the CPU and compares its vectors with the
// stored ones: whether its vectors and the server's are one model's.
func checkStored(ctx context.Context, cfg config.Config, spec embed.Spec, books int) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL, slog.New(slog.DiscardHandler))
	if err != nil {
		return err
	}
	defer pool.Close()
	o, err := embed.OpenOnnx(ctx, spec, embed.OnnxOptions{
		Runtime:  cfg.OnnxRuntime,
		ModelDir: cfg.ModelDir,
		Threads:  cfg.EmbedThreads,
		Fetch:    embed.FetchOptions{Offline: cfg.Offline},
	})
	if err != nil {
		return err
	}
	defer o.Close()
	start := time.Now()
	c, err := similar.CompareStored(ctx, sqlc.New(pool), spec, o, books)
	if err != nil {
		return err
	}
	took := time.Since(start)
	fmt.Printf("%s on the CPU against the stored vectors: %d books in %s, cosine mean %.4f, lowest %.4f (book %s)\n",
		spec.Model.Name, c.Books, took.Round(time.Second), c.Mean, c.MinCosine, c.Worst)
	if !c.Passed() {
		return fmt.Errorf("the vectors are not the stored ones: lowest cosine %.4f, want %.2f; with no stored vectors there is nothing to compare",
			c.MinCosine, similar.MinStoredCosine)
	}
	return nil
}
