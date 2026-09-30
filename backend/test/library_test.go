//go:build integration

package test

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// names lists the libraries a browser is shown, sorted.
func (a *app) libraryNames(c *http.Client) []string {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodGet, "/libraries", nil)
	if status != 200 {
		a.t.Fatalf("list libraries: %d %v", status, body)
	}
	var names []string
	for _, entry := range body["libraries"].([]any) {
		names = append(names, entry.(map[string]any)["name"].(string))
	}
	sort.Strings(names)
	return names
}

// library creates a managed library and returns its ID.
func (a *app) library(admin *http.Client, name, visibility string) string {
	a.t.Helper()
	status, body, _ := a.call(admin, http.MethodPost, "/libraries", map[string]any{
		"name": name, "mode": "managed", "visibility": visibility,
	})
	if status != 201 {
		a.t.Fatalf("create library %s: %d %v", name, status, body)
	}
	return body["id"].(string)
}

func fieldError(body map[string]any, field string) string {
	e, _ := body["error"].(map[string]any)
	fields, _ := e["fields"].(map[string]any)
	msg, _ := fields[field].(string)
	return msg
}

// The matrix the issue asks for: every kind of person against every kind of
// library, in the list and by direct lookup.
func TestLibraryVisibility(t *testing.T) {
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	owner, ownerID := a.signedIn("second-admin", "admin")
	member, memberID := a.signedIn("member", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	editor, _ := a.signedIn("editor", "editor")

	shared := a.library(admin, "Shared", "shared")
	private := a.library(owner, "Private", "private")
	withMember := a.library(admin, "With a member", "private")
	if status, _, _ := a.call(admin, http.MethodPut, "/libraries/"+withMember+"/members/"+memberID, nil); status != 204 {
		t.Fatalf("add member: %d", status)
	}

	people := []struct {
		name   string
		client *http.Client
		sees   map[string]bool
	}{
		// Administrators run the storage every library lives on.
		{"the admin who is not its owner", admin, map[string]bool{shared: true, private: true, withMember: true}},
		{"the owner", owner, map[string]bool{shared: true, private: true, withMember: true}},
		{"a member", member, map[string]bool{shared: true, withMember: true}},
		{"a reader who is no member", stranger, map[string]bool{shared: true}},
		{"an editor who is no member", editor, map[string]bool{shared: true}},
	}
	libraries := map[string]string{shared: "Shared", private: "Private", withMember: "With a member"}

	for _, p := range people {
		var want []string
		for id, name := range libraries {
			status, body, _ := a.call(p.client, http.MethodGet, "/libraries/"+id, nil)
			switch {
			case p.sees[id] && status != 200:
				t.Errorf("%s looking up %q: %d, want 200", p.name, name, status)
			case !p.sees[id] && (status != 404 || errorCode(body) != "not_found"):
				t.Errorf("%s looking up %q: %d %v, want 404", p.name, name, status, body)
			}
			if p.sees[id] {
				want = append(want, name)
			}
		}
		sort.Strings(want)
		if got := a.libraryNames(p.client); !slices.Equal(got, want) {
			t.Errorf("%s is listed %v, want %v", p.name, got, want)
		}
	}

	// A library that does not exist answers exactly like one that is hidden.
	_, hidden, _ := a.call(stranger, http.MethodGet, "/libraries/"+private, nil)
	_, missing, _ := a.call(stranger, http.MethodGet, "/libraries/019a0000-0000-7000-8000-000000000000", nil)
	delete(hidden["error"].(map[string]any), "requestId")
	delete(missing["error"].(map[string]any), "requestId")
	if !mapsEqual(hidden, missing) {
		t.Errorf("a hidden library answers %v, a missing one %v", hidden, missing)
	}
	if status, _, _ := a.call(stranger, http.MethodGet, "/libraries/not-a-uuid", nil); status != 404 {
		t.Errorf("an ID that is no UUID: %d, want 404", status)
	}

	// Membership and visibility take effect at once.
	a.call(admin, http.MethodDelete, "/libraries/"+withMember+"/members/"+memberID, nil)
	if got := a.libraryNames(member); !slices.Equal(got, []string{"Shared"}) {
		t.Errorf("after being removed, the former member is listed %v", got)
	}
	a.call(owner, http.MethodPatch, "/libraries/"+private, map[string]any{"visibility": "shared"})
	if got := a.libraryNames(stranger); !slices.Equal(got, []string{"Private", "Shared"}) {
		t.Errorf("after the library was shared, a reader is listed %v", got)
	}

	// The owner of a private library sees it without being an administrator.
	if _, err := a.pool.Exec(t.Context(), "UPDATE libraries SET visibility = 'private' WHERE id = $1", private); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(t.Context(), "UPDATE users SET role = 'reader' WHERE id = $1", ownerID); err != nil {
		t.Fatal(err)
	}
	if got := a.libraryNames(owner); !slices.Equal(got, []string{"Private", "Shared"}) {
		t.Errorf("the owner, now a reader, is listed %v", got)
	}

	if status, _, _ := a.call(a.browser(), http.MethodGet, "/libraries", nil); status != 401 {
		t.Errorf("nobody signed in: %d, want 401", status)
	}
}

func mapsEqual(a, b map[string]any) bool {
	return strings.TrimSpace(toJSON(a)) == strings.TrimSpace(toJSON(b))
}

func TestOnlyStorageManagersChangeLibraries(t *testing.T) {
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	id := a.library(admin, "Books", "shared")
	_, readerID := a.signedIn("somebody", "reader")

	for _, role := range []string{"reader", "editor"} {
		c, _ := a.signedIn("a-"+role, role)
		attempts := []struct {
			method, path string
			body         any
		}{
			{http.MethodPost, "/libraries", map[string]any{"name": "Mine", "mode": "managed", "visibility": "private"}},
			{http.MethodPatch, "/libraries/" + id, map[string]any{"name": "Renamed"}},
			{http.MethodDelete, "/libraries/" + id, nil},
			{http.MethodGet, "/libraries/" + id + "/members", nil},
			{http.MethodPut, "/libraries/" + id + "/members/" + readerID, nil},
			{http.MethodDelete, "/libraries/" + id + "/members/" + readerID, nil},
		}
		for _, at := range attempts {
			if status, body, _ := a.call(c, at.method, at.path, at.body); status != 403 {
				t.Errorf("%s %s as %s: %d %v, want 403", at.method, at.path, role, status, body)
			}
		}
		// What a reader is shown leaves out where the files lie on the server.
		_, body, _ := a.call(c, http.MethodGet, "/libraries/"+id, nil)
		if _, shown := body["rootPath"]; shown || body["name"] != "Books" {
			t.Errorf("a %s is shown %v", role, body)
		}
	}
	if _, body, _ := a.call(admin, http.MethodGet, "/libraries/"+id, nil); body["rootPath"] == nil {
		t.Errorf("an admin is not shown the folder: %v", body)
	}
	if got := a.libraryNames(admin); !slices.Equal(got, []string{"Books"}) {
		t.Errorf("after the refused attempts the libraries are %v", got)
	}
}

func TestManagedAndExternalLibraries(t *testing.T) {
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")

	// Managed: GOtome makes the folder and always writes to it.
	status, managed, _ := a.call(admin, http.MethodPost, "/libraries", map[string]any{
		"name": "  My   Books ", "mode": "managed", "visibility": "shared",
	})
	if status != 201 || managed["name"] != "My Books" || managed["writable"] != true {
		t.Fatalf("managed library: %d %v", status, managed)
	}
	root := managed["rootPath"].(string)
	if !strings.HasPrefix(root, a.dataDir) {
		t.Errorf("the managed folder %s is not under the data directory %s", root, a.dataDir)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Errorf("the managed folder was not created: %v", err)
	}
	status, body, _ := a.call(admin, http.MethodPatch, "/libraries/"+managed["id"].(string), map[string]any{"writable": false})
	if status != 422 || fieldError(body, "writable") == "" {
		t.Errorf("making a managed library read-only: %d %v", status, body)
	}

	// External: the folder must exist, and is read-only until somebody says otherwise.
	books := t.TempDir()
	status, body, _ = a.call(admin, http.MethodPost, "/libraries", map[string]any{
		"name": "NAS", "mode": "external", "rootPath": filepath.Join(books, "missing"), "visibility": "shared",
	})
	if status != 422 || fieldError(body, "rootPath") == "" {
		t.Errorf("an external library on a folder that does not exist: %d %v", status, body)
	}
	status, external, _ := a.call(admin, http.MethodPost, "/libraries", map[string]any{
		"name": "NAS", "mode": "external", "rootPath": books + "/./", "visibility": "shared",
	})
	if status != 201 || external["writable"] != false || external["rootPath"] != books {
		t.Fatalf("external library: %d %v", status, external)
	}
	status, body, _ = a.call(admin, http.MethodPatch, "/libraries/"+external["id"].(string), map[string]any{"writable": true})
	if status != 200 || body["writable"] != true {
		t.Errorf("making an external library writable: %d %v", status, body)
	}

	// What cannot be a library next to these two.
	refused := []struct {
		why   string
		body  map[string]any
		field string
	}{
		{"no name", map[string]any{"name": " ", "mode": "managed", "visibility": "shared"}, "name"},
		{"a name in use, in other case", map[string]any{"name": "my books", "mode": "managed", "visibility": "shared"}, "name"},
		{"no mode", map[string]any{"name": "X", "visibility": "shared"}, "mode"},
		{"no visibility", map[string]any{"name": "X", "mode": "managed"}, "visibility"},
		{"external without a folder", map[string]any{"name": "X", "mode": "external", "visibility": "shared"}, "rootPath"},
		{"a relative folder", map[string]any{"name": "X", "mode": "external", "rootPath": "books", "visibility": "shared"}, "rootPath"},
		{"a folder in use", map[string]any{"name": "X", "mode": "external", "rootPath": books, "visibility": "shared"}, "rootPath"},
		{"a folder inside another library", map[string]any{"name": "X", "mode": "external", "rootPath": filepath.Join(books, "sub"), "visibility": "shared"}, "rootPath"},
		{"a folder around another library", map[string]any{"name": "X", "mode": "external", "rootPath": filepath.Dir(books), "visibility": "shared"}, "rootPath"},
	}
	for _, r := range refused {
		status, body, _ := a.call(admin, http.MethodPost, "/libraries", r.body)
		if status != 422 || fieldError(body, r.field) == "" {
			t.Errorf("%s: %d %v, want 422 with a message for %s", r.why, status, body, r.field)
		}
	}
	if got := a.libraryNames(admin); !slices.Equal(got, []string{"My Books", "NAS"}) {
		t.Errorf("after the refused attempts the libraries are %v", got)
	}

	// Removing a library forgets it; the files are nobody's to delete.
	keep := filepath.Join(books, "book.epub")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := a.call(admin, http.MethodDelete, "/libraries/"+external["id"].(string), nil); status != 204 {
		t.Fatalf("delete: %d", status)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("deleting the library touched its files: %v", err)
	}
	if status, _, _ := a.call(admin, http.MethodDelete, "/libraries/"+external["id"].(string), nil); status != 404 {
		t.Errorf("deleting it again: %d, want 404", status)
	}
}

func TestLibraryMembers(t *testing.T) {
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	_, anna := a.signedIn("anna", "reader")
	_, bert := a.signedIn("Bert", "editor")
	id := a.library(admin, "Family", "private")
	members := "/libraries/" + id + "/members"

	for _, user := range []string{bert, anna, anna} {
		if status, _, _ := a.call(admin, http.MethodPut, members+"/"+user, nil); status != 204 {
			t.Fatalf("add member: %d", status)
		}
	}
	_, body, _ := a.call(admin, http.MethodGet, members, nil)
	var names []string
	for _, m := range body["members"].([]any) {
		names = append(names, m.(map[string]any)["username"].(string))
	}
	if !slices.Equal(names, []string{"anna", "Bert"}) {
		t.Errorf("members = %v, want anna and Bert, each once, by name", names)
	}

	if status, _, _ := a.call(admin, http.MethodPut, members+"/019a0000-0000-7000-8000-000000000000", nil); status != 404 {
		t.Errorf("adding a user who does not exist: %d, want 404", status)
	}
	if status, _, _ := a.call(admin, http.MethodGet, "/libraries/019a0000-0000-7000-8000-000000000000/members", nil); status != 404 {
		t.Errorf("members of a library that does not exist: %d, want 404", status)
	}

	// A deleted user is no member of anything.
	if _, err := a.pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", anna); err != nil {
		t.Fatal(err)
	}
	_, body, _ = a.call(admin, http.MethodGet, members, nil)
	if got := len(body["members"].([]any)); got != 1 {
		t.Errorf("%d members after one was deleted, want 1", got)
	}
}
