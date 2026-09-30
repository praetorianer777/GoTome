package auth

import (
	"slices"
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
