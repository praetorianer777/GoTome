/**
 * Every string a person reads. Components ask for a key, so a second language
 * is a second file of this shape and nothing else.
 */
export const en = {
	"app.name": "GOtome",

	"nav.library": "Library",
	"nav.main": "Main",
	"nav.skip": "Skip to the content",

	"user.menu": "Account",
	"user.signedInAs": "Signed in as {name}",
	"user.signOut": "Sign out",
	"role.admin": "Administrator",
	"role.editor": "Editor",
	"role.reader": "Reader",

	"theme.label": "Colours",
	"theme.system": "As the device",
	"theme.light": "Light",
	"theme.dark": "Dark",

	"field.username": "User name",
	"field.password": "Password",
	"field.email": "E-mail address",
	"field.optional": "optional",

	"setup.title": "Welcome to GOtome",
	"setup.intro":
		"Create the first account. It becomes the administrator of this library.",
	"setup.passwordHint": "At least 8 characters.",
	"setup.submit": "Create the account",
	"setup.submitting": "Creating the account…",

	"login.title": "Sign in",
	"login.submit": "Sign in",
	"login.submitting": "Signing in…",

	"library.title": "Library",
	"library.empty.title": "No books yet",
	"library.empty.body": "Once this library's folder is scanned or a book is uploaded, it shows up here.",
	"library.none.title": "No library yet",
	"library.none.admin": "A library is a folder of books. Add one to get started.",
	"library.none.other": "There is no library you may see yet. An administrator can add one or share one with you.",
	"library.none.add": "Add a library",

	"nav.libraries": "Libraries",
	"libraries.title": "Libraries",
	"libraries.existing": "Existing libraries",
	"libraries.none": "There is no library yet.",
	"libraries.new": "Add a library",
	"libraries.name": "Name",
	"libraries.rename": "Rename",
	"libraries.folder": "Folder",
	"libraries.folder.hint": "The path inside the GOtome container, such as /books. Mount the folder in the compose file first.",
	"libraries.mode": "Kind",
	"libraries.mode.managed": "Managed by GOtome",
	"libraries.mode.external": "An existing folder",
	"libraries.mode.managed.hint": "GOtome creates the folder and arranges the files. Uploads land here.",
	"libraries.mode.external.hint": "GOtome reads a folder you arrange yourself and changes nothing in it unless you allow it.",
	"libraries.visibility": "Who sees it",
	"libraries.visibility.shared": "Everybody",
	"libraries.visibility.private": "Only its members",
	"libraries.writable": "GOtome may change files here",
	"libraries.create": "Add the library",
	"libraries.delete": "Remove",
	"libraries.delete.confirm": "Remove it from GOtome? The folder and its files stay.",
	"libraries.delete.yes": "Remove",
	"libraries.delete.no": "Keep",

	"notFound.title": "There is no page here",
	"notFound.body": "The address may be mistyped, or the page has moved.",
	"notFound.back": "Back to the library",

	"error.generic": "Something went wrong. Try again.",
	"error.unreachable":
		"The server does not answer. Check the connection and try again.",
	"error.loading": "This page could not be loaded.",
	"error.retry": "Try again",
	loading: "Loading…",
} as const;

export type MessageKey = keyof typeof en;
