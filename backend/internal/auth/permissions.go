package auth

import "slices"

// Permission is one thing a route may require of whoever calls it.
type Permission string

// Two values are not permissions a role holds but statements about the route.
const (
	// Public routes answer anybody, signed in or not.
	Public Permission = "public"
	// SignedIn routes answer anybody who is signed in, whatever their role:
	// they concern the caller's own account.
	SignedIn Permission = "signed-in"
)

// What the roles are made of. A Reader uses the library, an Editor also
// changes it, an Admin also runs the installation.
const (
	// LibraryRead is viewing, reading and downloading books.
	LibraryRead Permission = "library:read"
	// PersonalManage is one's own reading status, progress and shelves.
	PersonalManage Permission = "personal:manage"

	BooksUpload  Permission = "books:upload"
	MetadataEdit Permission = "metadata:edit"
	// IndexRebuild is triggering scans and re-indexing.
	IndexRebuild Permission = "index:rebuild"

	UsersManage    Permission = "users:manage"
	SettingsManage Permission = "settings:manage"
	// StorageManage is libraries on disk and the limits on what users may fill.
	StorageManage Permission = "storage:manage"
)

var (
	readerPermissions = []Permission{LibraryRead, PersonalManage}
	editorPermissions = append(slices.Clone(readerPermissions), BooksUpload, MetadataEdit, IndexRebuild)
	adminPermissions  = append(slices.Clone(editorPermissions), UsersManage, SettingsManage, StorageManage)
)

// Permissions lists what a role may do. An unknown role may do nothing.
func Permissions(role string) []Permission {
	switch role {
	case RoleAdmin:
		return slices.Clone(adminPermissions)
	case RoleEditor:
		return slices.Clone(editorPermissions)
	case RoleReader:
		return slices.Clone(readerPermissions)
	}
	return nil
}

// Allows reports whether the role holds the permission.
func Allows(role string, p Permission) bool {
	return slices.Contains(Permissions(role), p)
}

// Known reports whether p is a value a route may declare.
func Known(p Permission) bool {
	return p == Public || p == SignedIn || slices.Contains(adminPermissions, p)
}
