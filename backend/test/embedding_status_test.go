//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

func TestEmbeddingProgressCountsTheBooksTheCallerSees(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	admin := a.admin
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	// A book of a private library the editor is not in, with its vector.
	vault := a.library(admin, "Vault", "private")
	secret := a.bookIn(vault, "Secretum")
	a.putVector(secret, similar.KindMetadata, direction(map[int]float32{0: 1}))

	status := func(c *http.Client) map[string]any {
		t.Helper()
		code, out, _ := a.call(c, http.MethodGet, "/embedding/status", nil)
		if code != 200 {
			t.Fatalf("status: %d %v", code, out)
		}
		return out
	}
	// The book with text, the bare one and the vault's; the wish is none.
	want := map[string]any{"model": embed.E5Small.Model.Name, "enabled": true, "books": 3.0, "withText": 1.0, "metadata": 1.0, "content": 0.0, "metadataLastHour": 1.0}
	if got := status(admin); !sameMap(got, want) {
		t.Errorf("before: %v, want %v", got, want)
	}
	if got := status(editor); got["books"] != 2.0 || got["metadata"] != 0.0 {
		t.Errorf("the editor, who does not see the vault: %v", got)
	}
	if code, _, _ := a.call(reader, http.MethodGet, "/embedding/status", nil); code != 403 {
		t.Errorf("a reader: %d", code)
	}

	a.embedAll()
	if got := status(editor); got["metadata"] != 2.0 || got["content"] != 1.0 {
		t.Errorf("after a pass: %v", got)
	}
	a.set(settings.EmbeddingEnabled, "off")
	if got := status(editor); got["enabled"] != false {
		t.Errorf("paused: %v", got)
	}
}

func (a *app) putVectorOf(book uuid.UUID, kind string, spec embed.Spec) {
	a.t.Helper()
	v := make([]float32, spec.Model.Dim)
	v[0] = 1
	if _, err := a.pool.Exec(context.Background(), `
		INSERT INTO book_vectors (book_id, kind, model, model_version, source_hash, embedding)
		VALUES ($1, $2, $3, $4, sha256('test'), $5::text::vector)
		ON CONFLICT (book_id, kind, model) DO UPDATE SET model_version = EXCLUDED.model_version, embedding = EXCLUDED.embedding`,
		book, kind, spec.Model.Name, similar.Version(spec.Model), vectorText(v)); err != nil {
		a.t.Fatal(err)
	}
}

