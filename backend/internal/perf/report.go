package perf

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/resources"
)

// Context is what can be said about the installation without naming
// anything in it: counts, the machine and the settings.
type Context struct {
	Version string
	// What the person asked as sees.
	Books, Files, Authors, Series, Vectors int64
	// Chunks is the planner's estimate.
	Chunks int64
	// TextFiles is how many books' texts the search words were taken
	// from; 0 for the titles.
	TextFiles int

	CPUs         int
	MachineBytes int64
	Limited      bool
	Postgres     string
	Settings     resources.Postgres

	EmbeddingOn    bool
	EmbeddingModel string
	// EmbeddingHere says the model runs on this machine, not on a server.
	EmbeddingHere bool

	Before, After resources.Usage
}

// Print writes the report as Markdown. Nothing in it comes from the
// library's text or names: a test holds it to that.
func Print(w io.Writer, c Context, results []Result) {
	fmt.Fprintf(w, "## GOtome performance check\n\n")
	fmt.Fprintf(w, "| | |\n|---|---|\n")
	row := func(k, v string) { fmt.Fprintf(w, "| %s | %s |\n", k, v) }
	row("GOtome", c.Version)
	row("Books", fmt.Sprintf("%d, with %d files", c.Books, c.Files))
	row("Authors and series", fmt.Sprintf("%d and %d", c.Authors, c.Series))
	row("Text", fmt.Sprintf("about %d chunks", c.Chunks))
	row("Books with vectors", fmt.Sprintf("%d", c.Vectors))
	machine := fmt.Sprintf("%d CPUs, %s memory", c.CPUs, size(c.MachineBytes))
	if c.Limited {
		machine += " (the container's limit)"
	}
	row("Machine", machine)
	row("PostgreSQL", c.Postgres)
	s := c.Settings
	row("Its memory settings", fmt.Sprintf("shared_buffers %s, work_mem %s, maintenance_work_mem %s, effective_cache_size %s, %d connections",
		size(s.SharedBuffersBytes), size(s.WorkMemBytes), size(s.MaintenanceWorkMemBytes), size(s.EffectiveCacheSizeBytes), s.MaxConnections))
	row("Embedding", embedding(c))
	row("Search words from", searchWords(c))
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "### Memory in use\n\n| | Before the views | After |\n|---|---:|---:|\n")
	usage := func(k string, before, after int64) {
		fmt.Fprintf(w, "| %s | %s | %s |\n", k, size(before), size(after))
	}
	usage("App process (`gotome serve`)", c.Before.AppProcessBytes, c.After.AppProcessBytes)
	usage("App container", c.Before.AppContainerBytes, c.After.AppContainerBytes)
	usage("Database container", c.Before.PostgresContainerBytes, c.After.PostgresContainerBytes)
	fmt.Fprintf(w, "\nMemory the kernel could give back (file cache not in use) is left out, and the app container's figure includes this command. \"not seen\" is a figure this command cannot read.\n\n")

	fmt.Fprintf(w, "### Views\n\n| View | Time | Rows | Reads every book |\n|---|---:|---:|---|\n")
	for _, r := range results {
		took, rows := r.Not, "–"
		if r.Not == NotSkipped {
			took = duration(r.Best)
			if r.Rows >= 0 {
				rows = fmt.Sprintf("%d", r.Rows)
			}
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s |\n", r.Name, took, rows, r.Whole)
	}
	fmt.Fprintf(w, "\nTime is the best of three requests after one to warm up. Rows are the entries of the longest list in the answer.\n")
}

func embedding(c Context) string {
	switch {
	case !c.EmbeddingOn:
		return "off"
	case c.EmbeddingHere:
		return "on, " + c.EmbeddingModel + ", on this machine"
	default:
		return "on, " + c.EmbeddingModel + ", on a server"
	}
}

func searchWords(c Context) string {
	if c.TextFiles == 0 {
		return "the titles"
	}
	return fmt.Sprintf("the text of %d books", c.TextFiles)
}

func duration(d time.Duration) string {
	if d >= 10*time.Second {
		return fmt.Sprintf("%.1f s", d.Seconds())
	}
	return fmt.Sprintf("%d ms", d.Milliseconds())
}

func size(n int64) string {
	switch {
	case n <= 0:
		return "not seen"
	case n >= 1<<30:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/(1<<30)), ".0") + " GiB"
	default:
		return fmt.Sprintf("%d MiB", n>>20)
	}
}
