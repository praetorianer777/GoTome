import { createMemoryHistory } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { App } from "@/app";
import { makeRouter } from "@/router";

function renderAt(path: string) {
	const router = makeRouter();
	router.update({ history: createMemoryHistory({ initialEntries: [path] }) });
	return render(<App router={router} />);
}

function serverAnswers(status: number, body: unknown) {
	vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
		Response.json(body, { status }),
	);
}

describe("app", () => {
	it("shows the server's version on the home page", async () => {
		serverAnswers(200, { version: "0.1.0" });
		renderAt("/");

		expect(await screen.findByRole("heading", { name: "GOtome" })).toBeInTheDocument();
		expect(await screen.findByText("Server version 0.1.0")).toBeInTheDocument();
	});

	it("says so when the server refuses", async () => {
		serverAnswers(403, {
			error: {
				code: "forbidden",
				message: "You do not have permission to do that.",
			},
		});
		renderAt("/");

		expect(
			await screen.findByText(
				"The server does not answer: You do not have permission to do that.",
			),
		).toBeInTheDocument();
	});

	it("answers an unknown address with a way back", async () => {
		serverAnswers(200, { version: "0.1.0" });
		renderAt("/no/such/page");

		expect(
			await screen.findByRole("heading", { name: "There is no page here" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: "Back to the library" }),
		).toHaveAttribute("href", "/");
	});
});
