import { screen, waitFor, within } from "@testing-library/react";
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
});