func TestEmbeddingModelsCountTheBooksTheCallerSees(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	admin := a.admin
	editor, editorID := a.signedIn("editor", "editor")
	a.server.Similar.Probe = func(_ context.Context, spec embed.Spec) similar.Readiness {
		if spec.Server != "" {
			return similar.Readiness{State: similar.NoServer, OnServer: true}
		}
		return similar.Readiness{State: similar.ReadyHere}
	}
	vault := a.library(admin, "Vault", "private")
	secret := a.bookIn(vault, "Secretum")
	a.putVectorOf(secret, similar.KindMetadata, embed.BGEM3)
	a.putVectorOf(secret, similar.KindContent, embed.BGEM3)
	a.putVectorOf(a.bare, similar.KindMetadata, embed.BGEM3)
	a.putVectorOf(a.text, similar.KindMetadata, embed.E5SmallFP32)
	a.putVectorOf(a.bare, similar.KindMetadata, embed.E5Small)

	code, out, _ := a.call(admin, http.MethodGet, "/embedding/models", nil)
	if code != 200 {
		t.Fatalf("models: %d %v", code, out)
	}
	if out["books"] != 3.0 || out["withText"] != 1.0 {
		t.Errorf("books %v, with text %v", out["books"], out["withText"])
	}
	models, _ := out["models"].([]any)
	var names []string
	byName := map[string]map[string]any{}
	for _, m := range models {
		m := m.(map[string]any)
		names = append(names, m["name"].(string))
		byName[m["name"].(string)] = m
	}
	if want := []string{"multilingual-e5-small", "multilingual-e5-small-fp32", "bge-m3"}; !slices.Equal(names, want) {
		t.Fatalf("models %v, want the smallest first: %v", names, want)
	}
	small, fp32, bge := byName["multilingual-e5-small"], byName["multilingual-e5-small-fp32"], byName["bge-m3"]
	if small["chosen"] != true || fp32["chosen"] != false || bge["chosen"] != false {
		t.Errorf("chosen: %v %v %v", small["chosen"], fp32["chosen"], bge["chosen"])
	}
	if small["metadata"] != 1.0 || fp32["metadata"] != 1.0 || bge["metadata"] != 2.0 || bge["content"] != 1.0 {
		t.Errorf("counts for the admin: e5 %v, fp32 %v, bge %v/%v", small["metadata"], fp32["metadata"], bge["metadata"], bge["content"])
	}
	if bge["diskBytes"] != float64(embed.BGEM3.DiskBytes()) || bge["memoryBytes"] != float64(embed.BGEM3.Memory) || bge["dimensions"] != 1024.0 {
		t.Errorf("bge-m3: %v", bge)
	}
	if r := small["readiness"].(map[string]any); r["state"] != "ready" || r["onServer"] != false {
		t.Errorf("e5-small readiness %v", r)
	}

	if code, _, _ := a.call(editor, http.MethodGet, "/embedding/models", nil); code != 403 {
		t.Errorf("an editor: %d", code)
	}
	// No one but an administrator may ask, and they see every library; the
	// counts are held to a scope that does not see the vault here.
	list, err := a.server.Similar.Models(context.Background(), library.Scope{Viewer: uuid.MustParse(editorID)})
	if err != nil {
		t.Fatal(err)
	}
	if list.Books != 2 {
		t.Errorf("the editor's scope counts %d books", list.Books)
	}
	for _, m := range list.Models {
		if m.Name == "bge-m3" && (m.Metadata != 1 || m.Content != 0) {
			t.Errorf("bge-m3 in the editor's scope: %d metadata, %d content", m.Metadata, m.Content)
		}
	}

	a.set(settings.EmbeddingModel, "bge-m3")
	if _, out, _ := a.call(admin, http.MethodGet, "/embedding/models", nil); out["models"].([]any)[2].(map[string]any)["chosen"] != true {
		t.Errorf("bge-m3 is not the chosen one after choosing it: %v", out)
	}
}

func TestAModelRunsWhereItsOpenerWouldOpenIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local := similar.OnnxProbe(embed.OnnxOptions{
		Runtime: "/nonexistent/libonnxruntime.so", ModelDir: t.TempDir(), Fetch: embed.FetchOptions{Offline: true},
	})
	if got := local(ctx, embed.E5Small); got.State != similar.NoRuntime || got.OnServer {
		t.Errorf("without a runtime: %+v", got)
	}
	// bge-m3 has its files, so ONNX Runtime is what it waits for, not a server.
	_, err := similar.OnnxOpener(embed.OnnxOptions{Runtime: "/nonexistent/libonnxruntime.so"})(ctx, embed.BGEM3)
	if !errors.Is(err, similar.ErrUnavailable) || strings.Contains(err.Error(), "embedding server") {
		t.Errorf("bge-m3 on the CPU without a runtime: %v", err)
	}
	server := ""
	probe := similar.ServerProbe(func(context.Context, embed.Spec) similar.Readiness {
		return similar.Readiness{State: similar.ReadyAfterDownload}
	}, func(context.Context) (string, error) { return server, nil })
	if got := probe(ctx, embed.BGEM3); got.State != similar.ReadyAfterDownload || got.OnServer {
		t.Errorf("bge-m3 without a server runs on the CPU: %+v", got)
	}
	if got := probe(ctx, embed.Spec{Model: embed.Model{Name: "x"}, Server: "x"}); got.State != similar.NoServer || !got.OnServer {
		t.Errorf("a model only a server runs, without one: %+v", got)
	}
	server = fakeOllama(t).URL
	if got := probe(ctx, embed.BGEM3); got.State != similar.ReadyHere || !got.OnServer {
		t.Errorf("bge-m3 on a server that answers: %+v", got)
	}
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	server = gone.URL
	if got := probe(ctx, embed.BGEM3); got.State != similar.ServerDown || !got.OnServer {
		t.Errorf("bge-m3 on a server that is gone: %+v", got)
	}
}
