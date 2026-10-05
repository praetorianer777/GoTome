import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BookDetail } from "@/books/api";
import type { Job } from "@/jobs/api";
import { FakeServer, renderApp } from "@/test/fake-server";

// PDF.js needs a canvas and a worker, which the test browser has not; the
// reader's tests see a document of ten pages instead.
vi.mock("@/reader/pdf", () => ({
	openPdf: async () => ({
		pages: 10,
		pageWidth: async () => 600,
		outline: async () => [
			{ id: "0", title: "Chapter One", page: 1, items: [] },
			{ id: "1", title: "Chapter Two", page: 6, items: [{ id: "1.0", title: "Nowhere", items: [] }] },
		],
		// Page 8 holds the passage the full-text search's tests open.
		text: async (page: number) => (page === 8 ? "a white ledger and a whale" : "a white page"),
		render: () => ({ done: Promise.resolve(), cancel: () => {} }),
		close: () => {},
	}),
}));

// foliate-js lays pages out, which the test browser does not; the reader's
// tests see a book of three chapters that moves a tenth on with each page.
const ebook = vi.hoisted(() => ({
	opened: [] as { name: string; start?: string }[],
	looks: [] as unknown[],
	finds: [] as { word: string; passage: string }[],
}));
vi.mock("@/reader/ebook", () => ({
	openEbook: async (
		_into: HTMLElement,
		file: File,
		options: {
			start?: string;
			look: unknown;
			onPlace: (p: { cfi: string; fraction: number; chapter?: string; atEnd: boolean }) => void;
		},
	) => {
		ebook.opened.push({ name: file.name, start: options.start });
		ebook.looks.push(options.look);
		let at = options.start ? Number(/\/(\d+)\)$/.exec(options.start)?.[1] ?? 0) : 0;
		const chapter = () => ["One", "Two", "Three"][Math.min(Math.floor(at / 4), 2)];
		const place = () =>
			options.onPlace({ cfi: `epubcfi(/6/${at})`, fraction: at / 10, chapter: chapter(), atEnd: at >= 10 });
		const turn = async (step: number) => {
			at = Math.min(Math.max(at + step, 0), 10);
			place();
		};
		place();
		return {
			toc: [
				{ key: "0", label: "One", href: "one.xhtml", depth: 0 },
				{ key: "1", label: "Two", href: "two.xhtml", depth: 0 },
				{ key: "2", label: "Three", href: "three.xhtml", depth: 1 },
			],
			// The word is found in the second chapter, at the sixth tenth.
			find: async (word: string, passage: string) => {
				ebook.finds.push({ word, passage });
				at = 5;
				place();
				return true;
			},
			goTo: async (target: string) => {
				at = target.startsWith("epubcfi(") ? Number(/\/(\d+)\)$/.exec(target)?.[1] ?? 0) : ["one.xhtml", "two.xhtml", "three.xhtml"].indexOf(target) * 4;
				place();
			},
			goLeft: () => turn(-1),
			goRight: () => turn(1),
			prev: () => turn(-1),
			next: () => turn(1),
			setLook: (look: unknown) => ebook.looks.push(look),
			close: () => {},
		};
	},
}));

let server: FakeServer;

beforeEach(() => {
	vi.restoreAllMocks();
	localStorage.clear();
	ebook.opened.length = 0;
	ebook.looks.length = 0;
	ebook.finds.length = 0;
	delete document.documentElement.dataset.theme;
	server = new FakeServer().install();
});

describe("a fresh installation", () => {
	it("leads from any address through setup into the empty library", async () => {
		const person = userEvent.setup();
		const { router } = renderApp("/");

		expect(
			await screen.findByRole("heading", { name: "Welcome to GOtome" }),
		).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/setup");

		await person.type(screen.getByLabelText("User name"), "Steve");
		await person.type(screen.getByLabelText("Password"), "a long password");
		await person.click(
			screen.getByRole("button", { name: "Create the account" }),
		);

		expect(
			await screen.findByRole("heading", { name: "No library yet" }),
		).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
		expect(screen.getByText("Signed in as Steve")).toBeInTheDocument();
		expect(server.requests).toContainEqual({
			method: "POST",
			path: "/setup",
			body: { username: "Steve", password: "a long password" },
		});
	});

	it("shows the server's message at the field it is about", async () => {
		const person = userEvent.setup();
		renderApp("/setup");

		await person.type(await screen.findByLabelText("User name"), "Steve");
		await person.type(screen.getByLabelText("Password"), "short");
		await person.click(
			screen.getByRole("button", { name: "Create the account" }),
		);

		const password = screen.getByLabelText("Password");
		await waitFor(() => expect(password).toBeInvalid());
		expect(password).toHaveAccessibleDescription(
			"A password has at least 8 characters.",
		);
	});

	it("shows the artwork without shifting the page when it loads", async () => {
		renderApp("/setup");
		const artwork = await screen.findByRole("img", { name: "GOtome" });
		// The size is reserved up front; without it the form would jump.
		expect(artwork).toHaveAttribute("width", "640");
		expect(artwork).toHaveAttribute("height", "659");
	});

	it("sends the sign-in page to setup", async () => {
		const { router } = renderApp("/login");
		expect(
			await screen.findByRole("heading", { name: "Welcome to GOtome" }),
		).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/setup");
	});
});

describe("an installation that is set up", () => {
	beforeEach(() => {
		server.withAccount("Steve", "a long password");
	});

	it("asks for sign-in and then goes where the person was going", async () => {
		const person = userEvent.setup();
		const { router } = renderApp("/?view=grid");

		expect(
			await screen.findByRole("heading", { name: "Sign in" }),
		).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/login");
		expect(router.state.location.search).toEqual({ redirect: "/?view=grid" });

		await person.type(screen.getByLabelText("User name"), "steve");
		await person.type(screen.getByLabelText("Password"), "a long password");
		await person.click(screen.getByRole("button", { name: "Sign in" }));

		expect(
			await screen.findByRole("heading", { name: "Library" }),
		).toBeInTheDocument();
		expect(router.state.location.href).toBe("/?view=grid");
	});

	it("says why a sign-in failed and stays on the page", async () => {
		const person = userEvent.setup();
		const { router } = renderApp("/login");

		await person.type(await screen.findByLabelText("User name"), "steve");
		await person.type(screen.getByLabelText("Password"), "not the password");
		await person.click(screen.getByRole("button", { name: "Sign in" }));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"The user name or the password is wrong.",
		);
		expect(router.state.location.pathname).toBe("/login");
	});

	it("does not follow a redirect to another site", async () => {
		const person = userEvent.setup();
		const { router } = renderApp("/login?redirect=//evil.example/steal");

		await person.type(await screen.findByLabelText("User name"), "steve");
		await person.type(screen.getByLabelText("Password"), "a long password");
		await person.click(screen.getByRole("button", { name: "Sign in" }));

		expect(
			await screen.findByRole("heading", { name: "Library" }),
		).toBeInTheDocument();
		expect(router.state.location.href).toBe("/");
	});

	it("keeps setup closed", async () => {
		const { router } = renderApp("/setup");
		expect(
			await screen.findByRole("heading", { name: "Sign in" }),
		).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/login");
	});
});

describe("signed in", () => {
	beforeEach(() => {
		server.withAccount("Steve", "a long password").signedInAs("Steve");
	});

	it("shows the shell around the empty library", async () => {
		renderApp("/");

		expect(
			await screen.findByRole("heading", { name: "Library" }),
		).toBeInTheDocument();
		expect(screen.getByRole("navigation", { name: "Main" })).toContainElement(
			screen.getByRole("link", { name: "Library" }),
		);
		expect(screen.getByText("Signed in as Steve")).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: "Skip to the content" }),
		).toHaveAttribute("href", "#content");
	});

	it("skips the sign-in page", async () => {
		const { router } = renderApp("/login");
		expect(
			await screen.findByRole("heading", { name: "Library" }),
		).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
	});

	it("signs out and asks for sign-in again", async () => {
		const person = userEvent.setup();
		const { router } = renderApp("/");

		await person.click(await screen.findByRole("button", { name: "Sign out" }));

		expect(
			await screen.findByRole("heading", { name: "Sign in" }),
		).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/login");
		expect(server.session).toBeNull();
	});

	it("remembers the chosen colours", async () => {
		const person = userEvent.setup();
		const first = renderApp("/");

		await person.selectOptions(await screen.findByLabelText("Colours"), "Dark");
		expect(document.documentElement.dataset.theme).toBe("dark");

		first.unmount();
		delete document.documentElement.dataset.theme;
		renderApp("/");
		expect(await screen.findByLabelText("Colours")).toHaveValue("dark");
		expect(document.documentElement.dataset.theme).toBe("dark");

		await person.selectOptions(
			screen.getByLabelText("Colours"),
			"As the device",
		);
		expect(document.documentElement.dataset.theme).toBeUndefined();
	});

	it("answers an unknown address with a way back", async () => {
		renderApp("/no/such/page");
		expect(
			await screen.findByRole("heading", { name: "There is no page here" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: "Back to the library" }),
		).toHaveAttribute("href", "/");
	});
});

describe("a server that does not answer", () => {
	it("says so and offers to try again", async () => {
		const person = userEvent.setup();
		server.withAccount("Steve", "a long password").signedInAs("Steve");
		server.down = true;
		renderApp("/");

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"The server does not answer.",
		);

		server.down = false;
		await person.click(screen.getByRole("button", { name: "Try again" }));
		expect(
			await screen.findByRole("heading", { name: "Library" }),
		).toBeInTheDocument();
	});
});

