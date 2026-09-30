package library

import (
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
)

func TestWithin(t *testing.T) {
	cases := []struct {
		child, parent string
		want          bool
	}{
		{"/books/fiction", "/books", true},
		{"/books/a/b/c", "/books", true},
		{"/books", "/books", false},
		{"/books", "/books/fiction", false},
		// A shared prefix of characters is not a shared folder.
		{"/books-old", "/books", false},
		{"/other", "/books", false},
		{"/books/..hidden", "/books", true},
	}
	for _, c := range cases {
		if got := within(c.child, c.parent); got != c.want {
			t.Errorf("within(%q, %q) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

func TestScopeOf(t *testing.T) {
	id := uuid.New()
	for role, seesAll := range map[string]bool{auth.RoleAdmin: true, auth.RoleEditor: false, auth.RoleReader: false, "": false} {
		scope := ScopeOf(auth.User{ID: id, Role: role})
		if scope.Viewer != id || scope.SeesAll != seesAll {
			t.Errorf("role %q: scope %+v, want SeesAll %v", role, scope, seesAll)
		}
	}
}

func TestValidName(t *testing.T) {
	fields := map[string]string{}
	if got := validName("  My   Books ", fields); got != "My Books" || len(fields) != 0 {
		t.Errorf("name = %q, fields = %v", got, fields)
	}
	validName("   ", fields)
	if fields["name"] == "" {
		t.Error("a blank name was accepted")
	}
}
