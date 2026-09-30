import { QueryClient } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { createElement } from "react";
import { vi } from "vitest";
import { App } from "@/app";
import type { CurrentUser } from "@/auth/session";
import type { BookDetail } from "@/books/api";
import type { Library, Scan } from "@/libraries/api";
import { makeRouter } from "@/router";

const PERMISSIONS = {
	reader: ["library:read", "personal:manage"],
	editor: [
		"library:read",
		"personal:manage",
		"books:upload",
		"metadata:edit",
		"index:rebuild",
	],
	admin: [
		"library:read",
		"personal:manage",
		"books:upload",
		"metadata:edit",
		"index:rebuild",
		"users:manage",
		"settings:manage",
		"storage:manage",
	],
} as const;

export function user(
	username: string,
	role: keyof typeof PERMISSIONS = "admin",
): CurrentUser {
	return {
		id: `id-${username}`,
		username,
		role,
		permissions: [...PERMISSIONS[role]],
	};
}

interface Account {
	password: string;
	user: CurrentUser;
}

/**
 * A stand-in for the server's auth endpoints with the same rules: setup works
 * once, login needs the right password, /auth/me needs a session.
 */
export class FakeServer {
	accounts = new Map<string, Account>();
	session: CurrentUser | null = null;
	requests: { method: string; path: string; body: unknown }[] = [];
	libraries: Library[] = [];
	books: BookDetail[] = [];
	/** What the next scan of a library finds; a scan is finished at once. */
	scanFinds: Partial<Scan> = { filesSeen: 0 };
	/** Set to make every request fail as if the server were unreachable. */
	down = false;

	withAccount(
		username: string,
		password: string,
		role: keyof typeof PERMISSIONS = "admin",
	): this {
		this.accounts.set(username.toLowerCase(), {
			password,
			user: user(username, role),
		});
		return this;
	}

	withLibrary(name: string, overrides: Partial<Library> = {}): this {
		const id = `lib-${this.libraries.length + 1}`;
		this.libraries.push({
			id,
			name,
			mode: "managed",
			writable: true,
			visibility: "shared",
			rootPath: `/data/libraries/${id}`,
			createdAt: "2026-01-01T00:00:00Z",
			filesPending: 0,
			...overrides,
		});
		return this;
	}

	/** Adds a book to the library of that name, or the first library. */
	withBook(title: string, overrides: Partial<BookDetail> = {}, libraryName?: string): this {
		const library = this.libraries.find((l) => l.name === libraryName) ?? this.libraries[0];
		const n = this.books.length + 1;
		this.books.push({
			id: `book-${n}`,
			libraryId: library?.id ?? "lib-1",
			title,
			contributors: [],
			tags: [],
			identifiers: [],
			files: [],
			addedAt: `2026-01-01T00:00:${String(n % 60).padStart(2, "0")}Z`,
			...overrides,
		});
		return this;
	}

	signedInAs(username: string): this {
		this.session = this.accounts.get(username.toLowerCase())?.user ?? null;
		return this;
	}

