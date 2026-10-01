import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { FakeServer, renderApp } from "@/test/fake-server";

let server: FakeServer;

beforeEach(() => {
	localStorage.clear();
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
				files: [{ id: "file-1", kind: "ebook", format: "epub", name: "Persuasion.epub", size: 1_400_000, missing: false, drm: false }],
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
				identifiers: [{ type: "isbn", value: "9780441013593" }],
				durationMs: 21 * 3600_000 + 2 * 60_000,
				files: [
					{ id: "file-2", kind: "audio", format: "mp3", name: "Dune 1.mp3", size: 400_000_000, missing: false, drm: false, part: 0, durationMs: 11 * 3600_000 },
					{ id: "file-3", kind: "audio", format: "mp3", name: "Dune 2.mp3", size: 380_000_000, missing: true, drm: false, part: 1 },
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
		const author = (name: string) => [{ name, role: "author" }];
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
