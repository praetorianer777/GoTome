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
	"library.empty.body":
		"Once a library folder is scanned or a book is uploaded, it shows up here.",

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
