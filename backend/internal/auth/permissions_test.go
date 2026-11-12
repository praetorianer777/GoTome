package auth

import (
	"slices"
	"strings"
	"testing"
)

func TestRolesNest(t *testing.T) {
	reader, editor, admin := Permissions(RoleReader), Permissions(RoleEditor), Permissions(RoleAdmin)
	if len(reader) == 0 || len(editor) <= len(reader) || len(admin) <= len(editor) {
		t.Fatalf("reader %d, editor %d, admin %d permissions: each role must add to the one below", len(reader), len(editor), len(admin))
	}
	for _, p := range reader {
		if !slices.Contains(editor, p) {
			t.Errorf("an editor lacks the reader's %s", p)
		}
	}
	for _, p := range editor {
		if !slices.Contains(admin, p) {
			t.Errorf("an admin lacks the editor's %s", p)
		}
	}
}

func TestAllows(t *testing.T) {
	cases := []struct {
		role       string
		permission Permission
		want       bool
	}{
		{RoleReader, LibraryRead, true},
		{RoleReader, PersonalManage, true},
		{RoleReader, BooksUpload, false},
		{RoleReader, UsersManage, false},
		{RoleEditor, BooksUpload, true},
		{RoleEditor, MetadataEdit, true},
		{RoleEditor, IndexRebuild, true},
		{RoleEditor, SettingsManage, false},
		{RoleEditor, StorageManage, false},
		{RoleAdmin, UsersManage, true},
		{RoleAdmin, SettingsManage, true},
		{RoleAdmin, StorageManage, true},
		{RoleAdmin, MetricsRead, true},
		{RoleEditor, MetricsRead, false},
		{"", LibraryRead, false},
		{"owner", LibraryRead, false},
		// The two route markers are not something a role holds.
		{RoleAdmin, Public, false},
		{RoleAdmin, SignedIn, false},
	}
	for _, c := range cases {
		if got := Allows(c.role, c.permission); got != c.want {
			t.Errorf("Allows(%q, %s) = %v, want %v", c.role, c.permission, got, c.want)
		}
	}
}

func TestPermissionsReturnsACopy(t *testing.T) {
	Permissions(RoleReader)[0] = UsersManage
	if Allows(RoleReader, UsersManage) {
		t.Error("a caller changed what a reader may do")
	}
}

func TestKnown(t *testing.T) {
	for _, p := range append(Permissions(RoleAdmin), Public, SignedIn) {
		if !Known(p) {
			t.Errorf("%s is not known", p)
		}
	}
	for _, p := range []Permission{"", "admin", "library:write"} {
		if Known(p) {
			t.Errorf("%q is known", p)
		}
	}
}

func TestEveryPermissionIsInOneScopeAndNoScopeInventsOne(t *testing.T) {
	held := map[Permission]string{}
	for _, scope := range Scopes() {
		perms := scopePermissions[scope]
		if len(perms) == 0 {
			t.Errorf("scope %s holds nothing", scope)
		}
		for _, p := range perms {
			if !Known(p) || p == Public || p == SignedIn {
				t.Errorf("scope %s holds %q, which is no role's permission", scope, p)
			}
			if other, dup := held[p]; dup {
				t.Errorf("%s is in both %s and %s", p, other, scope)
			}
			held[p] = scope
		}
	}
	for _, p := range Permissions(RoleAdmin) {
		if _, ok := held[p]; !ok {
			t.Errorf("no scope reaches %s: put it in one", p)
		}
	}
	if len(scopePermissions) != len(Scopes()) {
		t.Error("Scopes and the scopes' permissions differ")
	}
}

func TestScopesAllow(t *testing.T) {
	cases := []struct {
		scopes []string
		p      Permission
		want   bool
	}{
		{[]string{ScopeRead}, LibraryRead, true},
		{[]string{ScopeRead}, PersonalManage, false},
		{[]string{ScopeRead, ScopeProgress}, PersonalManage, true},
		{[]string{ScopeUpload}, BooksUpload, true},
		{[]string{ScopeUpload}, MetadataEdit, false},
		{[]string{ScopeEdit}, IndexRebuild, true},
		{[]string{ScopeEdit}, UsersManage, false},
		{[]string{ScopeAdmin}, SettingsManage, true},
		{[]string{ScopeAdmin}, LibraryRead, false},
		{[]string{ScopeMetrics}, MetricsRead, true},
		{[]string{ScopeAdmin}, MetricsRead, false},
		{[]string{ScopeMetrics}, SettingsManage, false},
		{[]string{"root"}, LibraryRead, false},
		{nil, LibraryRead, false},
		{[]string{ScopeAdmin}, SignedIn, false},
		{[]string{ScopeAdmin}, Public, false},
	}
	for _, c := range cases {
		if got := ScopesAllow(c.scopes, c.p); got != c.want {
			t.Errorf("ScopesAllow(%v, %s) = %v, want %v", c.scopes, c.p, got, c.want)
		}
	}
}

func TestAKeyIsKnownByItsShape(t *testing.T) {
	good := apiKeyPrefix + strings.Repeat("aB3-_", 8) + "xyz"
	if !LooksLikeAPIKey(good) {
		t.Errorf("%q is taken for no key", good)
	}
	for _, bad := range []string{"", "gotome_", good[:len(good)-1], good + "a", "other__" + good[len(apiKeyPrefix):], good[:10] + "!" + good[11:], good[:10] + " " + good[11:]} {
		if LooksLikeAPIKey(bad) {
			t.Errorf("%q is taken for a key", bad)
		}
	}
}