describe("libraries", () => {
	it("tells an administrator with no library how to get started", async () => {
		const person = userEvent.setup();
		server.withAccount("Steve", "a long password").signedInAs("Steve");
		const { router } = renderApp("/");

		await person.click(await screen.findByRole("link", { name: "Add a library" }));
		expect(await screen.findByRole("heading", { name: "Libraries", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/admin/libraries");
	});

	it("tells a reader with no library that there is none to see", async () => {
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita");
		renderApp("/");

		expect(await screen.findByRole("heading", { name: "No library yet" })).toBeInTheDocument();
		expect(screen.getByText(/An administrator can add one/)).toBeInTheDocument();
		expect(screen.queryByRole("link", { name: "Add a library" })).not.toBeInTheDocument();
		expect(screen.queryByRole("link", { name: "Libraries" })).not.toBeInTheDocument();
	});

	it("keeps a reader out of the administration page", async () => {
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita").withLibrary("Novels");
		const { router } = renderApp("/admin/libraries");

		expect(await screen.findByRole("heading", { name: "Novels" })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
	});

	it("adds a managed library and shows it on the library page", async () => {
		const person = userEvent.setup();
		server.withAccount("Steve", "a long password").signedInAs("Steve");
		renderApp("/admin/libraries");

		expect(await screen.findByText("There is no library yet.")).toBeInTheDocument();
		const form = within(screen.getByRole("region", { name: "Add a library" }));
		await person.type(form.getByLabelText("Name"), "Novels");
		await person.click(form.getByRole("button", { name: "Add the library" }));

		const row = await screen.findByRole("listitem", { name: "Novels" });
		expect(within(row).getByText("Managed by GOtome")).toBeInTheDocument();
		expect(form.getByLabelText("Name")).toHaveValue("");
		expect(server.requests).toContainEqual({
			method: "POST",
			path: "/libraries",
			body: { name: "Novels", mode: "managed", visibility: "shared" },
		});

		await person.click(screen.getByRole("link", { name: "Library" }));
		expect(await screen.findByRole("heading", { name: "Novels" })).toBeInTheDocument();
	});

	it("asks for the folder of an existing one and shows what the server says about it", async () => {
		const person = userEvent.setup();
		server.withAccount("Steve", "a long password").signedInAs("Steve");
		renderApp("/admin/libraries");

		const form = within(await screen.findByRole("region", { name: "Add a library" }));
		expect(form.queryByLabelText("Folder")).not.toBeInTheDocument();
		await person.selectOptions(form.getByLabelText("Kind"), "An existing folder");
		await person.type(form.getByLabelText("Name"), "NAS");
		await person.type(form.getByLabelText("Folder"), "books");
		await person.click(form.getByRole("button", { name: "Add the library" }));

		await waitFor(() => expect(form.getByLabelText("Folder")).toBeInvalid());
		expect(form.getByLabelText("Folder")).toHaveAccessibleDescription("Give the folder as a full path, such as /books.");
	});

	it("renames, changes who sees it, and removes only after being asked twice", async () => {
		const person = userEvent.setup();
		server
			.withAccount("Steve", "a long password")
			.signedInAs("Steve")
			.withLibrary("Novels")
			.withLibrary("NAS", { mode: "external", writable: false, rootPath: "/books" });
		renderApp("/admin/libraries");

		const novels = within(await screen.findByRole("listitem", { name: "Novels" }));
		// Only a folder GOtome does not manage can be read-only.
		expect(novels.queryByRole("checkbox")).not.toBeInTheDocument();
		await person.clear(novels.getByLabelText("Name"));
		await person.type(novels.getByLabelText("Name"), "Fiction");
		await person.click(novels.getByRole("button", { name: "Rename" }));
		const fiction = within(await screen.findByRole("listitem", { name: "Fiction" }));

		await person.selectOptions(fiction.getByLabelText("Who sees it"), "Only its members");
		await waitFor(() => expect(server.libraries[0]?.visibility).toBe("private"));

		const nas = within(screen.getByRole("listitem", { name: "NAS" }));
		expect(nas.getByText("/books")).toBeInTheDocument();
		await person.click(nas.getByRole("checkbox", { name: "GOtome may change files here" }));
		await waitFor(() => expect(server.libraries[1]?.writable).toBe(true));

		await person.click(nas.getByRole("button", { name: "Remove" }));
		expect(server.libraries).toHaveLength(2);
		expect(nas.getByText("Remove it from GOtome? The folder and its files stay.")).toBeInTheDocument();
		await person.click(nas.getByRole("button", { name: "Keep" }));
		await person.click(nas.getByRole("button", { name: "Remove" }));
		await person.click(nas.getByRole("button", { name: "Remove" }));

		await waitFor(() => expect(screen.queryByRole("listitem", { name: "NAS" })).not.toBeInTheDocument());
		expect(server.libraries.map((l) => l.name)).toEqual(["Fiction"]);
	});

	it("lets an administrator scan a library and says what the scan found", async () => {
		const person = userEvent.setup();
		server.withAccount("Steve", "a long password").signedInAs("Steve").withLibrary("Novels");
		server.scanFinds = { filesSeen: 12, filesAdded: 10, filesMissing: 2, booksAdded: 9 };
		renderApp("/admin/libraries");

		const row = within(await screen.findByRole("listitem", { name: "Novels" }));
		expect(row.getByRole("status")).toHaveTextContent("Not scanned yet.");
		await person.click(row.getByRole("button", { name: "Scan now" }));

		await waitFor(() => expect(row.getByRole("status")).toHaveTextContent(/^Scanned .+: 12 files, 10 new, 2 missing\.$/));
		expect(server.requests).toContainEqual({ method: "POST", path: "/libraries/lib-1/scans", body: undefined });
	});

	it("shows a scan that is under way and one that failed", async () => {
		const scan = {
			id: "scan-1", libraryId: "lib-1", requestedAt: "2026-01-02T10:00:00Z",
			filesSeen: 0, filesAdded: 0, filesChanged: 0, filesMoved: 0,
			filesRestored: 0, filesMissing: 0, filesSkipped: 0, booksAdded: 0,
		};
		server
			.withAccount("Steve", "a long password")
			.signedInAs("Steve")
			.withLibrary("Novels", { lastScan: { ...scan, state: "running" } })
			.withLibrary("NAS", { lastScan: { ...scan, state: "failed", error: "The library's folder could not be read." } });
		renderApp("/admin/libraries");

		const novels = within(await screen.findByRole("listitem", { name: "Novels" }));
		expect(novels.getByRole("status")).toHaveTextContent("Scanning…");
		// One scan at a time: the button waits for the one under way.
		expect(novels.getByRole("button", { name: "Scan now" })).toBeDisabled();

		const nas = within(screen.getByRole("listitem", { name: "NAS" }));
		expect(nas.getByRole("status")).toHaveTextContent(/failed\. The library's folder could not be read\.$/);
		expect(nas.getByRole("button", { name: "Scan now" })).toBeEnabled();
	});

	it("lets an editor scan from the library page, and a reader only look", async () => {
		const person = userEvent.setup();
		server
			.withAccount("Edith", "a long password", "editor")
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Edith")
			.withLibrary("Novels");
		server.scanFinds = { filesSeen: 3, filesAdded: 3, booksAdded: 2 };
		const { unmount } = renderApp("/");

		const section = within(await screen.findByRole("region", { name: "Novels" }));
		expect(await screen.findByRole("heading", { name: "No books yet" })).toBeInTheDocument();
		await person.click(section.getByRole("button", { name: "Scan now" }));
		await waitFor(() => expect(section.getByRole("status")).toHaveTextContent(/3 files, 3 new\.$/));
		unmount();

		server.signedInAs("Rita");
		renderApp("/");
		const forReader = within(await screen.findByRole("region", { name: "Novels" }));
		expect(forReader.getByRole("status")).toHaveTextContent(/3 files, 3 new\.$/);
		expect(forReader.queryByRole("button", { name: "Scan now" })).not.toBeInTheDocument();
	});
});

describe("books", () => {
	// The text of each book's card, leaving out the stand-in for a missing
	// cover, which carries the title again for the eye only.
	const cards = (books: ReturnType<typeof within>) =>
		books.getAllByRole("link").map((l: HTMLElement) => l.lastElementChild?.textContent);

	function withShelf() {
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withLibrary("Audio")
			.withBook("Persuasion", {
				contributors: [{ name: "Jane Austen", role: "author" }],
				files: [{ id: "file-1", kind: "ebook", format: "epub", name: "Persuasion.epub", size: 1_400_000, missing: false, drm: false , extractState: "done" }],
			})
			.withBook("Dune", {
				subtitle: "Deluxe Edition",
				contributors: [
					{ name: "Frank Herbert", role: "author" },
					{ name: "Scott Brick", role: "narrator" },
				],
				series: "Dune Chronicles",
				seriesIndex: 1,
				published: "1965",
				publisher: "Chilton",
				description: "A desert planet.\n\nAnd a boy.",
				tags: ["Science Fiction"],
				identifiers: [{ type: "isbn", value: "9780441013593", fromFile: true }],
				durationMs: 21 * 3600_000 + 2 * 60_000,
				files: [
					{ id: "file-2", kind: "audio", format: "mp3", name: "Dune 1.mp3", size: 400_000_000, missing: false, drm: false, part: 0, durationMs: 11 * 3600_000 , extractState: "done" },
					{ id: "file-3", kind: "audio", format: "mp3", name: "Dune 2.mp3", size: 380_000_000, missing: true, drm: false, part: 1 , extractState: "done" },
				],
			}, "Audio")
			.withBook("Emma", { contributors: [{ name: "Jane Austen", role: "author" }] });
	}

	it("shows every library's books, and one library's on request", async () => {
		const person = userEvent.setup();
		withShelf();
		const { router } = renderApp("/");

		const books = within(await screen.findByRole("region", { name: "Books" }));
		await waitFor(() => expect(cards(books)).toEqual([
			"DuneFrank Herbert", "EmmaJane Austen", "PersuasionJane Austen",
		]));

		await person.selectOptions(screen.getByLabelText("Library"), "Audio");
		await waitFor(() => expect(books.getAllByRole("link")).toHaveLength(1));
		expect(router.state.location.search).toEqual({ library: "lib-2" });
		expect(screen.getByRole("heading", { name: "Audio", level: 2 })).toBeInTheDocument();
		// A reader is shown the scan, not the button that starts one.
		expect(screen.queryByRole("button", { name: "Scan now" })).not.toBeInTheDocument();
	});

	it("sorts, and lists instead of showing covers", async () => {
		const person = userEvent.setup();
		withShelf();
		renderApp("/");
		const books = within(await screen.findByRole("region", { name: "Books" }));
		await waitFor(() => expect(books.getAllByRole("link")).toHaveLength(3));

		await person.selectOptions(screen.getByLabelText("Sort by"), "Author");
		await waitFor(() => expect(cards(books)).toEqual([
			"DuneFrank Herbert", "EmmaJane Austen", "PersuasionJane Austen",
		]));
		expect(server.requests.some((r) => r.path.includes("sort=author") && r.path.includes("order=asc"))).toBe(true);

		await person.selectOptions(screen.getByLabelText("Sort by"), "Recently added");
		await waitFor(() => expect(cards(books)).toEqual([
			"EmmaJane Austen", "DuneFrank Herbert", "PersuasionJane Austen",
		]));

		await person.click(screen.getByRole("button", { name: "List" }));
		expect(screen.getByRole("button", { name: "List" })).toHaveAttribute("aria-pressed", "true");
		const rows = within(books.getByRole("table")).getAllByRole("row");
		expect(rows[1]).toHaveTextContent("Emma");
		expect(rows[2]).toHaveTextContent("Dune");
	});

	it("loads a large library a page at a time", async () => {
		const person = userEvent.setup();
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita").withLibrary("Novels");
		for (let i = 0; i < 70; i++) {
			server.withBook(`Book ${String(i).padStart(2, "0")}`);
		}
		renderApp("/");
		const books = within(await screen.findByRole("region", { name: "Books" }));
		await waitFor(() => expect(books.getAllByRole("link")).toHaveLength(60));

		await person.click(books.getByRole("button", { name: "Show more" }));
		await waitFor(() => expect(books.getAllByRole("link")).toHaveLength(70));
		expect(books.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
	});

	it("opens a book with what is known about it and its files", async () => {
		const person = userEvent.setup();
		withShelf();
		const { router } = renderApp("/");
		await person.click(await screen.findByRole("link", { name: /^Dune/ }));

		expect(await screen.findByRole("heading", { name: "Dune", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/books/book-2");
		for (const text of ["Deluxe Edition", "by Frank Herbert", "Read by Scott Brick", "Book 1 of Dune Chronicles",
			"Chilton", "1965", "21 h 2 min", "ISBN 9780441013593", "Science Fiction"]) {
			expect(screen.getByText(text)).toBeInTheDocument();
		}
		const files = within(screen.getByRole("region", { name: "Files" }));
		const download = files.getByRole("link", { name: "Download Dune 1.mp3" });
		expect(download).toHaveAttribute("href", "/api/v1/files/file-2/download");
		expect(files.getByText(/Part 2 · Not in the folder any more/)).toBeInTheDocument();
		// A file that is gone cannot be downloaded.
		expect(files.queryByRole("link", { name: "Download Dune 2.mp3" })).not.toBeInTheDocument();

		await person.click(screen.getByRole("link", { name: "Back to the library" }));
		expect(await screen.findByRole("region", { name: "Books" })).toBeInTheDocument();
	});

	it("says so when a book is not there", async () => {
		withShelf();
		renderApp("/books/book-99");
		expect(await screen.findByText("There is no such book, or it is in a library you may not see.")).toBeInTheDocument();
	});
});

describe("uploads", () => {
	const book = (name: string, content: string) => new File([content], name);

	it("adds the files an editor picks, and says which the library had already", async () => {
		const person = userEvent.setup({ applyAccept: false });
		server
			.withAccount("Edith", "a long password", "editor")
			.signedInAs("Edith")
			.withLibrary("Novels")
			// External libraries are only read, so they are not offered.
			.withLibrary("Archive", { mode: "external", writable: false });
		const { router } = renderApp("/");

		await person.click(await screen.findByRole("link", { name: "Add books" }));
		expect(await screen.findByRole("heading", { name: "Add books", level: 1 })).toBeInTheDocument();
		expect(screen.getByText("Drop files here to add them to Novels, or")).toBeInTheDocument();
		expect(screen.queryByRole("combobox", { name: "Into the library" })).not.toBeInTheDocument();

		await person.upload(screen.getByLabelText("Choose files"), [
			book("Emma.epub", "an epub"),
			book("Emma copy.epub", "an epub"),
			book("notes.txt", "not a book"),
		]);

		const emma = within(await screen.findByRole("listitem", { name: "Emma.epub" }));
		expect(await emma.findByText("Added.")).toBeInTheDocument();
		const copy = within(screen.getByRole("listitem", { name: "Emma copy.epub" }));
		expect(await copy.findByText("Already in the library as “Emma”, so not added again.")).toBeInTheDocument();
		expect(copy.getByRole("link", { name: "Open the book" })).toHaveAttribute("href", "/books/book-1");
		const notes = within(screen.getByRole("listitem", { name: "notes.txt" }));
		expect(await notes.findByText("GOtome does not read this kind of file.")).toBeInTheDocument();
		expect(server.requests.filter((r) => r.path === "/libraries/lib-1/uploads")).toHaveLength(3);

		await person.click(screen.getByRole("button", { name: "Clear the finished ones" }));
		expect(screen.queryByRole("listitem", { name: "Emma.epub" })).not.toBeInTheDocument();

		await person.click(screen.getByRole("link", { name: "Library" }));
		expect(await screen.findByRole("link", { name: /^Emma/ })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
	});

	it("takes files dropped on the page, into the library chosen", async () => {
		server
			.withAccount("Edith", "a long password", "editor")
			.signedInAs("Edith")
			.withLibrary("Novels")
			.withLibrary("Poetry");
		const person = userEvent.setup();
		renderApp("/upload");

		await person.selectOptions(await screen.findByRole("combobox", { name: "Into the library" }), "Poetry");
		fireEvent.drop(screen.getByRole("region", { name: "Files to add" }), {
			dataTransfer: { files: [book("Odes.pdf", "a pdf")] },
		});

		const odes = within(await screen.findByRole("listitem", { name: "Odes.pdf" }));
		expect(await odes.findByText("Added.")).toBeInTheDocument();
		expect(server.requests).toContainEqual({ method: "POST", path: "/libraries/lib-2/uploads", body: { file: "Odes.pdf" } });
	});

	it("says what to do when there is no library to add to", async () => {
		server.withAccount("Steve", "a long password").signedInAs("Steve").withLibrary("Archive", { mode: "external" });
		renderApp("/upload");

		expect(await screen.findByText(/there is none yet/)).toBeInTheDocument();
		expect(screen.getByRole("link", { name: "Add a library" })).toBeInTheDocument();
		expect(screen.queryByLabelText("Choose files")).not.toBeInTheDocument();
	});

	it("keeps a reader out", async () => {
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita").withLibrary("Novels");
		const { router } = renderApp("/upload");

		expect(await screen.findByRole("heading", { name: "Novels" })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
		expect(screen.queryByRole("link", { name: "Add books" })).not.toBeInTheDocument();
	});
});

describe("filters", () => {
	const titles = () =>
		within(screen.getByRole("region", { name: "Books" }))
			.getAllByRole("link")
			.map((l) => l.lastElementChild?.firstElementChild?.textContent);

	function withShelf() {
		const author = (name: string) => [{ name, role: "author" as const }];
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Emma", { contributors: author("Jane Austen"), language: "en-GB", published: "1815", tags: ["Fiction", "Classics"] })
			.withBook("Persuasion", { contributors: author("Jane Austen"), language: "en", published: "1817", tags: ["Fiction"] })
			.withBook("Jane Eyre", { contributors: author("Charlotte Brontë"), language: "en", published: "1847", tags: ["Fiction"] })
			.withBook("Der Process", { contributors: author("Franz Kafka"), language: "de", published: "1925" });
	}

	it("narrows the books by what is picked, and counts what is left", async () => {
		const person = userEvent.setup();
		withShelf();
		const { router } = renderApp("/");
		const filters = within(await screen.findByRole("complementary", { name: "Filters" }));

		await person.click(await filters.findByRole("checkbox", { name: "Jane Austen (2)" }));
		await waitFor(() => expect(titles()).toEqual(["Emma", "Persuasion"]));
		expect(router.state.location.search).toMatchObject({ author: ["jane austen"] });
		// Another author widens the list; the tags count only what is shown.
		expect(await filters.findByRole("checkbox", { name: "Classics (1)" })).toBeInTheDocument();
		expect(filters.getByRole("checkbox", { name: "Fiction (2)" })).toBeInTheDocument();
		await person.click(filters.getByRole("checkbox", { name: "Charlotte Brontë (1)" }));
		await waitFor(() => expect(titles()).toEqual(["Emma", "Jane Eyre", "Persuasion"]));

		await person.click(filters.getByRole("checkbox", { name: "1810s (2)" }));
		await waitFor(() => expect(titles()).toEqual(["Emma", "Persuasion"]));
		expect(server.requests.at(-1)?.path).toContain("filter=");

		await person.click(filters.getByRole("button", { name: "Clear the filters" }));
		await waitFor(() => expect(titles()).toHaveLength(4));
		expect(router.state.location.search).not.toHaveProperty("author");
	});

	it("opens filtered from a shared address, and says when nothing matches", async () => {
		const person = userEvent.setup();
		withShelf();
		const pick = (field: string, values: string[]) => `${field}=${encodeURIComponent(JSON.stringify(values))}`;
		const { unmount } = renderApp(`/?${pick("language", ["de"])}`);

		await waitFor(() => expect(titles()).toEqual(["Der Process"]));
		const filters = within(screen.getByRole("complementary", { name: "Filters" }));
		expect(filters.getByRole("checkbox", { name: "German (1)" })).toBeChecked();
		unmount();

		// The panel offers nothing that would leave no book, but an address may.
		renderApp(`/?${pick("language", ["de"])}&${pick("author", ["jane austen"])}`);
		expect(await screen.findByRole("heading", { name: "No book matches" })).toBeInTheDocument();
		// What was picked stays in view, to be taken back.
		const picked = within(screen.getByRole("complementary", { name: "Filters" }));
		expect(await picked.findByRole("checkbox", { name: "jane austen (0)" })).toBeChecked();
		await person.click(within(screen.getByRole("region", { name: "Books" })).getByRole("button", { name: "Clear the filters" }));
		await waitFor(() => expect(titles()).toHaveLength(4));
	});
});

describe("quick search", () => {
	function withShelf() {
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Mistborn", { contributors: [{ name: "Brandon Sanderson", role: "author" }] })
			.withBook("The Way of Kings", { contributors: [{ name: "Brandon Sanderson", role: "author" }] })
			.withBook("Emma", { contributors: [{ name: "Jane Austen", role: "author" }] });
	}

	it("finds a misspelt author and opens a book from the keyboard", async () => {
		const person = userEvent.setup();
		withShelf();
		const { router } = renderApp("/");
		await screen.findByRole("heading", { name: "Library", level: 1 });

		// "/" anywhere on the page comes to the search box.
		await person.keyboard("/");
		const box = screen.getByRole("combobox", { name: "Search books" });
		expect(box).toHaveFocus();
		await person.type(box, "Sandersen");

		const results = await screen.findByRole("listbox", { name: "Books found" });
		// Two books, and last the search of their text.
		await waitFor(() => expect(within(results).getAllByRole("option")).toHaveLength(3));
		expect(within(results).getAllByRole("option")[2]).toHaveTextContent("Search the text of the books for “Sandersen”");
		expect(box).toHaveAttribute("aria-expanded", "true");

		await person.keyboard("{ArrowDown}{ArrowDown}");
		expect(within(results).getAllByRole("option")[1]).toHaveAttribute("aria-selected", "true");
		await person.keyboard("{Enter}");
		expect(await screen.findByRole("heading", { name: "The Way of Kings", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/books/book-2");
		expect(box).toHaveValue("");
	});

	it("waits for three letters, says when nothing is found, and closes on Escape", async () => {
		const person = userEvent.setup();
		withShelf();
		renderApp("/");
		const box = await screen.findByRole("combobox", { name: "Search books" });

		await person.type(box, "em");
		await new Promise((r) => setTimeout(r, 300));
		expect(server.requests.some((r) => r.path.startsWith("/books/search"))).toBe(false);

		await person.type(box, "ma");
		const results = await screen.findByRole("listbox", { name: "Books found" });
		await waitFor(() => expect(within(results).getByRole("option", { name: /Emma/ })).toBeInTheDocument());
		await person.keyboard("{Escape}");
		expect(box).toHaveAttribute("aria-expanded", "false");

		await person.clear(box);
		await person.type(box, "qwertz");
		expect(await screen.findByText("No title, author or series looks like that.")).toBeInTheDocument();
	});
});

describe("settings", () => {
	it("lets an administrator set a token that is never shown again", async () => {
		const person = userEvent.setup();
		server.withAccount("Steve", "a long password").signedInAs("Steve");
		renderApp("/");

		await person.click(await screen.findByRole("link", { name: "Settings" }));
		expect(await screen.findByRole("heading", { name: "Settings", level: 1 })).toBeInTheDocument();
		const token = screen.getByLabelText("Hardcover API token");
		expect(token).toHaveAttribute("type", "password");
		expect(screen.getByText(/Needed to look books up on Hardcover\. Not set\./)).toBeInTheDocument();

		await person.type(token, "hc-secret");
		await person.clear(screen.getByLabelText("Language for book details"));
		await person.type(screen.getByLabelText("Language for book details"), "deutsch");
		await person.click(screen.getByRole("button", { name: "Save" }));
		expect(await screen.findByText("Give a language as its two-letter code, such as en or de.")).toBeInTheDocument();
		expect(server.secrets.size).toBe(0);

		await person.clear(screen.getByLabelText("Language for book details"));
		await person.type(screen.getByLabelText("Language for book details"), "de");
		await person.click(screen.getByRole("button", { name: "Save" }));
		expect(await screen.findByRole("status")).toHaveTextContent("Saved.");
		expect(server.secrets.get("metadata.hardcoverToken")).toBe("hc-secret");
		// The field is empty again and only says that a token is there.
		expect(screen.getByLabelText("Hardcover API token")).toHaveValue("");
		expect(screen.getByText(/Saved on/)).toBeInTheDocument();
		expect(screen.getByLabelText("Language for book details")).toHaveValue("de");
		expect(document.body.textContent).not.toContain("hc-secret");
		// Only what changed was sent: the Google key stays as it was.
		expect(server.requests.at(-1)?.body).toEqual({ values: { "metadata.hardcoverToken": "hc-secret", "metadata.language": "de" } });

		await person.click(screen.getByRole("button", { name: "Remove the Hardcover API token" }));
		await waitFor(() => expect(server.secrets.has("metadata.hardcoverToken")).toBe(false));
		expect(await screen.findByText(/Needed to look books up on Hardcover\. Not set\./)).toBeInTheDocument();
	});

	it("keeps an editor out", async () => {
		server.withAccount("Edith", "a long password", "editor").signedInAs("Edith").withLibrary("Novels");
		const { router } = renderApp("/admin/settings");

		expect(await screen.findByRole("heading", { name: "Novels" })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
		expect(screen.queryByRole("link", { name: "Settings" })).not.toBeInTheDocument();
	});
});

describe("users", () => {
	it("lets an administrator add, promote and disable accounts, but not leave none", async () => {
		const person = userEvent.setup();
		server.withAccount("Steve", "a long password").signedInAs("Steve");
		renderApp("/");

		await person.click(await screen.findByRole("link", { name: "Users" }));
		const form = within(await screen.findByRole("region", { name: "Add a user" }));
		await person.type(form.getByLabelText("User name"), "rita");
		await person.type(form.getByLabelText("Password"), "short");
		await person.click(form.getByRole("button", { name: "Add the user" }));
		expect(await form.findByText("A password has at least 8 characters.")).toBeInTheDocument();
		await person.clear(form.getByLabelText("Password"));
		await person.type(form.getByLabelText("Password"), "a long password");
		await person.click(form.getByRole("button", { name: "Add the user" }));

		const rita = within(await screen.findByRole("listitem", { name: "rita" }));
		expect(rita.getByRole("combobox", { name: "Role" })).toHaveValue("reader");
		await person.selectOptions(rita.getByRole("combobox", { name: "Role" }), "Editor");
		await waitFor(() => expect(server.accounts.get("rita")?.user.role).toBe("editor"));
		await person.click(rita.getByRole("button", { name: "Disable" }));
		expect(await rita.findByText("Disabled")).toBeInTheDocument();
		expect(server.accounts.get("rita")?.disabled).toBe(true);

		await person.click(rita.getByRole("button", { name: "Set a new password" }));
		await person.type(rita.getByLabelText("New password for rita"), "another long one");
		await person.click(rita.getByRole("button", { name: "Save the password" }));
		await waitFor(() => expect(server.accounts.get("rita")?.password).toBe("another long one"));

		// The only administrator cannot step down.
		const steve = within(screen.getByRole("listitem", { name: "Steve" }));
		expect(steve.getByText("(you)")).toBeInTheDocument();
		await person.click(steve.getByRole("button", { name: "Disable" }));
		expect(await steve.findByRole("alert")).toHaveTextContent("This is the last administrator");
	});

	it("keeps editors out", async () => {
		server.withAccount("Edith", "a long password", "editor").signedInAs("Edith").withLibrary("Novels");
		const { router } = renderApp("/admin/users");
		expect(await screen.findByRole("heading", { name: "Novels" })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
		expect(screen.queryByRole("link", { name: "Users" })).not.toBeInTheDocument();
	});
});

describe("profile", () => {
	it("changes the password and signs out another browser", async () => {
		const person = userEvent.setup();
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita");
		renderApp("/");

		await person.click(await screen.findByRole("link", { name: "Signed in as Rita" }));
		expect(await screen.findByRole("heading", { name: "Your account", level: 1 })).toBeInTheDocument();
		expect(screen.getByText("Signed in as Rita, Reader.")).toBeInTheDocument();

		const sessions = within(screen.getByRole("region", { name: "Where you are signed in" }));
		expect(await sessions.findByText("Firefox on Linux")).toBeInTheDocument();
		expect(sessions.getByText("This browser")).toBeInTheDocument();
		await person.click(sessions.getByRole("button", { name: "Sign out Safari on iPhone" }));
		await waitFor(() => expect(sessions.queryByText("Safari on iPhone")).not.toBeInTheDocument());

		const password = within(screen.getByRole("region", { name: "Password" }));
		await person.type(password.getByLabelText("Current password"), "not it at all");
		await person.type(password.getByLabelText("New password"), "a new long password");
		await person.click(password.getByRole("button", { name: "Change the password" }));
		expect(await password.findByText("That is not your current password.")).toBeInTheDocument();
		await person.clear(password.getByLabelText("Current password"));
		await person.type(password.getByLabelText("Current password"), "a long password");
		await person.click(password.getByRole("button", { name: "Change the password" }));
		expect(await password.findByRole("status")).toHaveTextContent("Changed. Other devices are signed out.");
		expect(server.accounts.get("rita")?.password).toBe("a new long password");
	});
});

describe("quotas", () => {
	it("lets an administrator set a quota, which the uploader sees and meets", async () => {
		const person = userEvent.setup();
		server
			.withAccount("Steve", "a long password")
			.withAccount("Edith", "a long password", "editor")
			.signedInAs("Steve")
			.withLibrary("Novels");
		const { unmount } = renderApp("/admin/users");

		const edith = within(await screen.findByRole("listitem", { name: "Edith" }));
		expect(edith.getByText("Uploads take up 0 B; no limit.")).toBeInTheDocument();
		await person.type(edith.getByLabelText("Upload quota in GB"), "lots");
		await person.click(edith.getByRole("button", { name: "Set the quota" }));
		expect(edith.getByText("Give a number of gigabytes, or nothing for no limit.")).toBeInTheDocument();
		await person.clear(edith.getByLabelText("Upload quota in GB"));
		await person.type(edith.getByLabelText("Upload quota in GB"), "0,00001");
		await person.click(edith.getByRole("button", { name: "Set the quota" }));
		expect(await edith.findByText("Uploads take up 0 B of 10 KB.")).toBeInTheDocument();
		expect(server.accounts.get("edith")?.quotaBytes).toBe(10_000);
		unmount();

		server.signedInAs("Edith");
		renderApp("/upload");
		expect(await screen.findByText("Your uploads take up 0 B of the 10 KB you may use.")).toBeInTheDocument();
		await person.upload(screen.getByLabelText("Choose files"), [
			new File(["x".repeat(6000)], "Small.pdf"),
			new File(["y".repeat(6000)], "Second.pdf"),
		]);
		expect(await within(await screen.findByRole("listitem", { name: "Small.pdf" })).findByText("Added.")).toBeInTheDocument();
		expect(await within(screen.getByRole("listitem", { name: "Second.pdf" })).findByText("This file does not fit into your storage.")).toBeInTheDocument();
		expect(await screen.findByText("Your uploads take up 6 KB of the 10 KB you may use.")).toBeInTheDocument();
	});
});

describe("jobs", () => {
	const job = (id: number, overrides: Partial<Job>): Job => ({
		id,
		kind: "ingest.extract_file",
		state: "available",
		attempt: 0,
		maxAttempts: 3,
		createdAt: "2026-01-02T10:00:00Z",
		scheduledAt: "2026-01-02T10:00:00Z",
		...overrides,
	});

	it("lists jobs with their errors, and tries a failed one again", async () => {
		const person = userEvent.setup();
		server.withAccount("Edith", "a long password", "editor").signedInAs("Edith").withLibrary("Novels");
		server.jobs = [
			job(3, { kind: "ingest.scan_library", libraryName: "Novels", state: "discarded", attempt: 1, maxAttempts: 5, lastError: "the library's folder cannot be read" }),
			job(2, { filePath: "Emma.epub", bookId: "book-1", state: "running", attempt: 1 }),
			job(1, { kind: "auth.sweep_sessions", state: "completed", attempt: 1 }),
		];
		renderApp("/");

		await person.click(await screen.findByRole("link", { name: "Jobs" }));
		const list = within(await screen.findByRole("list", { name: "Jobs" }));
		const scan = within(list.getByRole("listitem", { name: "Scan Novels" }));
		expect(scan.getByText("Failed")).toBeInTheDocument();
		expect(scan.getByText("the library's folder cannot be read")).toBeInTheDocument();
		expect(list.getByRole("link", { name: "Read Emma.epub" })).toHaveAttribute("href", "/books/book-1");
		expect(list.getByRole("listitem", { name: "Clear away old sessions" })).toHaveTextContent("Done");

		await person.click(scan.getByRole("button", { name: "Try again: Scan Novels" }));
		expect(await scan.findByText("Waiting")).toBeInTheDocument();
		await person.click(list.getByRole("button", { name: "Cancel: Read Emma.epub" }));
		await waitFor(() => expect(server.jobs[1]?.state).toBe("cancelled"));
	});

	it("shows a file that could not be read, and reads it again", async () => {
		const person = userEvent.setup();
		server
			.withAccount("Edith", "a long password", "editor")
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Edith")
			.withLibrary("Novels", { filesFailed: 1 })
			.withBook("Broken", {
				files: [{ id: "file-9", kind: "ebook", format: "epub", name: "Broken.epub", size: 10, missing: false, drm: false, extractState: "failed", extractError: "not a zip file" }],
			});
		const { unmount } = renderApp("/");

		const status = await screen.findByText("Files that could not be read: 1.");
		expect(status).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Read them again" }));
		await waitFor(() => expect(server.reread).toContain("lib-1"));

		await person.click(await screen.findByRole("link", { name: /^Broken/ }));
		expect(await screen.findByText("Could not be read: not a zip file")).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Read Broken.epub again" }));
		await waitFor(() => expect(server.reread).toContain("file-9"));
		unmount();

		server.signedInAs("Rita");
		const { router } = renderApp("/jobs");
		expect(await screen.findByRole("heading", { name: "Library", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
		expect(screen.queryByRole("link", { name: "Jobs" })).not.toBeInTheDocument();
	});
});

describe("editing a book", () => {
	function withBook() {
		server
			.withAccount("Edith", "a long password", "editor")
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Edith")
			.withLibrary("Novels")
			.withBook("Emma", {
				contributors: [{ name: "Jane Austen", role: "author" }],
				language: "en",
				tags: ["Fiction"],
				identifiers: [{ type: "isbn", value: "9780141439587", fromFile: true }],
				fields: {
					title: { source: "file", detail: "epub", locked: false },
					language: { source: "file", detail: "epub", locked: false },
				},
			})
			.withBook("Persuasion", { contributors: [{ name: "Jane Austenová", role: "translator" }] });
	}
	const edits = () => server.requests.filter((r) => r.method === "PATCH").map((r) => r.body);

	it("sends what was changed, which is then locked and set by hand", async () => {
		const person = userEvent.setup();
		withBook();
		const { router } = renderApp("/books/book-1");
		await person.click(await screen.findByRole("link", { name: "Edit details" }));

		const title = await screen.findByRole("textbox", { name: "Title" });
		expect(title).toHaveAccessibleDescription("From the EPUB file");
		expect(screen.getByRole("checkbox", { name: "Lock Title" })).not.toBeChecked();
		expect(screen.getByText("ISBN 9780141439587, from a file of the book")).toBeInTheDocument();

		await person.clear(title);
		await person.type(title, "Emma (Annotated)");
		expect(screen.getByRole("checkbox", { name: "Lock Title" })).toBeChecked();
		await person.type(screen.getByRole("combobox", { name: "New tag" }), "Classics{Enter}");
		await person.click(screen.getByRole("button", { name: "Add a person" }));
		await person.type(screen.getByRole("combobox", { name: "Name of person 2" }), "Fiona Stafford");
		await person.selectOptions(screen.getByRole("combobox", { name: "Role of person 2" }), "Editor");
		// Unlocked without being changed.
		await person.click(screen.getByRole("checkbox", { name: "Lock Language" }));
		await person.click(screen.getByRole("checkbox", { name: "Lock Language" }));
		await person.click(screen.getByRole("button", { name: "Save" }));

		expect(await screen.findByRole("heading", { name: "Emma (Annotated)", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/books/book-1");
		expect(edits()).toEqual([
			{
				title: "Emma (Annotated)",
				contributors: [
					{ name: "Jane Austen", role: "author" },
					{ name: "Fiona Stafford", role: "editor" },
				],
				tags: ["Fiction", "Classics"],
			},
		]);

		await person.click(screen.getByRole("link", { name: "Edit details" }));
		expect(await screen.findByRole("textbox", { name: "Title" })).toHaveAccessibleDescription("Set by hand");
		expect(screen.getByRole("checkbox", { name: "Lock Title" })).toBeChecked();
		await person.click(screen.getByRole("checkbox", { name: "Lock Title" }));
		await person.click(screen.getByRole("button", { name: "Save" }));
		await waitFor(() => expect(edits()).toHaveLength(2));
		expect(edits()[1]).toEqual({ locks: { title: false } });
	});

	it("offers names already in use and shows the server's word at its field", async () => {
		const person = userEvent.setup();
		withBook();
		renderApp("/books/book-1/edit");
		const name = await screen.findByRole("combobox", { name: "Name of person 1" });
		await person.clear(name);
		await person.type(name, "aus");
		await waitFor(() => {
			const options = [...(document.getElementById(name.getAttribute("list") ?? "")?.querySelectorAll("option") ?? [])];
			expect(options.map((o) => o.value)).toEqual(["Jane Austen", "Jane Austenová"]);
		});

		const language = screen.getByRole("textbox", { name: "Language" });
		await person.clear(language);
		await person.type(language, "not a language");
		await person.click(screen.getByRole("button", { name: "Save" }));
		expect(await screen.findByText("Give a language code such as en, de or pt-BR.")).toBeInTheDocument();
		expect(language).toHaveAttribute("aria-invalid", "true");
	});

	it("keeps readers out", async () => {
		withBook();
		server.signedInAs("Rita");
		const { router } = renderApp("/books/book-1/edit");
		expect(await screen.findByRole("heading", { name: "Library", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
		await router.navigate({ to: "/books/$bookId", params: { bookId: "book-1" } });
		expect(await screen.findByRole("heading", { name: "Emma", level: 1 })).toBeInTheDocument();
		expect(screen.queryByRole("link", { name: "Edit details" })).not.toBeInTheDocument();
	});
});

describe("finding details online", () => {
	it("compares a result with the book and takes the ticked, unlocked fields", async () => {
		const person = userEvent.setup();
		server
			.withAccount("Edith", "a long password", "editor")
			.signedInAs("Edith")
			.withLibrary("Novels")
			.withBook("Emma (my copy)", {
				contributors: [{ name: "Jane Austen", role: "author" }],
				identifiers: [{ type: "isbn", value: "9780141439587", fromFile: false }],
				fields: { title: { source: "manual", locked: true } },
			});
		server.candidates["book-1"] = {
			candidates: [
				{
					provider: "openlibrary", id: "OL1M", score: 1, title: "Emma", publisher: "Penguin", published: "2003",
					description: "A comedy of manners.", contributors: [{ name: "Jane Austen", role: "author" }], tags: [],
					identifiers: [{ type: "isbn", value: "9780141439587" }, { type: "openlibrary", value: "OL1M" }],
				},
				{
					provider: "openlibrary", id: "OL2W", score: 0.4, title: "Emma: A Play", contributors: [], tags: [], identifiers: [],
				},
			],
			failures: [{ provider: "google", message: "It asks to be asked again later." }],
		};
		const { router } = renderApp("/books/book-1");
		await person.click(await screen.findByRole("link", { name: "Find details online" }));

		expect(await screen.findByText("google did not answer: It asks to be asked again later.")).toBeInTheDocument();
		const results = screen.getByRole("list", { name: "What the sources found" });
		expect(within(results).getByText("Same ISBN")).toBeInTheDocument();
		expect(within(results).getByText("40% match")).toBeInTheDocument();
		await person.click(within(results).getAllByRole("button")[0] as HTMLElement);

		const take = (field: string) => screen.getByRole("checkbox", { name: `Take ${field}` });
		expect(take("Title")).toBeDisabled();
		expect(take("Publisher")).toBeChecked();
		await person.click(take("Description"));
		await person.click(screen.getByRole("button", { name: "Take what is ticked" }));

		expect(await screen.findByText("Penguin")).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/books/book-1");
		expect(server.requests.find((r) => r.path.endsWith("/candidates/apply"))?.body).toEqual({
			provider: "openlibrary",
			publisher: "Penguin",
			published: "2003",
			identifiers: [
				{ type: "isbn", value: "9780141439587" },
				{ type: "openlibrary", value: "OL1M" },
			],
		});
		expect(screen.getByRole("heading", { name: "Emma (my copy)", level: 1 })).toBeInTheDocument();
	});
});

describe("reviewing matches", () => {
	it("takes and rejects matches from the keyboard", async () => {
		const person = userEvent.setup();
		server
			.withAccount("Edith", "a long password", "editor")
			.signedInAs("Edith")
			.withLibrary("Novels")
			.withBook("Emma", { contributors: [{ name: "Emma Donoghue", role: "author" }] })
			.withBook("Persuasion");
		const match = (matchId: string, title: string, publisher: string) => ({
			matchId, provider: "openlibrary", id: matchId, score: 0.6, title, publisher,
			contributors: [{ name: "Jane Austen", role: "author" as const }], tags: [], identifiers: [],
		});
		server.review = [
			{ book: server.books[0] as BookDetail, matches: [match("m1", "Emma", "Penguin"), match("m2", "Emma", "John Murray")] },
			{ book: server.books[1] as BookDetail, matches: [match("m3", "Persuasion", "Penguin")] },
		];
		renderApp("/");
		await person.click(await screen.findByRole("link", { name: "Review" }));

		expect(await screen.findByText("2 books wait.")).toBeInTheDocument();
		expect(screen.getByRole("heading", { name: "Emma", level: 2 })).toBeInTheDocument();
		await person.keyboard("2");
		expect(screen.getByRole("button", { name: /^2\. Emma/ })).toHaveAttribute("aria-pressed", "true");
		await person.keyboard("a");

		expect(await screen.findByRole("heading", { name: "Persuasion", level: 2 })).toBeInTheDocument();
		expect(server.requests.find((r) => r.path === "/matches/m2/accept")?.body).toMatchObject({ publisher: "John Murray" });
		expect(server.requests.find((r) => r.path === "/matches/m2/accept")?.body).not.toHaveProperty("provider");
		await person.keyboard("r");
		expect(await screen.findByText("Nothing waits for review.")).toBeInTheDocument();
		expect(server.requests.some((r) => r.path === "/matches/m3/reject")).toBe(true);
		expect(server.books[0]?.publisher).toBe("John Murray");
	});

	it("keeps readers out", async () => {
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita");
		const { router } = renderApp("/review");
		expect(await screen.findByRole("heading", { name: "Library", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/");
		expect(screen.queryByRole("link", { name: "Review" })).not.toBeInTheDocument();
	});
});

describe("changing many books", () => {
	function withShelf() {
		server
			.withAccount("Edith", "a long password", "editor")
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Edith")
			.withLibrary("Novels")
			.withBook("Mort", { series: "Discworl", tags: ["Fantasy"] })
			.withBook("Eric", { series: "Discworl", fields: { series: { source: "manual", locked: true } } })
			.withBook("Emma");
	}
	const bulkRequests = () => server.requests.filter((r) => r.path === "/books/bulk").map((r) => r.body);

	it("edits every book that matches the filter, and reports the locked ones", async () => {
		const person = userEvent.setup();
		withShelf();
		const { router } = renderApp("/?series=discworl");
		await person.click(await screen.findByRole("button", { name: "Select books" }));
		await person.click(screen.getByRole("button", { name: "Select every book that matches" }));
		expect(screen.getByText("Every book that matches is selected")).toBeInTheDocument();
		expect(await screen.findByRole("checkbox", { name: "Select Mort" })).toBeChecked();

		await person.click(screen.getByRole("button", { name: "Edit" }));
		await person.click(screen.getByRole("button", { name: "Change the books" }));
		expect(screen.getByRole("alert")).toHaveTextContent("Choose at least one field to change.");
		await person.selectOptions(screen.getByRole("combobox", { name: "What to do with Series" }), "Set to");
		await person.type(screen.getByRole("textbox", { name: "Value for Series" }), "Discworld");
		await person.selectOptions(screen.getByRole("combobox", { name: "What to do with Tags" }), "Add");
		await person.type(screen.getByRole("textbox", { name: "Value for Tags" }), "Humour, Satire,");
		await person.click(screen.getByRole("button", { name: "Change the books" }));

		expect(await screen.findByRole("heading", { name: "Editing many books", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/bulk/bulk-1");
		expect(bulkRequests()).toEqual([
			{
				library: "lib-1",
				filter: JSON.stringify({ all: [{ field: "series", op: "in", values: ["discworl"] }] }),
				action: "edit",
				change: { series: "Discworld", addTags: ["Humour", "Satire"] },
			},
		]);
		expect(screen.getByText("2 of 2 books done.")).toBeInTheDocument();
		const eric = screen.getByRole("link", { name: "Eric" }).closest("li") as HTMLElement;
		expect(within(eric).getByText("Locked, left as it was: Series")).toBeInTheDocument();
		expect(within(eric).getByText("Changed")).toBeInTheDocument();
		expect(server.books[0]?.series).toBe("Discworld");
		expect(server.books[1]?.series).toBe("Discworl");
		expect(server.books[2]?.tags).toEqual([]);

		await person.click(screen.getByRole("button", { name: "Changed: 2" }));
		expect(screen.getAllByRole("listitem")).toHaveLength(2);
	});

	it("looks up the books ticked", async () => {
		const person = userEvent.setup();
		withShelf();
		renderApp("/");
		await person.click(await screen.findByRole("button", { name: "Select books" }));
		await person.click(await screen.findByRole("checkbox", { name: "Select Emma" }));
		await person.click(screen.getByRole("checkbox", { name: "Select Mort" }));
		await person.click(screen.getByRole("checkbox", { name: "Select Mort" }));
		expect(screen.getByText("1 selected")).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Fetch details" }));

		expect(await screen.findByRole("heading", { name: "Fetching details for many books", level: 1 })).toBeInTheDocument();
		expect(bulkRequests()).toEqual([{ books: ["book-3"], action: "fetch" }]);
	});

	it("lets a reader set their status on many books, but not edit them", async () => {
		const person = userEvent.setup();
		withShelf();
		server.signedInAs("Rita");
		renderApp("/");
		await person.click(await screen.findByRole("button", { name: "Select books" }));
		await person.click(screen.getByRole("button", { name: "Select every book that matches" }));
		expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Fetch details" })).not.toBeInTheDocument();
		await person.selectOptions(screen.getByRole("combobox", { name: "Set my status" }), "Wishlist");

		expect(await screen.findByText("Status set on 3 books.")).toBeInTheDocument();
		expect(server.requests.find((r) => r.path === "/books/reading")?.body).toEqual({ library: "lib-1", status: "wishlist" });
		expect(server.books.map((b) => b.reading.status)).toEqual(["wishlist", "wishlist", "wishlist"]);
	});
});

describe("reading state", () => {
	it("sets status and rating on a book, shows them in the list and filters by them", async () => {
		const person = userEvent.setup();
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Emma")
			.withBook("Persuasion");
		const { router } = renderApp("/books/book-1");

		await person.selectOptions(await screen.findByRole("combobox", { name: "Your status" }), "Reading");
		await waitFor(() => expect(server.books[0]?.reading.status).toBe("reading"));
		await person.click(screen.getByRole("button", { name: "4 of 5 stars" }));
		await waitFor(() => expect(screen.getByRole("button", { name: "4 of 5 stars" })).toHaveAttribute("aria-pressed", "true"));
		expect(server.requests.filter((r) => r.method === "PUT").map((r) => r.body)).toEqual([{ status: "reading" }, { rating: 4 }]);
		// The same star again takes the rating away.
		await person.click(screen.getByRole("button", { name: "4 of 5 stars" }));
		await waitFor(() => expect(server.books[0]?.reading.rating).toBeUndefined());
		await person.click(screen.getByRole("button", { name: "5 of 5 stars" }));

		await person.click(screen.getByRole("link", { name: "Back to the library" }));
		const emma = (await screen.findByRole("link", { name: /^Emma/ })) as HTMLElement;
		expect(within(emma).getByText("Reading")).toBeInTheDocument();
		expect(within(emma).getByRole("img", { name: "5 of 5 stars" })).toBeInTheDocument();

		const filters = within(screen.getByRole("complementary", { name: "Filters" }));
		expect(await filters.findByRole("checkbox", { name: "5 of 5 stars (1)" })).toBeInTheDocument();
		await person.click(await filters.findByRole("checkbox", { name: "Unread (1)" }));
		await waitFor(() => expect(screen.queryByRole("link", { name: /^Emma/ })).not.toBeInTheDocument());
		expect(screen.getByRole("link", { name: /^Persuasion/ })).toBeInTheDocument();
		expect(router.state.location.search).toMatchObject({ status: ["unread"] });
	});
});

describe("reading progress", () => {
	it("shows how far the person read and listened, and how often to the end", async () => {
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Emma");
		const at = { clientId: "phone", updatedAt: "2026-01-02T00:00:00Z" };
		server.progress["book-1"] = {
			ebook: { ...at, locator: "epubcfi(/6/8)", fraction: 0.42, chapter: "Volume II" },
			audio: { ...at, locator: "5400000", fraction: 0.8, positionMs: 5_400_000 },
			finishes: 2,
		};
		renderApp("/books/book-1");
		expect(await screen.findByText("Read 42% · Volume II")).toBeInTheDocument();
		expect(screen.getByText(/^Listened to 80%, at /)).toBeInTheDocument();
		expect(screen.getByText("Read to the end 2 times")).toBeInTheDocument();
	});
});

describe("the PDF reader", () => {
	function withPdf() {
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Emma", {
				files: [{ id: "file-1", kind: "ebook", format: "pdf", name: "Emma.pdf", size: 2_000_000, missing: false, drm: false, extractState: "done" }],
			});
	}
	const page = () => screen.getByRole("textbox", { name: "Page number" });
	const saves = () => server.requests.filter((r) => r.method === "PUT" && r.path.includes("/progress/")).map((r) => r.body);

	it("opens where the person left off and keeps the page they turn to", async () => {
		const person = userEvent.setup();
		withPdf();
		server.progress["book-1"] = {
			ebook: { fileId: "file-1", locator: "page:4", fraction: 0.4, page: 4, clientId: "phone", updatedAt: "2026-01-02T00:00:00Z" },
			finishes: 0,
		};
		const { router } = renderApp("/books/book-1");
		await person.click(await screen.findByRole("link", { name: "Read Emma.pdf" }));
		expect(router.state.location.pathname).toBe("/books/book-1/read/file-1");
		expect(await screen.findByRole("figure", { name: "Page 4 of Emma" })).toBeInTheDocument();
		expect(page()).toHaveValue("4");
		expect(screen.getByText("of 10")).toBeInTheDocument();

		await person.click(screen.getByRole("button", { name: "Next page" }));
		await waitFor(() => expect(saves()).toHaveLength(1));
		expect(saves()[0]).toMatchObject({
			fileId: "file-1", locator: "page:5", page: 5, fraction: 0.5, basedOn: "2026-01-02T00:00:00Z",
		});

		await person.keyboard("{End}");
		expect(page()).toHaveValue("10");
		await person.clear(page());
		await person.type(page(), "2{Enter}");
		expect(screen.getByRole("figure", { name: "Page 2 of Emma" })).toBeInTheDocument();

		await person.click(screen.getByRole("button", { name: "Contents" }));
		const contents = within(screen.getByRole("navigation", { name: "Contents" }));
		await person.click(await contents.findByRole("button", { name: "Chapter Two" }));
		expect(page()).toHaveValue("6");
		expect(contents.getByText("Nowhere")).toBeInTheDocument();
		expect(contents.queryByRole("button", { name: "Nowhere" })).not.toBeInTheDocument();

		await person.click(screen.getByRole("button", { name: "Zoom in" }));
		expect(screen.getByRole("button", { name: "125%" })).toBeInTheDocument();
		await waitFor(() => expect(saves().at(-1)).toMatchObject({ locator: "page:6" }));
	});

	it("offers the further place another device saved", async () => {
		const person = userEvent.setup();
		withPdf();
		server.furtherProgress = { fileId: "file-1", locator: "page:8", fraction: 0.8, page: 8, clientId: "tablet", updatedAt: "2026-01-02T10:00:00Z" };
		renderApp("/books/book-1/read/file-1");
		expect(await screen.findByRole("figure", { name: "Page 1 of Emma" })).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Next page" }));

		expect(await screen.findByText("Another device is further on, at page 8.")).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Go there" }));
		expect(page()).toHaveValue("8");

		await person.click(screen.getByRole("button", { name: "Previous page" }));
		expect(await screen.findByText("Another device is further on, at page 8.")).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Stay here" }));
		await waitFor(() => expect(saves().at(-1)).toMatchObject({ locator: "page:7", force: true }));
		expect(saves().at(-2)).toMatchObject({ locator: "page:7", basedOn: "2026-01-02T10:00:00Z" });
	});
});

describe("the audiobook player", () => {
	const timeline = (format: "mp3" | "m4b") => ({
		durationMs: 110_000,
		complete: true,
		parts: [
			{ fileId: "file-a", format, mediaType: format === "mp3" ? "audio/mpeg" : "audio/mp4", name: `Emma 1.${format}`, startMs: 0, durationMs: 60_000 },
			{ fileId: "file-b", format, mediaType: format === "mp3" ? "audio/mpeg" : "audio/mp4", name: `Emma 2.${format}`, startMs: 60_000, durationMs: 50_000 },
		],
		chapters: [
			{ title: "Volume I", fileId: "file-a", startMs: 0, endMs: 30_000 },
			{ title: "Volume II", fileId: "file-a", startMs: 30_000, endMs: 60_000 },
			{ title: "Volume III", fileId: "file-b", startMs: 60_000, endMs: 110_000 },
		],
	});
	function withAudiobook(format: "mp3" | "m4b") {
		const file = (id: string, name: string) => ({ id, kind: "audio", format, name, size: 1_000_000, missing: false, drm: false, extractState: "done" });
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Emma", { files: [file("file-a", `Emma 1.${format}`), file("file-b", `Emma 2.${format}`)] });
		server.audio["book-1"] = timeline(format);
	}
	// The test browser has media elements that cannot play; these stand in.
	function playable(answer: string) {
		vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockReturnValue(answer as CanPlayTypeResult);
		vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(function (this: HTMLMediaElement) {
			this.dispatchEvent(new Event("loadedmetadata"));
		});
		vi.spyOn(HTMLMediaElement.prototype, "play").mockImplementation(async function (this: HTMLMediaElement) {
			this.dispatchEvent(new Event("play"));
		});
		vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(function (this: HTMLMediaElement) {
			this.dispatchEvent(new Event("pause"));
		});
	}
	const audio = () => document.querySelector("audio") as HTMLAudioElement;
	const saves = () => server.requests.filter((r) => r.method === "PUT" && r.path.includes("/progress/audio")).map((r) => r.body);

	it("plays a book in parts as one, from where it was left, and goes on across pages", async () => {
		const person = userEvent.setup();
		playable("maybe");
		withAudiobook("mp3");
		server.progress["book-1"] = {
			audio: { fileId: "file-b", locator: "75000", fraction: 0.68, positionMs: 75_000, clientId: "phone", updatedAt: "2026-01-02T00:00:00Z" },
			finishes: 0,
		};
		const { router } = renderApp("/books/book-1");
		await person.click(await screen.findByRole("link", { name: "Listen" }));
		expect(router.state.location.pathname).toBe("/books/book-1/listen");

		expect(await screen.findByTestId("position")).toHaveTextContent("1:15");
		expect(audio().getAttribute("src")).toContain("/files/file-b/");
		expect(screen.getByRole("button", { name: /^Volume III/ })).toHaveAttribute("aria-current", "true");

		await person.click(screen.getByRole("button", { name: "Play" }));
		expect(await screen.findByRole("button", { name: "Pause" })).toBeInTheDocument();
		// Back across the start of the second part, into the first.
		await person.click(screen.getByRole("button", { name: /^Volume II(?!I)/ }));
		expect(audio().getAttribute("src")).toContain("/files/file-a/");
		expect(screen.getByTestId("position")).toHaveTextContent("0:30");
		await waitFor(() =>
			expect(saves().at(-1)).toMatchObject({ fileId: "file-a", positionMs: 30_000, locator: "30000", chapter: "Volume II" }),
		);
		expect(saves().at(-1)).toMatchObject({ basedOn: "2026-01-02T00:00:00Z" });

		await person.selectOptions(screen.getByRole("combobox", { name: "Speed" }), "1.5×");
		expect(audio().playbackRate).toBe(1.5);

		// Elsewhere in the app the book plays on, in the bar along the bottom.
		await person.click(screen.getByRole("link", { name: "Back to the book" }));
		const bar = within(await screen.findByRole("region", { name: "Now playing" }));
		expect(bar.getByRole("link", { name: "Emma" })).toBeInTheDocument();
		expect(bar.getByText("Volume II · 0:30")).toBeInTheDocument();
		await person.click(bar.getByRole("button", { name: "Pause" }));
		expect(bar.getByRole("button", { name: "Play" })).toBeInTheDocument();
		await person.click(bar.getByRole("button", { name: "Close the player" }));
		expect(screen.queryByRole("region", { name: "Now playing" })).not.toBeInTheDocument();
	});

	it("says which formats this browser cannot play, and offers the files", async () => {
		const person = userEvent.setup();
		playable("");
		withAudiobook("m4b");
		renderApp("/books/book-1");
		await person.click(await screen.findByRole("link", { name: "Listen" }));
		expect(await screen.findByRole("alert")).toHaveTextContent("This browser cannot play M4B files.");
		expect(screen.getByRole("link", { name: "Download Emma 1.m4b" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Play" })).toBeDisabled();
		expect(audio().getAttribute("src")).toBeNull();
	});
});

describe("reading statistics", () => {
	it("shows the person's own figures, years with re-reads, and their history", async () => {
		const person = userEvent.setup();
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita").withLibrary("Novels").withBook("Emma");
		server.stats = {
			days: [
				{ date: "2026-10-03", pages: 100, readingMinutes: 40, listeningMinutes: 0 },
				{ date: "2026-10-04", pages: 30, readingMinutes: 20, listeningMinutes: 30 },
			],
			pagesPerDay: 4.3, readingMinutesPerDay: 2.2, listeningMinutesPerDay: 1,
			totalPages: 160, totalReadingMinutes: 75, totalListeningMinutes: 30, pagesEstimated: true,
			years: [{ year: 2026, finishes: 1, books: 1 }, { year: 2025, finishes: 2, books: 1 }],
			history: [{ bookId: "book-1", title: "Emma", event: "finished", date: "2025-11-01" }],
		};
		renderApp("/");
		await person.click(await screen.findByRole("link", { name: "Statistics" }));

		expect(await screen.findByRole("heading", { name: "Your reading", level: 1 })).toBeInTheDocument();
		const figure = (label: string) => screen.getByText(label).nextElementSibling?.textContent;
		expect(figure("Pages a day")).toBe("≈ 4.3");
		expect(figure("Reading in all")).toBe("1 h 15 min");
		expect(figure("Listening a day")).toBe("1 min");
		expect(screen.getByText(/estimated from the length of their text/)).toBeInTheDocument();
		expect(screen.getByText(/2 finished, 1 different books/)).toBeInTheDocument();
		expect(screen.getByRole("link", { name: "Emma" })).toHaveAttribute("href", "/books/book-1");
		expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(3);

		await person.click(screen.getByRole("button", { name: "7 days" }));
		await waitFor(() => expect(server.requests.at(-1)?.path).toMatch(/^\/me\/stats\?days=7&tz=/));
	});
});

describe("the wishlist", () => {
	it("finds a book with the providers, wishes for it, and keeps it out of the library", async () => {
		const person = userEvent.setup();
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita").withLibrary("Novels").withBook("Persuasion");
		server.candidates.search = {
			candidates: [
				{
					provider: "openlibrary", id: "OL1", score: 0.9, title: "Emma", publisher: "John Murray", published: "1815",
					contributors: [{ name: "Jane Austen", role: "author" }], tags: [], identifiers: [{ type: "isbn", value: "9780141439587" }],
				},
			],
			failures: [],
		};
		const { router } = renderApp("/");
		await person.click(await screen.findByRole("link", { name: "Wishlist" }));
		expect(await screen.findByText("You wish for nothing yet.")).toBeInTheDocument();

		await person.type(screen.getByRole("textbox", { name: "Title" }), "Emma");
		await person.click(screen.getByRole("button", { name: "Look it up" }));
		expect(server.requests.at(-1)?.path).toBe("/metadata/search?title=Emma");
		await person.click(await screen.findByRole("button", { name: "Wish for Emma" }));

		expect(await screen.findByText(/^Not in the library yet\. It is wished for/)).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/books/book-2");
		expect(server.requests.find((r) => r.path === "/books/wishes")?.body).toMatchObject({
			library: "lib-1", provider: "openlibrary", title: "Emma", publisher: "John Murray",
			identifiers: [{ type: "isbn", value: "9780141439587" }],
		});

		await person.click(screen.getByRole("link", { name: "Library" }));
		expect(await screen.findByRole("link", { name: /^Persuasion/ })).toBeInTheDocument();
		expect(screen.queryByRole("link", { name: /^Emma/ })).not.toBeInTheDocument();
		await person.click(screen.getByRole("link", { name: "Wishlist" }));
		const wished = within(await screen.findByRole("region", { name: "Wished for" }));
		expect(await wished.findByText("Not in the library yet")).toBeInTheDocument();
	});
});

describe("collections", () => {
	function withShelf() {
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Mort")
			.withBook("Eric")
			.withBook("Emma");
	}
	const titles = () =>
		within(screen.getByRole("list", { name: "Books in this collection" }))
			.getAllByRole("button", { name: /^Move .+ up$/ })
			.map((b) => b.getAttribute("aria-label")?.slice("Move ".length, -" up".length));

	it("makes a collection, fills it from a book's page and a selection, and orders it", async () => {
		const person = userEvent.setup();
		withShelf();
		const { router } = renderApp("/");
		await person.click(await screen.findByRole("link", { name: "Collections" }));
		expect(await screen.findByText("You have no collections yet.")).toBeInTheDocument();
		await person.type(screen.getByRole("textbox", { name: "Name" }), "Favourites");
		await person.selectOptions(screen.getByRole("combobox", { name: "Who sees it" }), "Everyone signed in");
		await person.click(screen.getByRole("button", { name: "Create" }));
		expect(await screen.findByRole("heading", { name: "Favourites", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/collections/col-1");
		expect(screen.getByText(/^No books yet/)).toBeInTheDocument();
		expect(server.requests.find((r) => r.path === "/collections" && r.method === "POST")?.body).toEqual({
			name: "Favourites",
			visibility: "shared",
		});

		await router.navigate({ to: "/books/$bookId", params: { bookId: "book-3" } });
		await person.click(await screen.findByText("Your collections holding it: 0"));
		await person.click(await screen.findByRole("checkbox", { name: "Favourites" }));
		expect(await screen.findByText("Your collections holding it: 1")).toBeInTheDocument();
		expect(server.collections[0]?.books).toEqual(["book-3"]);

		await person.click(screen.getByRole("link", { name: "Library" }));
		await person.click(await screen.findByRole("button", { name: "Select books" }));
		await person.click(screen.getByRole("button", { name: "Select every book that matches" }));
		await person.selectOptions(await screen.findByRole("combobox", { name: "Add to collection" }), "Favourites");
		expect(await screen.findByText("Added to Favourites: 2 not in it before.")).toBeInTheDocument();
		expect(server.requests.filter((r) => r.path === "/collections/col-1/books").map((r) => r.body)).toEqual([
			{ books: ["book-3"] },
			{ library: "lib-1" },
		]);

		await router.navigate({ to: "/collections/$collectionId", params: { collectionId: "col-1" } });
		await screen.findByRole("button", { name: "Move Eric up" });
		expect(titles()).toEqual(["Emma", "Mort", "Eric"]);
		await person.click(screen.getByRole("button", { name: "Move Eric up" }));
		await waitFor(() => expect(titles()).toEqual(["Emma", "Eric", "Mort"]));
		expect(server.collections[0]?.books).toEqual(["book-3", "book-2", "book-1"]);
		expect(screen.getByRole("button", { name: "Move Emma up" })).toBeDisabled();
		await person.click(screen.getByRole("button", { name: "Take Emma out of the collection" }));
		await waitFor(() => expect(titles()).toEqual(["Eric", "Mort"]));
	});

	it("shows another person's shared collection without letting it be changed", async () => {
		withShelf();
		server.withCollection("Classics", ["book-3", "book-1"], { mine: false, ownerName: "Edith", visibility: "shared" });
		renderApp("/collections");
		const shared = within(await screen.findByRole("region", { name: "Shared by others" }));
		expect(shared.getByRole("link", { name: /^Classics/ })).toHaveTextContent("Books: 2 · Shared by Edith");
		await userEvent.setup().click(shared.getByRole("link", { name: /^Classics/ }));
		expect(await screen.findByRole("link", { name: /^Emma/ })).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Edit collection" })).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: /^Move/ })).not.toBeInTheDocument();
	});

	it("renames a collection and deletes it", async () => {
		const person = userEvent.setup();
		withShelf();
		server.withCollection("Notes", ["book-1"]);
		const { router } = renderApp("/collections/col-1");
		await person.click(await screen.findByRole("button", { name: "Edit collection" }));
		const name = screen.getByRole("textbox", { name: "Name" });
		await person.clear(name);
		await person.type(name, "To read");
		await person.click(screen.getByRole("button", { name: "Save" }));
		expect(await screen.findByRole("heading", { name: "To read", level: 1 })).toBeInTheDocument();

		await person.click(screen.getByRole("button", { name: "Edit collection" }));
		await person.click(screen.getByRole("button", { name: "Delete collection" }));
		await person.click(screen.getByRole("button", { name: "Yes, delete" }));
		expect(await screen.findByText("You have no collections yet.")).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/collections");
		expect(server.books).toHaveLength(3);
	});
});

describe("smart shelves", () => {
	function withShelf() {
		const by = (name: string) => ({ contributors: [{ name, role: "author" as const }] });
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Mistborn", { ...by("Brandon Sanderson"), reading: { status: "unread", rating: 5 } })
			.withBook("Elantris", { ...by("Brandon Sanderson"), reading: { status: "completed", rating: 4 } })
			.withBook("Emma", { ...by("Jane Austen"), reading: { status: "unread", rating: 5 } });
	}

	it("builds a shelf from rules with a live count, and what is on it follows the person's status", async () => {
		const person = userEvent.setup();
		withShelf();
		const { router } = renderApp("/collections");
		expect(await screen.findByText(/^No smart shelves yet/)).toBeInTheDocument();
		await person.click(screen.getByRole("link", { name: "New smart shelf" }));
		await person.type(await screen.findByRole("textbox", { name: "Name" }), "Next Sanderson");

		const first = within(screen.getByRole("group", { name: "Rule 1 of Rules" }));
		await person.type(first.getByRole("textbox", { name: "Values" }), "Brandon Sanderson");
		expect(await screen.findByText("Books matching now: 2")).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Add a rule" }));
		const second = within(screen.getByRole("group", { name: "Rule 2 of Rules" }));
		await person.selectOptions(second.getByRole("combobox", { name: "Field" }), "My status");
		await person.click(second.getByRole("checkbox", { name: "Unread" }));
		expect(await screen.findByText("Books matching now: 1")).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Create" }));

		expect(await screen.findByRole("heading", { name: "Next Sanderson", level: 1 })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/smart-shelves/smart-1");
		expect(server.requests.find((r) => r.path === "/smart-shelves" && r.method === "POST")?.body).toEqual({
			name: "Next Sanderson",
			visibility: "private",
			filter: JSON.stringify({
				all: [
					{ field: "author", op: "in", values: ["Brandon Sanderson"] },
					{ field: "status", op: "in", values: ["unread"] },
				],
			}),
		});
		const books = within(screen.getByRole("list", { name: "Books on this shelf" }));
		expect(await books.findByRole("link", { name: /^Mistborn/ })).toBeInTheDocument();
		expect(books.getAllByRole("link")).toHaveLength(1);

		await person.click(books.getByRole("link", { name: /^Mistborn/ }));
		await person.selectOptions(await screen.findByRole("combobox", { name: "Your status" }), "Completed");
		await waitFor(() => expect(server.books[0]?.reading.status).toBe("completed"));
		router.history.back();
		expect(await screen.findByText("No book you may see matches these rules now.")).toBeInTheDocument();
	});

	it("edits the rules of a shelf, with groups and rules turned round", async () => {
		const person = userEvent.setup();
		withShelf();
		server.withSmartShelf("Sanderson", { field: "author", op: "in", values: ["Brandon Sanderson"] });
		renderApp("/smart-shelves/smart-1");
		await person.click(await screen.findByRole("button", { name: "Edit smart shelf" }));
		expect(within(screen.getByRole("group", { name: "Rule 1 of Rules" })).getByRole("textbox", { name: "Values" })).toHaveValue(
			"Brandon Sanderson",
		);
		await person.selectOptions(screen.getByRole("combobox", { name: "Books that match" }), "any rule");
		await person.click(screen.getByRole("button", { name: "Add a group" }));
		const group = within(screen.getByRole("group", { name: "Group 2 of Rules" }));
		await person.click(group.getByRole("button", { name: "Add a rule" }));
		const rule = within(screen.getByRole("group", { name: "Rule 1 of Group 2 of Rules" }));
		await person.selectOptions(rule.getByRole("combobox", { name: "Field" }), "My rating");
		await person.selectOptions(rule.getByRole("combobox", { name: "Comparison" }), "is between");
		await person.type(rule.getByRole("textbox", { name: "From" }), "5");
		await person.click(rule.getByRole("checkbox", { name: "Not" }));
		expect(await screen.findByText("Books matching now: 2")).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Save" }));

		await waitFor(() =>
			expect(JSON.parse(server.smartShelves[0]?.filter ?? "")).toEqual({
				any: [
					{ field: "author", op: "in", values: ["Brandon Sanderson"] },
					{ all: [{ not: { field: "rating", op: "between", values: ["5", ""] } }] },
				],
			}),
		);
		expect(await screen.findByRole("button", { name: "Edit smart shelf" })).toBeInTheDocument();
	});

	it("shows another person's shared shelf as it matches for the viewer, without letting it be changed", async () => {
		withShelf();
		server.withSmartShelf(
			"Five stars",
			{ field: "rating", op: "in", values: ["5"] },
			{ mine: false, ownerName: "Edith", visibility: "shared" },
		);
		renderApp("/collections");
		const shelves = within(await screen.findByRole("region", { name: "Smart shelves" }));
		expect(await shelves.findByRole("link", { name: /^Five stars/ })).toHaveTextContent("Books: 2 · Shared by Edith");
		await userEvent.setup().click(shelves.getByRole("link", { name: /^Five stars/ }));
		expect(await screen.findByRole("link", { name: /^Emma/ })).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Edit smart shelf" })).not.toBeInTheDocument();
	});
});

describe("the EPUB and MOBI reader", () => {
	function withBook(format: string, name: string) {
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Persuasion", {
				files: [{ id: "file-1", kind: "ebook", format, name, size: 200_000, missing: false, drm: false, extractState: "done" }],
			});
	}
	const saves = () => server.requests.filter((r) => r.method === "PUT" && r.path.includes("/progress/")).map((r) => r.body);

	it("opens an EPUB where the person left off, turns pages, and keeps the place", async () => {
		const person = userEvent.setup();
		withBook("epub", "Persuasion.epub");
		server.progress["book-1"] = {
			ebook: { fileId: "file-1", locator: "epubcfi(/6/3)", fraction: 0.3, chapter: "One", clientId: "phone", updatedAt: "2026-01-02T00:00:00Z" },
			finishes: 0,
		};
		const { router } = renderApp("/books/book-1");
		await person.click(await screen.findByRole("link", { name: "Read Persuasion.epub" }));
		expect(router.state.location.pathname).toBe("/books/book-1/read/file-1");
		expect(await screen.findByText("30%")).toBeInTheDocument();
		expect(ebook.opened).toEqual([{ name: "Persuasion.epub", start: "epubcfi(/6/3)" }]);
		expect(server.requests.some((r) => r.path === "/files/file-1/download")).toBe(true);

		await person.click(screen.getByRole("button", { name: "Page right" }));
		expect(await screen.findByText("40%")).toBeInTheDocument();
		expect(screen.getByRole("region", { name: "Pages of Persuasion" })).toHaveAttribute("data-chapter", "Two");
		await waitFor(() => expect(saves()).toHaveLength(1));
		expect(saves()[0]).toMatchObject({
			fileId: "file-1", locator: "epubcfi(/6/4)", fraction: 0.4, chapter: "Two", basedOn: "2026-01-02T00:00:00Z",
		});

		await person.keyboard("{ArrowRight}{ArrowRight}");
		expect(await screen.findByText("60%")).toBeInTheDocument();
		await person.keyboard("{ArrowLeft}");
		expect(await screen.findByText("50%")).toBeInTheDocument();

		await person.click(screen.getByRole("button", { name: "Contents" }));
		const contents = within(screen.getByRole("navigation", { name: "Contents" }));
		expect(contents.getByRole("button", { name: "Two" })).toHaveAttribute("aria-current", "location");
		await person.click(contents.getByRole("button", { name: "Three" }));
		expect(await screen.findByText("80%")).toBeInTheDocument();
		expect(screen.queryByRole("navigation", { name: "Contents" })).not.toBeInTheDocument();
		await waitFor(() => expect(saves().at(-1)).toMatchObject({ locator: "epubcfi(/6/8)", chapter: "Three" }));
	});

	it("sets colours, text size and layout, and keeps them for the next book", async () => {
		const person = userEvent.setup();
		withBook("mobi", "Persuasion.mobi");
		renderApp("/books/book-1/read/file-1");
		expect(await screen.findByText("0%")).toBeInTheDocument();
		expect(ebook.opened).toEqual([{ name: "Persuasion.mobi", start: undefined }]);
		await person.click(screen.getByRole("button", { name: "Display" }));
		await person.selectOptions(screen.getByRole("combobox", { name: "Page colours" }), "Sepia");
		await person.selectOptions(screen.getByRole("combobox", { name: "Layout" }), "Scrolling");
		await person.click(screen.getByRole("button", { name: "Larger" }));
		expect(screen.getByText("Text size 110%")).toBeInTheDocument();
		await waitFor(() => expect(ebook.looks.at(-1)).toEqual({ flow: "scrolled", theme: "sepia", fontSize: 110 }));
		expect(JSON.parse(localStorage.getItem("gotome.reader.look") ?? "{}")).toEqual({ flow: "scrolled", theme: "sepia", fontSize: 110 });
	});

	it("reaching the end saves the whole book as read", async () => {
		const person = userEvent.setup();
		withBook("epub", "Persuasion.epub");
		server.progress["book-1"] = {
			ebook: { fileId: "file-1", locator: "epubcfi(/6/9)", fraction: 0.9, clientId: "phone", updatedAt: "2026-01-02T00:00:00Z" },
			finishes: 0,
		};
		renderApp("/books/book-1/read/file-1");
		expect(await screen.findByText("90%")).toBeInTheDocument();
		await person.click(screen.getByRole("button", { name: "Page right" }));
		expect(await screen.findByText("100%")).toBeInTheDocument();
		await waitFor(() => expect(saves().at(-1)).toMatchObject({ locator: "epubcfi(/6/10)", fraction: 1 }));
	});

	it("says when a file cannot be read in the browser", async () => {
		withBook("fb2", "Persuasion.fb2");
		renderApp("/books/book-1/read/file-1");
		expect(await screen.findByText(/^This file cannot be read in the browser/)).toBeInTheDocument();
		expect(ebook.opened).toEqual([]);
	});
});

describe("notifications", () => {
	/** An EventSource the test pushes events through, as the server's stream would. */
	class FakeEventSource {
		static opened: FakeEventSource[] = [];
		listeners = new Map<string, ((e: MessageEvent) => void)[]>();
		closed = false;
		constructor(readonly url: string) {
			FakeEventSource.opened.push(this);
		}
		addEventListener(type: string, listener: (e: MessageEvent) => void) {
			this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
		}
		close() {
			this.closed = true;
		}
		emit(type: string, data: string) {
			for (const listener of this.listeners.get(type) ?? []) listener(new MessageEvent(type, { data }));
		}
	}

	beforeEach(() => {
		FakeEventSource.opened = [];
		vi.stubGlobal("EventSource", FakeEventSource);
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita").withLibrary("Novels");
	});
	afterEach(() => vi.unstubAllGlobals());

	it("shows a notification the stream announces, without a reload, and marks it read", async () => {
		const person = userEvent.setup();
		renderApp("/");
		expect(await screen.findByRole("button", { name: "Notifications, 0 unread" })).toBeInTheDocument();
		const stream = FakeEventSource.opened.at(-1);
		expect(stream?.url).toBe("/api/v1/notifications/stream");

		server.notify();
		stream?.emit("unread", '{"unread":1}');
		const bell = await screen.findByRole("button", { name: "Notifications, 1 unread" });
		await person.click(bell);
		const panel = within(screen.getByRole("region", { name: "Notifications" }));
		expect(panel.getByRole("button", { name: /^Unread: Editing many books is done/ })).toHaveTextContent(
			"Changed: 2 · Failed: 1",
		);
		await person.click(panel.getByRole("button", { name: "Mark all as read" }));
		expect(await screen.findByRole("button", { name: "Notifications, 0 unread" })).toBeInTheDocument();
		expect(server.requests.find((r) => r.path === "/notifications/read")?.body).toEqual({ all: true });
		expect(panel.getByRole("button", { name: /^Editing many books is done/ })).toBeInTheDocument();

		await person.keyboard("{Escape}");
		expect(screen.queryByRole("region", { name: "Notifications" })).not.toBeInTheDocument();
	});

	it("goes where a notification points, marking that one read", async () => {
		const person = userEvent.setup();
		server.notify({ id: "note-old", readAt: "2026-01-01T00:00:00Z" }).notify({ link: "/collections" });
		const { router } = renderApp("/");
		await person.click(await screen.findByRole("button", { name: "Notifications, 1 unread" }));
		await person.click(screen.getByRole("button", { name: /^Unread: Editing many books is done/ }));
		await waitFor(() => expect(router.state.location.pathname).toBe("/collections"));
		expect(server.requests.find((r) => r.path === "/notifications/read")?.body).toEqual({ ids: ["note-2"] });
		expect(await screen.findByRole("button", { name: "Notifications, 0 unread" })).toBeInTheDocument();
		expect(screen.queryByRole("region", { name: "Notifications" })).not.toBeInTheDocument();
	});
});

describe("the full-text search", () => {
	function withPassages() {
		server
			.withAccount("Rita", "a long password", "reader")
			.signedInAs("Rita")
			.withLibrary("Novels")
			.withBook("Moby-Dick", {
				files: [{ id: "file-1", kind: "ebook", format: "epub", name: "Moby-Dick.epub", size: 1_000_000, missing: false, drm: false, extractState: "done" }],
			})
			.withBook("Ledger", {
				files: [{ id: "file-2", kind: "ebook", format: "pdf", name: "Ledger.pdf", size: 1_000_000, missing: false, drm: false, extractState: "done" }],
			});
		server.passages = [
			{
				bookId: "book-1",
				hit: {
					fileId: "file-1", format: "epub", position: 3, chapter: "Two", offset: 24_000,
					snippet: [{ text: "the " }, { text: "white", match: true }, { text: " " }, { text: "whale", match: true }, { text: " rose" }],
				},
			},
			{
				bookId: "book-2",
				hit: {
					fileId: "file-2", format: "pdf", position: 0, pageFrom: 7, pageTo: 8, offset: 0,
					snippet: [{ text: "a " }, { text: "white", match: true }, { text: " ledger and a " }, { text: "whale", match: true }],
				},
			},
		];
	}
	const results = async () => within(await screen.findByRole("region", { name: "Books found" }));
	const saves = () => server.requests.filter((r) => r.method === "PUT" && r.path.includes("/progress/"));

	it("is reached from the header with the words typed there, and keeps query and filters in the address", async () => {
		const person = userEvent.setup();
		withPassages();
		const { router } = renderApp("/");
		await person.type(await screen.findByRole("combobox", { name: "Search books" }), "white whale");
		await person.click(await screen.findByRole("option", { name: "Search the text of the books for “white whale”" }));

		const found = await results();
		expect(await found.findByRole("link", { name: "Moby-Dick" })).toBeInTheDocument();
		expect(found.getByRole("link", { name: "Ledger" })).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/search");
		expect(router.state.location.search).toMatchObject({ q: "white whale" });
		expect(screen.getByRole("searchbox", { name: "Words to look for" })).toHaveValue("white whale");
		expect(found.getAllByText("whale")[0]?.tagName).toBe("MARK");
		expect(found.getByText("Two")).toBeInTheDocument();
		expect(found.getByText("Pages 7–8")).toBeInTheDocument();

		const filters = within(screen.getByRole("complementary", { name: "Filters" }));
		await person.click(await filters.findByRole("checkbox", { name: "PDF (1)" }));
		await waitFor(() => expect(found.queryByRole("link", { name: "Moby-Dick" })).not.toBeInTheDocument());
		expect(router.state.location.search).toMatchObject({ q: "white whale", format: ["pdf"] });
		const asked = server.requests.filter((r) => r.path.startsWith("/search?")).at(-1)?.path ?? "";
		expect(new URLSearchParams(asked.slice("/search?".length)).get("filter")).toBe(
			JSON.stringify({ all: [{ field: "format", op: "in", values: ["pdf"] }] }),
		);

		await person.clear(screen.getByRole("searchbox", { name: "Words to look for" }));
		await person.type(screen.getByRole("searchbox", { name: "Words to look for" }), "nothing like it{Enter}");
		expect(await screen.findByText("No book's text holds those words.")).toBeInTheDocument();
		expect(router.state.location.search).toMatchObject({ q: "nothing like it", format: ["pdf"] });
	});

	it("opens a PDF at the page of a passage among its pages, and saves no place until the person moves on", async () => {
		const person = userEvent.setup();
		withPassages();
		const { router } = renderApp("/search?q=ledger");
		await person.click(await (await results()).findByRole("link", { name: "Read from here" }));
		expect(router.state.location.pathname).toBe("/books/book-2/read/file-2");
		expect(router.state.location.search).toMatchObject({ page: 7, to: 8, find: "white" });
		await waitFor(() => expect(screen.getByRole("textbox", { name: "Page number" })).toHaveValue("8"));
		await new Promise((r) => setTimeout(r, 800));
		expect(saves()).toEqual([]);
		await person.click(screen.getByRole("button", { name: "Next page" }));
		await waitFor(() => expect(saves()).toHaveLength(1));
		expect(saves()[0]?.body).toMatchObject({ locator: "page:9" });
	});

	it("opens an EPUB at the passage, looked for by its longest word near its text", async () => {
		const person = userEvent.setup();
		withPassages();
		renderApp("/search?q=rose");
		await person.click(await (await results()).findByRole("link", { name: "Read from here" }));
		await waitFor(() => expect(ebook.finds).toEqual([{ word: "white", passage: "the white whale rose" }]));
		expect(ebook.opened.at(-1)?.start).toBeUndefined();
		await waitFor(() => expect(screen.getByText("50%")).toBeInTheDocument());
		await new Promise((r) => setTimeout(r, 800));
		expect(saves()).toEqual([]);
		await person.click(screen.getByRole("button", { name: "Page right" }));
		await waitFor(() => expect(saves()).toHaveLength(1));
		expect(saves()[0]?.body).toMatchObject({ locator: "epubcfi(/6/6)" });
	});
});

describe("the search index", () => {
	it("says how much of the text search knows, and has it read again or the index rebuilt", async () => {
		const person = userEvent.setup();
		server.withAccount("Edith", "a long password", "editor").signedInAs("Edith").withLibrary("Novels");
		renderApp("/jobs");
		const panel = within(await screen.findByRole("region", { name: "Search index" }));
		expect(await panel.findByText("Books whose text search knows: 4 of 4.")).toBeInTheDocument();

		await person.click(panel.getByRole("button", { name: "Read all text again" }));
		expect(await panel.findByText(/^Books whose text is read again in the background: 4\./)).toBeInTheDocument();
		expect(await panel.findByText("Books whose text search knows: 0 of 4.")).toBeInTheDocument();
		expect(server.requests.find((r) => r.path === "/search/reread")?.body).toEqual({});

		await person.click(panel.getByRole("button", { name: "Rebuild the index" }));
		expect(await panel.findByText(/^The index is built again in the background/)).toBeInTheDocument();
		expect(await panel.findByText(/^The index is being built again/)).toBeInTheDocument();
		expect(panel.getByRole("button", { name: "Rebuild the index" })).toBeDisabled();
	});

	it("reads one library's or one book's text again", async () => {
		const person = userEvent.setup();
		server
			.withAccount("Edith", "a long password", "editor")
			.signedInAs("Edith")
			.withLibrary("Novels")
			.withBook("Emma", {
				files: [{ id: "file-1", kind: "ebook", format: "epub", name: "Emma.epub", size: 10, missing: false, drm: false, extractState: "done", hasText: true }],
			});
		renderApp("/books/book-1");
		await person.click(await screen.findByRole("button", { name: "Read the text again for search" }));
		expect(await screen.findByText(/^Books whose text is read again in the background: 1\./)).toBeInTheDocument();
		expect(server.requests.find((r) => r.path === "/search/reread")?.body).toEqual({ book: "book-1" });

		await person.click(screen.getByRole("link", { name: "Back to the library" }));
		await person.click(await screen.findByRole("button", { name: "Read this library's text again for search" }));
		await waitFor(() =>
			expect(server.requests.filter((r) => r.path === "/search/reread").at(-1)?.body).toEqual({ library: server.libraries[0]?.id }),
		);
	});

	it("is not offered to a reader", async () => {
		server.withAccount("Rita", "a long password", "reader").signedInAs("Rita").withLibrary("Novels").withBook("Emma");
		renderApp("/books/book-1");
		expect(await screen.findByRole("heading", { name: "Emma", level: 1 })).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Read the text again for search" })).not.toBeInTheDocument();
	});
});

describe("the duplicates page", () => {
	function withPairs(role: "editor" | "reader") {
		const file = (id: string, format: string, name: string, size: number) => ({
			id, kind: "ebook", format, name, size, missing: false, drm: false, extractState: "done",
		});
		server
			.withAccount("Edith", "a long password", role)
			.signedInAs("Edith")
			.withLibrary("Novels")
			.withBook("Emma", { files: [file("f1", "epub", "Emma.epub", 300_000)] })
			.withBook("Emma (print)", { files: [file("f2", "pdf", "Emma.pdf", 2_000_000)] })
			.withBook("The Complete Austen", { files: [file("f3", "epub", "Austen.epub", 5_000_000)] })
			.withBook("Persuasion", { files: [file("f4", "epub", "Persuasion.epub", 280_000)] });
		server
			.withPair("pair-1", "book-1", "book-2", [
				{ kind: "isbn", detail: "9780141439587", score: 0.9 },
				{ kind: "overlap", detail: "jaccard=0.92 a_in_b=0.95 b_in_a=0.94", score: 0.95 },
			])
			.withPair("pair-2", "book-4", "book-3", [
				{ kind: "overlap", detail: "jaccard=0.18 a_in_b=0.97 b_in_a=0.19", score: 0.97 },
			]);
		server.pairs.sort((x, y) => y.score - x.score);
	}

	it("says why each pair looks like one, compares them side by side, and keeps both", async () => {
		const person = userEvent.setup();
		withPairs("editor");
		const { router } = renderApp("/");
		await person.click(await screen.findByRole("link", { name: "Duplicates" }));
		const list = within(await screen.findByRole("region", { name: "Pairs" }));
		const omnibus = within(await list.findByRole("listitem", { name: "Persuasion is 97% contained in The Complete Austen." }));
		const emma = within(list.getByRole("listitem", { name: "92% text overlap between Emma and Emma (print)." }));
		expect(omnibus.getByText("Score 97%")).toBeInTheDocument();
		const evidence = within(emma.getByRole("list", { name: "Evidence" }));
		expect(evidence.getByText("The ISBN 9780141439587")).toBeInTheDocument();
		expect(evidence.getByText(/^Shared text: 92% overlap; 95% of Emma is in Emma \(print\)/)).toBeInTheDocument();
		expect(evidence.getAllByText(/^\d+%$/).map((n) => n.textContent)).toEqual(["95%", "90%"]);

		await person.click(emma.getByRole("button", { name: "Compare" }));
		const files = await emma.findByRole("row", { name: /^Files/ });
		expect(files).toHaveTextContent("EPUB Emma.epub");
		expect(files).toHaveTextContent("PDF Emma.pdf");

		await person.selectOptions(screen.getByRole("combobox", { name: "Evidence" }), "isbn");
		await waitFor(() => expect(list.queryByRole("listitem", { name: /Persuasion/ })).not.toBeInTheDocument());
		expect(router.state.location.search).toMatchObject({ kind: "isbn" });
		await person.selectOptions(screen.getByRole("combobox", { name: "Evidence" }), "");

		await person.click(within(await list.findByRole("listitem", { name: /^Persuasion/ })).getByRole("button", { name: "Keep both" }));
		await waitFor(() => expect(list.queryByRole("listitem", { name: /^Persuasion/ })).not.toBeInTheDocument());
		expect(server.pairs.find((p) => p.id === "pair-2")?.state).toBe("kept_both");
		await person.click(screen.getByRole("button", { name: "Kept both" }));
		expect(await list.findByRole("listitem", { name: /^Persuasion/ })).toBeInTheDocument();
		expect(router.state.location.search).toMatchObject({ state: "kept_both" });
	});

	it("shows a reader the pairs but no way to decide", async () => {
		withPairs("reader");
		renderApp("/duplicates");
		const list = within(await screen.findByRole("region", { name: "Pairs" }));
		expect(await list.findAllByRole("button", { name: "Compare" })).toHaveLength(2);
		expect(list.queryByRole("button", { name: "Keep both" })).not.toBeInTheDocument();
	});
});

describe("the trash", () => {
	const file = { id: "file-1", kind: "ebook", format: "epub", name: "Emma.epub", size: 300_000, missing: false, drm: false, extractState: "done" };

	it("takes a file out of its book, and puts it back", async () => {
		const person = userEvent.setup();
		server.withAccount("Edith", "a long password", "editor").signedInAs("Edith").withLibrary("Novels").withBook("Emma", { files: [file] });
		renderApp("/books/book-1");
		await person.click(await screen.findByRole("button", { name: "Move Emma.epub to the trash" }));
		await waitFor(() => expect(screen.queryByRole("button", { name: "Move Emma.epub to the trash" })).not.toBeInTheDocument());
		expect(server.requests.some((r) => r.method === "POST" && r.path === "/files/file-1/trash")).toBe(true);

		await person.click(screen.getByRole("link", { name: "Trash" }));
		const item = within(await screen.findByRole("listitem", { name: "Emma.epub" }));
		expect(item.getByRole("link", { name: "Emma" })).toHaveAttribute("href", "/books/book-1");
		expect(item.getByText(/^Trashed on .* by Edith\. Deleted for good on /)).toBeInTheDocument();
		// Deleting for good is an administrator's.
		expect(item.queryByRole("button", { name: "Delete now" })).not.toBeInTheDocument();
		await person.click(item.getByRole("button", { name: "Restore" }));
		expect(await screen.findByText("The trash is empty.")).toBeInTheDocument();
		expect(server.books[0]?.files).toHaveLength(1);
	});

	it("lets an administrator delete for good, after saying so twice", async () => {
		const person = userEvent.setup();
		server.withAccount("Ada", "a long password", "admin").signedInAs("Ada").withLibrary("Novels").withBook("Emma", { files: [file] });
		renderApp("/books/book-1");
		await person.click(await screen.findByRole("button", { name: "Move Emma.epub to the trash" }));
		await waitFor(() => expect(server.trashed).toHaveLength(1));
		await person.click(screen.getByRole("link", { name: "Trash" }));
		const item = within(await screen.findByRole("listitem", { name: "Emma.epub" }));
		await person.click(item.getByRole("button", { name: "Delete now" }));
		expect(server.trashed).toHaveLength(1);
		await person.click(item.getByRole("button", { name: "Delete for good" }));
		expect(await screen.findByText("The trash is empty.")).toBeInTheDocument();
		expect(server.books[0]?.files).toHaveLength(0);
	});

	it("is not offered in a library GOtome does not write to", async () => {
		server.withAccount("Edith", "a long password", "editor").signedInAs("Edith").withLibrary("Shelf", { mode: "external", writable: false }).withBook("Emma", { files: [file] });
		renderApp("/books/book-1");
		expect(await screen.findByRole("heading", { name: "Emma", level: 1 })).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Move Emma.epub to the trash" })).not.toBeInTheDocument();
	});
});

describe("merging books", () => {
	it("keeps one book with the values picked, and leads the other's address to it", async () => {
		const person = userEvent.setup();
		const file = (id: string, format: string, name: string) => ({
			id, kind: "ebook", format, name, size: 1000, missing: false, drm: false, extractState: "done",
		});
		server
			.withAccount("Edith", "a long password", "editor")
			.signedInAs("Edith")
			.withLibrary("Novels")
			.withBook("Emma", { files: [file("f1", "epub", "Emma.epub")] })
			.withBook("Emma: A Novel", { files: [file("f2", "pdf", "Emma.pdf")] })
			.withPair("pair-1", "book-1", "book-2", [{ kind: "title_author", detail: "emma / austen", score: 0.75 }]);
		const { router } = renderApp("/duplicates");
		const pair = within(await screen.findByRole("listitem", { name: "Emma and Emma: A Novel have the same title and author." }));
		await person.click(pair.getByRole("button", { name: "Merge…" }));
		const form = within(await pair.findByRole("form", { name: "Merge the two books" }));
		expect(form.getByRole("radio", { name: "Emma, files: 1" })).toBeChecked();
		const title = within(form.getByRole("radiogroup", { name: "Title" }));
		await person.click(title.getByRole("radio", { name: "Emma: A Novel" }));
		await person.click(form.getByRole("button", { name: "Merge into Emma" }));

		await waitFor(() => expect(screen.queryByRole("listitem", { name: /have the same title and author/ })).not.toBeInTheDocument());
		const sent = server.requests.find((r) => r.path === "/books/book-1/merge");
		expect(sent?.body).toEqual({ from: "book-2", take: ["title"] });

		await router.navigate({ to: "/books/$bookId", params: { bookId: "book-2" } });
		expect(await screen.findByRole("heading", { name: "Emma: A Novel", level: 1 })).toBeInTheDocument();
		await waitFor(() => expect(router.state.location.pathname).toBe("/books/book-1"));
	});
});
