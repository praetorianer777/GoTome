import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { saveProgress } from "@/books/progress";
import { setLocale } from "@/i18n";
import { FakeServer, renderApp } from "@/test/fake-server";
import { installFakeCaches, removeFakeServiceWorker } from "@/test/fake-caches";
import { resetDb } from "./db";
import { offlineFetch } from "./fetch";
import { clearOffline, listKept } from "./kept";
import { flushQueue, queuedPositions } from "./queue";

let server: FakeServer;
let stores: Map<string, Map<string, Response>>;

beforeEach(() => {
	localStorage.clear();
	setLocale(null);
	resetDb();
	stores = installFakeCaches();
	server = new FakeServer().install();
	server
		.withAccount("Rita", "a long password", "reader")
		.signedInAs("Rita")
		.withLibrary("Novels")
		.withBook("Persuasion", {
			contributors: [{ name: "Jane Austen", role: "author" }],
			files: [
				{
					id: "file-1",
					kind: "ebook",
					format: "epub",
					name: "Persuasion.epub",
					size: 1_400_000,
					missing: false,
					drm: false,
					extractState: "done",
				},
			],
		});
});

afterEach(() => removeFakeServiceWorker());

const update = (fraction: number) => ({ locator: `fraction:${fraction}`, fraction });

function client() {
	return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

describe("kept books", () => {
	it("keeps a file from the book's page, lists it on the profile and removes it", async () => {
		const person = userEvent.setup();
		const first = renderApp("/books/book-1");
		await person.click(
			await screen.findByRole("button", { name: "Keep Persuasion.epub offline" }),
		);
		expect(
			await screen.findByRole("button", { name: "Remove the offline copy of Persuasion.epub" }),
		).toBeInTheDocument();
		const kept = [...stores.values()].flatMap((s) => [...s.keys()]);
		expect(kept).toContain(`${window.location.origin}/api/v1/files/file-1/download`);
		expect(kept.some((u) => u.endsWith("/api/v1/books/book-1/progress"))).toBe(true);

		first.unmount();
		renderApp("/profile");
		const offline = within(await screen.findByRole("region", { name: "Offline" }));
		expect(await offline.findByRole("link", { name: "Persuasion" })).toBeInTheDocument();
		expect(offline.getByText("Kept offline: 1.4 MB.")).toBeInTheDocument();
		await person.click(
			offline.getByRole("button", { name: "Remove the offline copy of Persuasion.epub" }),
		);
		await waitFor(() => expect(offline.queryByRole("link", { name: "Persuasion" })).toBeNull());
		expect(await listKept()).toEqual([]);
		expect([...stores.get([...stores.keys()].find((k) => k.endsWith(":meta")) ?? "")?.keys() ?? []]).toEqual([]);
	});

	it("says so where the browser cannot keep books", async () => {
		removeFakeServiceWorker();
		renderApp("/profile");
		expect(
			await screen.findByText(/cannot run the helper that keeps books offline/),
		).toBeInTheDocument();
	});
});

describe("answers kept for offline reading", () => {
	it("are read from the copy when the network does not answer, and only then", async () => {
		renderApp("/books/book-1");
		await userEvent.click(
			await screen.findByRole("button", { name: "Keep Persuasion.epub offline" }),
		);
		await screen.findByRole("button", { name: /Remove the offline copy/ });
		const book = new Request(new URL("/api/v1/books/book-1", window.location.origin));
		server.down = true;
		const answer = await offlineFetch(book);
		expect((await answer.json()).title).toBe("Persuasion");
		await expect(
			offlineFetch(new Request(new URL("/api/v1/books/book-2", window.location.origin))),
		).rejects.toThrow(TypeError);
	});

	it("go when the account that kept them signs out", async () => {
		const person = userEvent.setup();
		renderApp("/books/book-1");
		await person.click(
			await screen.findByRole("button", { name: "Keep Persuasion.epub offline" }),
		);
		await screen.findByRole("button", { name: /Remove the offline copy/ });
		await person.click(screen.getByRole("button", { name: /^Account/ }));
		await person.click(await screen.findByRole("button", { name: "Sign out" }));
		await waitFor(async () => expect(await listKept()).toEqual([]));
		expect([...stores.values()].flatMap((s) => [...s.keys()])).toEqual([]);
	});
});

describe("positions written offline", () => {
	it("are queued, one per book on its first basedOn, and sent when the network is back", async () => {
		const queryClient = client();
		queryClient.setQueryData(["progress", "book-1"], {
			finishes: 0,
			ebook: { locator: "fraction:0.1", fraction: 0.1, updatedAt: "2026-01-02T00:00:00Z" },
		});
		server.down = true;
		expect(await saveProgress(queryClient, "book-1", "ebook", update(0.2))).toBeUndefined();
		expect(await saveProgress(queryClient, "book-1", "ebook", update(0.4))).toBeUndefined();
		const queued = await queuedPositions();
		expect(queued).toHaveLength(1);
		expect(queued[0]).toMatchObject({ bookId: "book-1", basedOn: "2026-01-02T00:00:00Z" });
		expect(server.requests.filter((r) => r.method === "PUT")).toHaveLength(0);

		await flushQueue(queryClient);
		expect(await queuedPositions()).toHaveLength(1);

		server.down = false;
		await flushQueue(queryClient);
		expect(await queuedPositions()).toEqual([]);
		const sent = server.requests.filter((r) => r.method === "PUT");
		expect(sent).toHaveLength(1);
		expect(sent[0]?.body).toMatchObject({
			fraction: 0.4,
			basedOn: "2026-01-02T00:00:00Z",
		});
	});

	it("are dropped when another device was further, as online", async () => {
		const queryClient = client();
		server.furtherProgress = {
			locator: "fraction:0.9",
			fraction: 0.9,
			updatedAt: "2026-01-04T00:00:00Z",
		} as typeof server.furtherProgress;
		server.down = true;
		await saveProgress(queryClient, "book-1", "ebook", update(0.2));
		server.down = false;
		await flushQueue(queryClient);
		expect(await queuedPositions()).toEqual([]);
		expect(server.progress["book-1"]).toBeUndefined();
	});

	it("go ahead of a position written online afterwards", async () => {
		const queryClient = client();
		server.down = true;
		await saveProgress(queryClient, "book-1", "ebook", update(0.2));
		server.down = false;
		await saveProgress(queryClient, "book-1", "ebook", update(0.3));
		const sent = server.requests.filter((r) => r.method === "PUT").map((r) => (r.body as { fraction: number }).fraction);
		expect(sent).toEqual([0.2, 0.3]);
	});

	it("are cleared with everything else kept", async () => {
		const queryClient = client();
		server.down = true;
		await saveProgress(queryClient, "book-1", "ebook", update(0.2));
		await clearOffline();
		expect(await queuedPositions()).toEqual([]);
		vi.restoreAllMocks();
	});
});
