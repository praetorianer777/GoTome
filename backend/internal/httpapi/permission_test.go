package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/gotome/backend/internal/auth"
)

// as sends a request through require on behalf of a user with the role; an
// empty role is nobody signed in.
func as(t *testing.T, role string, permission auth.Permission) int {
	t.Helper()
	reached := false
	h := handle(require(permission, func(w http.ResponseWriter, _ *http.Request) error {
		reached = true
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if role != "" {
		r = r.WithContext(context.WithValue(r.Context(), userKey, &auth.User{Username: role, Role: role}))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if reached != (rec.Code == http.StatusNoContent) {
		t.Fatalf("%s as %q: the handler ran = %v, status %d", permission, role, reached, rec.Code)
	}
	return rec.Code
}

func TestRequire(t *testing.T) {
	const ok, signIn, refused = http.StatusNoContent, http.StatusUnauthorized, http.StatusForbidden
	cases := []struct {
		permission                    auth.Permission
		nobody, reader, editor, admin int
	}{
		{auth.Public, ok, ok, ok, ok},
		{auth.SignedIn, signIn, ok, ok, ok},
		{auth.LibraryRead, signIn, ok, ok, ok},
		{auth.PersonalManage, signIn, ok, ok, ok},
		{auth.BooksUpload, signIn, refused, ok, ok},
		{auth.MetadataEdit, signIn, refused, ok, ok},
		{auth.IndexRebuild, signIn, refused, ok, ok},
		{auth.UsersManage, signIn, refused, refused, ok},
		{auth.SettingsManage, signIn, refused, refused, ok},
		{auth.StorageManage, signIn, refused, refused, ok},
	}
	covered := map[auth.Permission]bool{}
	for _, c := range cases {
		covered[c.permission] = true
		for role, want := range map[string]int{"": c.nobody, auth.RoleReader: c.reader, auth.RoleEditor: c.editor, auth.RoleAdmin: c.admin} {
			if got := as(t, role, c.permission); got != want {
				t.Errorf("%s as %q: status %d, want %d", c.permission, role, got, want)
			}
		}
	}
	for _, p := range auth.Permissions(auth.RoleAdmin) {
		if !covered[p] {
			t.Errorf("%s is not in this table; say who may use it", p)
		}
	}
	// A role that does not exist holds nothing.
	if got := as(t, "owner", auth.LibraryRead); got != refused {
		t.Errorf("an unknown role: status %d, want %d", got, refused)
	}
}

func TestEveryRouteDeclaresAPermission(t *testing.T) {
	doc := Spec()
	for _, rt := range testServer().routes() {
		name := rt.Method + " " + rt.Path
		if !auth.Known(rt.Permission) {
			t.Errorf("%s declares no known permission (%q)", name, rt.Permission)
			continue
		}
		op := doc.Paths[rt.Path][strings.ToLower(rt.Method)]
		if op.Permission != string(rt.Permission) {
			t.Errorf("%s: the document says %q, the route %q", name, op.Permission, rt.Permission)
		}
		if (len(op.Security) == 0) != (rt.Permission == auth.Public) {
			t.Errorf("%s: permission %s, but the document's security is %v", name, rt.Permission, op.Security)
		}
	}
	if doc.Components.SecuritySchemes[sessionScheme].Name != SessionCookie {
		t.Error("the document does not describe the session cookie")
	}
}

func mounts(routes ...Route) (panicked any) {
	defer func() { panicked = recover() }()
	mount(chi.NewRouter(), routes)
	return nil
}

func TestRouterRefusesATableWithAnUndeclaredRoute(t *testing.T) {
	handler := func(http.ResponseWriter, *http.Request) error { return nil }
	declared := Route{Method: http.MethodGet, Path: "/a", Permission: auth.LibraryRead, Handler: handler}

	if rec := mounts(declared); rec != nil {
		t.Errorf("a declared route does not mount: %v", rec)
	}
	for name, permission := range map[string]auth.Permission{"none": "", "a role, not a permission": "admin"} {
		undeclared := Route{Method: http.MethodGet, Path: "/b", Permission: permission, Handler: handler}
		rec := mounts(declared, undeclared)
		if rec == nil || !strings.Contains(rec.(string), "GET /b") {
			t.Errorf("%s: mounting said %v, want a refusal naming the route", name, rec)
		}
	}
	if rec := mounts(testServer().routes()...); rec != nil {
		t.Errorf("the real route table does not mount: %v", rec)
	}
}

func TestSessionRoutesOverHTTP(t *testing.T) {
	h := testServer().Routes()
	rec := do(t, h, http.MethodGet, APIPrefix+"/auth/me")
	if rec.Code != http.StatusUnauthorized || decodeError(t, rec).Code != "unauthorized" {
		t.Errorf("without a session: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, http.MethodGet, APIPrefix+"/version"); rec.Code != http.StatusOK {
		t.Errorf("a public route without a session: %d", rec.Code)
	}
}
