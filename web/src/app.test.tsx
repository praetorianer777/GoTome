import { screen, waitFor } from "@testing-library/react";
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
			await screen.findByRole("heading", { name: "No books yet" }),
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