	install(): this {
		vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
			if (this.down) {
				throw new TypeError("Failed to fetch");
			}
			const request = input as Request;
			const url = new URL(request.url);
			const path = url.pathname.replace(/^\/api\/v1/, "");
			const body: unknown =
				request.method === "GET"
					? undefined
					: await request.json().catch(() => undefined);
			this.requests.push({ method: request.method, path: path + url.search, body });
			if (path === "/books" || path.startsWith("/books/")) {
				return this.answerBooks(path, url.searchParams);
			}
			return this.answer(
				request.method,
				path,
				body as Record<string, string> | undefined,
			);
		});
		return this;
	}

	private answer(method: string, path: string, body: Record<string, string> = {}): Response {
		const refuse = (
			status: number,
			code: string,
			message: string,
			fields?: Record<string, string>,
		) =>
			Response.json(
				{ error: { code, message, fields, requestId: "req-1" } },
				{ status },
			);

		if (path === "/libraries" || path.startsWith("/libraries/")) {
			return this.answerLibraries(method, path.slice("/libraries".length + 1), body, refuse);
		}

		switch (`${method} ${path}`) {
			case "GET /setup":
				return Response.json({ needed: this.accounts.size === 0 });
			case "POST /setup": {
				if (this.accounts.size > 0) {
					return refuse(
						409,
						"conflict",
						"GOtome is already set up. Sign in instead.",
					);
				}
				if ((body.password ?? "").length < 8) {
					return refuse(
						422,
						"validation_failed",
						"Some fields need attention.",
						{
							password: "A password has at least 8 characters.",
						},
					);
				}
				this.withAccount(body.username ?? "", body.password ?? "").signedInAs(
					body.username ?? "",
				);
				return Response.json(this.session, { status: 201 });
			}
			case "POST /auth/login": {
				const account = this.accounts.get((body.username ?? "").toLowerCase());
				if (!account || account.password !== body.password) {
					return refuse(
						401,
						"unauthorized",
						"The user name or the password is wrong.",
					);
				}
				this.session = account.user;
				return Response.json(account.user);
			}
			case "POST /auth/logout":
				this.session = null;
				return new Response(null, { status: 204 });
			case "GET /auth/me":
				return this.session
					? Response.json(this.session)
					: refuse(401, "unauthorized", "Sign in to continue.");
			default:
				return refuse(404, "not_found", "There is nothing at this address.");
		}
	}
	private answerBooks(path: string, query: URLSearchParams): Response {
		if (!this.session) {
			return Response.json({ error: { code: "unauthorized", message: "Sign in to continue." } }, { status: 401 });
		}
		if (path !== "/books") {
			const book = this.books.find((b) => `/books/${b.id}` === path);
			return book
				? Response.json(book)
				: Response.json({ error: { code: "not_found", message: "There is no such book." } }, { status: 404 });
		}
		const authorOf = (b: BookDetail) => b.contributors.find((c) => c.role === "author")?.name ?? "";
		const sort = query.get("sort") ?? "title";
		const key = (b: BookDetail) =>
			sort === "author" ? `${authorOf(b)}\u0000${b.title}` : sort === "added" ? b.addedAt : b.title;
		const sorted = this.books
			.filter((b) => !query.get("library") || b.libraryId === query.get("library"))
			.sort((a, b) => key(a).localeCompare(key(b)));
		if (query.get("order") === "desc") {
			sorted.reverse();
		}
		// The cursor is how many books came before; the real one is not, but
		// a client cannot tell.
		const start = Number(query.get("cursor") || 0);
		const limit = Number(query.get("limit") || 50);
		const page = sorted.slice(start, start + limit);
		return Response.json({
			books: page.map((b) => ({
				id: b.id,
				libraryId: b.libraryId,
				title: b.title,
				authors: b.contributors.filter((c) => c.role === "author").map((c) => c.name),
				coverKey: b.coverKey,
				formats: [...new Set(b.files.map((f) => f.format))],
				addedAt: b.addedAt,
			})),
			nextCursor: start + limit < sorted.length ? String(start + limit) : undefined,
		});
	}

	private answerLibraries(
		method: string,
		id: string,
		body: Record<string, unknown>,
		refuse: (status: number, code: string, message: string, fields?: Record<string, string>) => Response,
	): Response {
		if (!this.session) {
			return refuse(401, "unauthorized", "Sign in to continue.");
		}
		const manages = this.session.permissions.includes("storage:manage");
		// What a reader is shown leaves out where the files lie.
		const shown = (library: Library): Library =>
			manages ? library : { ...library, rootPath: undefined, ownerId: undefined };

		if (method === "GET" && id === "") {
			return Response.json({ libraries: this.libraries.map(shown) });
		}
		if (method === "POST" && id.endsWith("/scans")) {
			const library = this.libraries.find((l) => `${l.id}/scans` === id);
			if (!this.session.permissions.includes("index:rebuild")) {
				return refuse(403, "forbidden", "You do not have permission to do that.");
			}
			if (!library) {
				return refuse(404, "not_found", "There is no such library.");
			}
			library.lastScan = {
				id: `scan-${library.id}`,
				libraryId: library.id,
				state: "done",
				requestedAt: "2026-01-02T10:00:00Z",
				startedAt: "2026-01-02T10:00:00Z",
				finishedAt: "2026-01-02T10:00:01Z",
				filesSeen: 0,
				filesAdded: 0,
				filesChanged: 0,
				filesMoved: 0,
				filesRestored: 0,
				filesMissing: 0,
				filesSkipped: 0,
				booksAdded: 0,
				...this.scanFinds,
			};
			return Response.json(library.lastScan, { status: 202 });
		}
		if (!manages) {
			return refuse(403, "forbidden", "You do not have permission to do that.");
		}
		const invalid = (fields: Record<string, string>) =>
			refuse(422, "validation_failed", "Some fields need attention.", fields);
		const nameTaken = (name: string, except?: string) =>
			this.libraries.some((l) => l.id !== except && l.name.toLowerCase() === name.toLowerCase());

		if (method === "POST" && id === "") {
			const name = String(body.name ?? "").trim();
			if (name === "") {
				return invalid({ name: "Give the library a name." });
			}
			if (nameTaken(name)) {
				return invalid({ name: "Another library already has this name." });
			}
			if (body.mode === "external" && !String(body.rootPath ?? "").startsWith("/")) {
				return invalid({ rootPath: "Give the folder as a full path, such as /books." });
			}
			this.withLibrary(name, {
				mode: body.mode as string,
				visibility: body.visibility as string,
				writable: body.mode === "managed",
				...(body.mode === "external" ? { rootPath: body.rootPath as string } : {}),
			});
			return Response.json(this.libraries.at(-1), { status: 201 });
		}

		const library = this.libraries.find((l) => l.id === id);
		if (!library) {
			return refuse(404, "not_found", "There is no such library.");
		}
		if (method === "PATCH") {
			if (typeof body.name === "string" && nameTaken(body.name.trim(), id)) {
				return invalid({ name: "Another library already has this name." });
			}
			Object.assign(library, body);
			return Response.json(library);
		}
		if (method === "DELETE") {
			this.libraries = this.libraries.filter((l) => l.id !== id);
			return new Response(null, { status: 204 });
		}
		return refuse(404, "not_found", "There is nothing at this address.");
	}
}

/** Renders the whole app at an address, against the fake server. */
export function renderApp(path: string) {
	// No retries: a test that expects a failure should not wait for three.
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	const history = createMemoryHistory({ initialEntries: [path] });
	const router = makeRouter(queryClient, history);
	return { router, ...render(createElement(App, { queryClient, router })) };
}
