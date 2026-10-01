import { QueryClient } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { createElement } from "react";
import { vi } from "vitest";
import { App } from "@/app";
import type { CurrentUser } from "@/auth/session";
import type { BookDetail } from "@/books/api";
import type { BulkRequest, BulkResult, BulkStatus } from "@/books/bulk";
import type { BookEdit, Candidate, CandidateApply, ReviewBook } from "@/books/edit";
import type { Uploaded } from "@/books/upload";
import type { Library, Scan } from "@/libraries/api";
import type { Setting } from "@/settings/api";
import type { Job } from "@/jobs/api";
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
	disabled?: boolean;
	quotaBytes?: number;
	usedBytes?: number;
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
	/** The signed-in person's sessions, as the profile page lists them. */
	sessions = [
		{ id: "s-1", userAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0", createdAt: "2026-01-01T08:00:00Z", lastSeenAt: "2026-01-03T08:00:00Z", current: true },
		{ id: "s-2", userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile Safari/604.1", createdAt: "2026-01-01T08:00:00Z", lastSeenAt: "2026-01-02T08:00:00Z", current: false },
	];
	/** The jobs the jobs page lists, newest first. */
	jobs: Job[] = [];
	/** The books whose matches wait for review. */
	review: ReviewBook[] = [];
	/** What the metadata providers know, by book. */
	candidates: Record<string, { candidates: Candidate[]; failures: { provider: string; message: string }[] }> = {};
	/** The bulk changes asked for, each finished as soon as it is asked for. */
	bulks: BulkStatus[] = [];
	/** Files somebody asked to have read again. */
	reread: string[] = [];
	/** The secrets as they were sent, which the fake keeps and never sends back. */
	secrets = new Map<string, string>();
	private settings: Setting[] = [
		{ key: "metadata.language", kind: "text", value: "en", isSet: false },
		{ key: "metadata.googleBooksKey", kind: "secret", isSet: false },
		{ key: "metadata.hardcoverToken", kind: "secret", isSet: false },
	];
	/** What was uploaded, by content, to find a file sent twice. */
	private uploaded = new Map<string, Uploaded>();

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
			filesFailed: 0,
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
			fields: {},
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
			if (path === "/books/bulk" || path.startsWith("/bulk/")) {
				return this.answerBulk(path, body as BulkRequest);
			}
			if (path.startsWith("/matches")) {
				return this.answerMatches(request.method, path, body as CandidateApply);
			}
			if (path === "/books" || path.startsWith("/books/")) {
				return this.answerBooks(request.method, path, url.searchParams, body);
			}
			return this.answer(
				request.method,
				path,
				body as Record<string, string> | undefined,
			);
		});
		// Uploads go through XMLHttpRequest, which can report progress.
		const isDown = () => this.down;
		const record = (method: string, path: string, name: string) =>
			this.requests.push({ method, path, body: { file: name } });
		const answer = (path: string, file: File) => this.answerUpload(path, file);
		class FakeXMLHttpRequest {
			status = 0;
			responseText = "";
			responseType = "";
			withCredentials = false;
			upload: { onprogress: ((event: ProgressEvent) => void) | null } = {
				onprogress: null,
			};
			onload: (() => void) | null = null;
			onerror: (() => void) | null = null;
			private method = "";
			private url = "";

			open(method: string, url: string) {
				this.method = method;
				this.url = url;
			}

			send(body: FormData) {
				void (async () => {
					if (isDown()) {
						this.onerror?.();
						return;
					}
					const file = body.get("file") as File;
					const path = new URL(this.url).pathname.replace(/^\/api\/v1/, "");
					record(this.method, path, file.name);
					this.upload.onprogress?.({
						lengthComputable: true,
						loaded: file.size,
						total: file.size,
					} as ProgressEvent);
					const response = await answer(path, file);
					this.status = response.status;
					this.responseText = await response.text();
					this.onload?.();
				})();
			}
		}
		vi.stubGlobal("XMLHttpRequest", FakeXMLHttpRequest);
		return this;
	}

	private async answerUpload(path: string, file: File): Promise<Response> {
		const refuse = (status: number, code: string, message: string, fields?: Record<string, string>) =>
			Response.json({ error: { code, message, fields, requestId: "req-1" } }, { status });
		if (!this.session) {
			return refuse(401, "unauthorized", "Sign in to continue.");
		}
		if (!this.session.permissions.includes("books:upload")) {
			return refuse(403, "forbidden", "You do not have permission to do that.");
		}
		const library = this.libraries.find((l) => path === `/libraries/${l.id}/uploads`);
		if (!library) {
			return refuse(404, "not_found", "There is no such library.");
		}
		if (library.mode !== "managed") {
			return refuse(409, "conflict", "Books can only be uploaded into a managed library.");
		}
		const dot = file.name.lastIndexOf(".");
		const format = dot < 0 ? "" : file.name.slice(dot + 1).toLowerCase();
		if (!["epub", "pdf", "mobi", "azw", "azw3", "m4b", "m4a", "mp3", "flac", "ogg", "opus"].includes(format)) {
			return refuse(422, "validation_failed", "Some fields need attention.", {
				file: "GOtome does not read this kind of file.",
			});
		}
		const content = await file.text();
		const known = this.uploaded.get(content);
		if (known) {
			return Response.json({ ...known, outcome: "duplicate", fileId: undefined });
		}
		const me = this.accounts.get(this.session.username.toLowerCase());
		if (me?.quotaBytes !== undefined && (me.usedBytes ?? 0) + file.size > me.quotaBytes) {
			return refuse(413, "over_quota", "This file does not fit into your storage.");
		}
		if (me) {
			me.usedBytes = (me.usedBytes ?? 0) + file.size;
		}
		const title = file.name.slice(0, dot);
		this.withBook(title, {}, library.name);
		const book = this.books.at(-1) as BookDetail;
		const result: Uploaded = {
			outcome: "added",
			bookId: book.id,
			libraryId: library.id,
			title,
			fileId: `file-${book.id}`,
		};
		this.uploaded.set(content, result);
		return Response.json(result);
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

		if (path === "/jobs" || path.startsWith("/jobs/") || path.startsWith("/files/") || /^\/libraries\/[^/]+\/extractions$/.test(path)) {
			if (!this.session) {
				return refuse(401, "unauthorized", "Sign in to continue.");
			}
			if (!this.session.permissions.includes("index:rebuild")) {
				return refuse(403, "forbidden", "You do not have permission to do that.");
			}
			if (path === "/jobs") {
				return Response.json({ jobs: this.jobs });
			}
			const action = path.match(/^\/jobs\/(\d+)\/(retry|cancel)$/);
			if (action) {
				const job = this.jobs.find((j) => j.id === Number(action[1]));
				if (!job) {
					return refuse(404, "not_found", "There is no such job.");
				}
				job.state = action[2] === "retry" ? "available" : "cancelled";
				return Response.json(job);
			}
			const file = path.match(/^\/files\/([^/]+)\/extraction$/);
			if (file?.[1]) {
				this.reread.push(file[1]);
				return Response.json({ queued: 1 }, { status: 202 });
			}
			const library = this.libraries.find((l) => path === `/libraries/${l.id}/extractions`);
			if (library) {
				this.reread.push(library.id);
				library.filesFailed = 0;
				return Response.json({ queued: 1 }, { status: 202 });
			}
			return refuse(404, "not_found", "There is nothing at this address.");
		}
		if (path === "/libraries" || path.startsWith("/libraries/")) {
			return this.answerLibraries(method, path.slice("/libraries".length + 1), body, refuse);
		}

		if (path === "/users" || path.startsWith("/users/") || path.startsWith("/auth/sessions") || path === "/auth/password" || path === "/auth/storage") {
			return this.answerAccounts(method, path, body as Record<string, unknown>, refuse);
		}
		if (path === "/settings") {
			return this.answerSettings(method, body as unknown as { values?: Record<string, string | null> }, refuse);
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
				if (!account || account.password !== body.password || account.disabled) {
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
	private answerBooks(method: string, path: string, query: URLSearchParams, body: unknown): Response {
		if (!this.session) {
			return Response.json({ error: { code: "unauthorized", message: "Sign in to continue." } }, { status: 401 });
		}
		if (path === "/books/names") {
			return Response.json({ names: this.names(query.get("kind") ?? "", query.get("q") ?? "") });
		}
		const asked = /^\/books\/([^/]+)\/candidates(\/apply)?$/.exec(path);
		if (asked && !asked[2]) {
			return Response.json(this.candidates[asked[1] ?? ""] ?? { candidates: [], failures: [] });
		}
		if (asked?.[2]) {
			return this.applyCandidate(asked[1] ?? "", body as CandidateApply);
		}
		if (method !== "GET") {
			return this.editBook(method, path, body as BookEdit | undefined);
		}
		if (path === "/books/search") {
			return Response.json({ books: this.search(query.get("q") ?? "") });
		}
		if (path === "/books/facets") {
			return Response.json({ facets: this.facets(query) });
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
		const tree = query.get("filter");
		const sorted = this.books
			.filter((b) => !query.get("library") || b.libraryId === query.get("library"))
			.filter((b) => !tree || matches(b, JSON.parse(tree)))
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

	/** Names in use on the books, by the beginning of a word. */
	private names(kind: string, typed: string): string[] {
		const all = this.books.flatMap((b) =>
			kind === "author"
				? b.contributors.map((c) => c.name)
				: kind === "tag"
					? b.tags
					: [kind === "series" ? b.series : b.publisher].filter((n): n is string => !!n),
		);
		const q = typed.toLowerCase();
		return [...new Set(all)].filter((n) => n.toLowerCase().split(/\s+/).some((w) => w.startsWith(q)));
	}

	/**
	 * A bulk edit sets the series and adds and removes tags, leaving locked
	 * fields unless told otherwise; the other actions change nothing here.
	 */
	private answerBulk(path: string, req: BulkRequest): Response {
		if (!this.session?.permissions.includes("metadata:edit")) {
			return Response.json({ error: { code: "forbidden", message: "You may not do this." } }, { status: 403 });
		}
		if (path !== "/books/bulk") {
			const status = this.bulks.find((b) => `/bulk/${b.id}` === path);
			return status
				? Response.json(status)
				: Response.json({ error: { code: "not_found", message: "There is no such bulk change." } }, { status: 404 });
		}
		const chosen = this.books.filter((b) =>
			req.books?.length
				? req.books.includes(b.id)
				: (!req.library || b.libraryId === req.library) && (!req.filter || matches(b, JSON.parse(req.filter))),
		);
		const change = req.change ?? {};
		const results: BulkResult[] = chosen.map((book) => {
			const skipped = (["series", "tags"] as const).filter(
				(f) => (f === "series" ? change.series !== undefined : !!change.addTags || !!change.removeTags) &&
					book.fields[f]?.locked && !change.includeLocked,
			);
			let changed = false;
			if (change.series !== undefined && !skipped.includes("series") && book.series !== change.series) {
				book.series = change.series || undefined;
				book.fields.series = { source: "manual", locked: true };
				changed = true;
			}
			if ((change.addTags || change.removeTags) && !skipped.includes("tags")) {
				const tags = [...book.tags, ...(change.addTags ?? [])].filter((t) => !change.removeTags?.includes(t));
				changed ||= tags.join() !== book.tags.join();
				book.tags = [...new Set(tags)];
				book.fields.tags = { source: "manual", locked: true };
			}
			const outcome = changed ? "changed" : skipped.length > 0 ? "locked" : "unchanged";
			return { bookId: book.id, title: book.title, outcome, skipped };
		});
		const counts: Record<string, number> = {};
		for (const r of results) {
			counts[r.outcome ?? ""] = (counts[r.outcome ?? ""] ?? 0) + 1;
		}
		const id = `bulk-${this.bulks.length + 1}`;
		this.bulks.push({
			id, action: req.action, createdAt: "2026-01-04T00:00:00Z", finishedAt: "2026-01-04T00:00:01Z",
			total: results.length, done: results.length, counts, books: results,
		});
		return Response.json({ id, total: results.length }, { status: 202 });
	}

	private answerMatches(method: string, path: string, body: CandidateApply): Response {
		if (!this.session?.permissions.includes("metadata:edit")) {
			return Response.json({ error: { code: "forbidden", message: "You may not do this." } }, { status: 403 });
		}
		if (method === "GET") {
			return Response.json({ books: this.review, total: this.review.length });
		}
		const [, , id, action] = path.split("/");
		const item = this.review.find((b) => b.matches.some((m) => m.matchId === id));
		const match = item?.matches.find((m) => m.matchId === id);
		if (!item || !match) {
			return Response.json({ error: { code: "not_found", message: "There is no such match waiting." } }, { status: 404 });
		}
		if (action === "reject") {
			item.matches = item.matches.filter((m) => m !== match);
		} else {
			this.applyCandidate(item.book.id, { ...body, provider: match.provider });
			item.matches = [];
		}
		this.review = this.review.filter((b) => b.matches.length > 0);
		return action === "reject"
			? new Response(null, { status: 204 })
			: Response.json(this.books.find((b) => b.id === item.book.id));
	}

	/** Takes a candidate's values the way the server does: locked fields stay. */
	private applyCandidate(id: string, body: CandidateApply): Response {
		const book = this.books.find((b) => b.id === id);
		if (!book) {
			return Response.json({ error: { code: "not_found", message: "There is no such book." } }, { status: 404 });
		}
		const { provider, coverToken, ...values } = body;
		const fields = Object.keys(values).filter((f) => !book.fields[f]?.locked);
		const edit: Record<string, unknown> = {};
		for (const f of fields) edit[f] = values[f as keyof typeof values];
		this.editBook("PATCH", `/books/${id}`, edit as BookEdit);
		for (const f of fields) {
			book.fields[f] = { source: "provider", detail: provider, locked: false };
		}
		if (coverToken && !book.fields.cover?.locked) {
			book.coverKey = `cover-${coverToken}`;
			book.fields.cover = { source: "provider", detail: provider, locked: false };
		}
		return Response.json(book);
	}

	private editBook(method: string, path: string, edit: BookEdit | undefined): Response {
		const refuse = (status: number, code: string, message: string, fields?: Record<string, string>) =>
			Response.json({ error: { code, message, fields } }, { status });
		if (!this.session?.permissions.includes("metadata:edit")) {
			return refuse(403, "forbidden", "You may not do this.");
		}
		const match = /^\/books\/([^/]+)(\/cover)?$/.exec(path);
		const book = this.books.find((b) => b.id === match?.[1]);
		if (!match || !book) {
			return refuse(404, "not_found", "There is no such book.");
		}
		const typed = (field: string) => {
			book.fields[field] = { source: "manual", locked: true };
		};
		if (match[2]) {
			book.coverKey = method === "PUT" ? `cover-${this.requests.length}` : undefined;
			typed("cover");
			return Response.json(book);
		}
		const e = edit ?? {};
		const problems: Record<string, string> = {};
		if (e.title !== undefined && e.title.trim() === "") problems.title = "A book needs a title.";
		if (e.language && !/^[a-z]{2,3}(-[a-z0-9]{2,8})*$/i.test(e.language)) {
			problems.language = "Give a language code such as en, de or pt-BR.";
		}
		if (Object.keys(problems).length > 0) {
			return refuse(422, "validation_failed", "Some fields need attention.", problems);
		}
		const text = ["title", "subtitle", "description", "language", "published", "publisher"] as const;
		for (const key of text) {
			const value = e[key];
			if (value !== undefined) {
				if (key === "title") book.title = value;
				else book[key] = value || undefined;
				typed(key);
			}
		}
		if (e.series) {
			book.series = e.series.name || undefined;
			book.seriesIndex = e.series.name ? e.series.index : undefined;
			typed("series");
		}
		if (e.pageCount !== undefined) {
			book.pageCount = e.pageCount || undefined;
			typed("pageCount");
		}
		if (e.contributors) {
			book.contributors = e.contributors;
			typed("contributors");
		}
		if (e.tags) {
			book.tags = e.tags;
			typed("tags");
		}
		if (e.identifiers) {
			book.identifiers = [
				...book.identifiers.filter((i) => i.fromFile),
				...e.identifiers.map((i) => ({ ...i, fromFile: false })),
			];
			typed("identifiers");
		}
		for (const [field, on] of Object.entries(e.locks ?? {})) {
			book.fields[field] = { ...book.fields[field], locked: on };
		}
		return Response.json(book);
	}

	private answerAccounts(
		method: string,
		path: string,
		body: Record<string, unknown>,
		refuse: (status: number, code: string, message: string, fields?: Record<string, string>) => Response,
	): Response {
		if (!this.session) {
			return refuse(401, "unauthorized", "Sign in to continue.");
		}
		const me = this.accounts.get(this.session.username.toLowerCase());
		if (path === "/auth/password") {
			if (!me || body.currentPassword !== me.password) {
				return refuse(422, "validation_failed", "Some fields need attention.", { currentPassword: "That is not your current password." });
			}
			if (String(body.newPassword ?? "").length < 8) {
				return refuse(422, "validation_failed", "Some fields need attention.", { newPassword: "A password has at least 8 characters." });
			}
			me.password = String(body.newPassword);
			this.sessions = this.sessions.filter((s) => s.current);
			return new Response(null, { status: 204 });
		}
		if (path === "/auth/storage") {
			return Response.json({ usedBytes: me?.usedBytes ?? 0, quotaBytes: me?.quotaBytes });
		}
		if (path === "/auth/sessions") {
			return Response.json({ sessions: this.sessions });
		}
		if (path.startsWith("/auth/sessions/") && method === "DELETE") {
			this.sessions = this.sessions.filter((s) => `/auth/sessions/${s.id}` !== path);
			return new Response(null, { status: 204 });
		}

		if (!this.session.permissions.includes("users:manage")) {
			return refuse(403, "forbidden", "You do not have permission to do that.");
		}
		const view = (a: Account) => ({
			...a.user,
			disabled: !!a.disabled,
			createdAt: "2026-01-01T00:00:00Z",
			sessions: a.disabled ? 0 : 1,
			lastSeenAt: "2026-01-02T00:00:00Z",
			usedBytes: a.usedBytes ?? 0,
			quotaBytes: a.quotaBytes,
		});
		if (path === "/users" && method === "GET") {
			return Response.json({ users: [...this.accounts.values()].map(view) });
		}
		if (path === "/users" && method === "POST") {
			const fields: Record<string, string> = {};
			if (String(body.password ?? "").length < 8) {
				fields.password = "A password has at least 8 characters.";
			}
			if (!String(body.username ?? "").trim()) {
				fields.username = "Choose a user name.";
			}
			if (Object.keys(fields).length > 0) {
				return refuse(422, "validation_failed", "Some fields need attention.", fields);
			}
			if (this.accounts.has(String(body.username).toLowerCase())) {
				return refuse(409, "conflict", "That user name or e-mail address is already in use.");
			}
			this.withAccount(String(body.username), String(body.password), body.role as "admin" | "editor" | "reader");
			return Response.json(view(this.accounts.get(String(body.username).toLowerCase()) as Account), { status: 201 });
		}
		const target = [...this.accounts.values()].find((a) => path.startsWith(`/users/${a.user.id}`));
		if (!target) {
			return refuse(404, "not_found", "There is no such user.");
		}
		if (path.endsWith("/sessions")) {
			return new Response(null, { status: 204 });
		}
		if (path.endsWith("/quota")) {
			target.quotaBytes = (body.quotaBytes as number | null) ?? undefined;
			return Response.json(view(target));
		}
		const role = (body.role as CurrentUser["role"] | undefined) ?? target.user.role;
		const disabled = (body.disabled as boolean | undefined) ?? target.disabled;
		const admins = [...this.accounts.values()].filter((a) => a.user.role === "admin" && !a.disabled);
		if (admins.length === 1 && admins[0] === target && (role !== "admin" || disabled)) {
			return refuse(409, "conflict", "This is the last administrator who can sign in. Make another account an administrator first.");
		}
		if (body.password !== undefined) {
			if (String(body.password).length < 8) {
				return refuse(422, "validation_failed", "Some fields need attention.", { password: "A password has at least 8 characters." });
			}
			target.password = String(body.password);
		}
		target.user = user(target.user.username, role);
		target.disabled = disabled;
		return Response.json(view(target));
	}

	private answerSettings(
		method: string,
		body: { values?: Record<string, string | null> },
		refuse: (status: number, code: string, message: string, fields?: Record<string, string>) => Response,
	): Response {
		if (!this.session) {
			return refuse(401, "unauthorized", "Sign in to continue.");
		}
		if (!this.session.permissions.includes("settings:manage")) {
			return refuse(403, "forbidden", "You do not have permission to do that.");
		}
		if (method === "PATCH") {
			const values = body.values ?? {};
			const language = values["metadata.language"];
			if (language && !/^[a-z]{2,3}$/i.test(language.trim())) {
				return refuse(422, "validation_failed", "Some fields need attention.", {
					"metadata.language": "Give a language as its two-letter code, such as en or de.",
				});
			}
			for (const [key, value] of Object.entries(values)) {
				const setting = this.settings.find((s) => s.key === key);
				if (!setting) {
					continue;
				}
				const unset = value === null || value.trim() === "";
				setting.isSet = !unset;
				setting.updatedAt = unset ? undefined : "2026-01-03T09:00:00Z";
				if (setting.kind === "secret") {
					if (unset) {
						this.secrets.delete(key);
					} else {
						this.secrets.set(key, value.trim());
					}
				} else {
					setting.value = unset ? "en" : value.trim().toLowerCase();
				}
			}
		}
		return Response.json({ settings: this.settings });
	}

	/**
	 * Titles and authors that contain the words, or a word one letter off:
	 * close enough to the server's trigrams for what the tests type.
	 */
	private search(words: string) {
		const q = nameKey(words);
		if (q.replace(/ /g, "").length < 3) {
			return [];
		}
		const near = (text: string) =>
			nameKey(text).includes(q) || nameKey(text).split(" ").some((w) => oneOff(w, q));
		return this.books.flatMap((b) => {
			const author = b.contributors.find((c) => c.role === "author" && near(c.name));
			const match = near(b.title) ? "title" : author ? "author" : undefined;
			return match
				? [{ id: b.id, libraryId: b.libraryId, title: b.title, authors: b.contributors.filter((c) => c.role === "author").map((c) => c.name), formats: [], addedAt: b.addedAt, match }]
				: [];
		});
	}

	/** The facets the server would count, from the same books. */
	private facets(query: URLSearchParams) {
		const tree: Rule = JSON.parse(query.get("filter") || "{}");
		return FIELDS.map((field) => {
			// A field's own rules do not narrow its counts.
			const without: Rule = { all: (tree.all ?? []).filter((r) => fieldsOf(r).join() !== field) };
			const counts = new Map<string, { label: string; count: number }>();
			for (const b of this.books) {
				if (query.get("library") && b.libraryId !== query.get("library")) {
					continue;
				}
				if (!matches(b, without)) {
					continue;
				}
				for (const [value, label] of valuesOf(b, field)) {
					const entry = counts.get(value) ?? { label, count: 0 };
					entry.count++;
					counts.set(value, entry);
				}
			}
			return {
				field,
				values: [...counts].map(([value, { label, count }]) => ({ value, label, count }))
					.sort((a, b) => b.count - a.count || a.value.localeCompare(b.value)),
			};
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

type Field = "author" | "series" | "tag" | "language" | "published" | "format";
const FIELDS: Field[] = ["author", "series", "tag", "language", "published", "format"];

interface Rule {
	all?: Rule[];
	any?: Rule[];
	not?: Rule;
	field?: Field;
	op?: string;
	values?: string[];
}

/** A name as the server compares it, near enough for the tests. */
function nameKey(name: string): string {
	return name
		.normalize("NFKD")
		.replace(/\p{Mn}/gu, "")
		.toLowerCase()
		.replace(/[^\p{L}\p{N}]+/gu, " ")
		.trim();
}

/** Whether two words differ by one letter put in, left out or changed. */
function oneOff(a: string, b: string): boolean {
	if (Math.abs(a.length - b.length) > 1) {
		return false;
	}
	let i = 0;
	while (i < a.length && a[i] === b[i]) {
		i++;
	}
	return (
		a.slice(i + 1) === b.slice(i + 1) ||
		a.slice(i) === b.slice(i + 1) ||
		a.slice(i + 1) === b.slice(i)
	);
}

/** The values a book has for a field, as [value, label] pairs. */
function valuesOf(b: BookDetail, field: Field): [string, string][] {
	switch (field) {
		case "author":
			return b.contributors.filter((c) => c.role === "author").map((c) => [nameKey(c.name), c.name]);
		case "series":
			return b.series ? [[nameKey(b.series), b.series]] : [];
		case "tag":
			return b.tags.map((tag) => [nameKey(tag), tag]);
		case "language": {
			const lang = b.language?.split("-")[0]?.toLowerCase();
			return lang ? [[lang, lang]] : [];
		}
		case "published": {
			const decade = b.published ? String(Math.floor(Number(b.published.slice(0, 4)) / 10) * 10) : undefined;
			return decade ? [[decade, decade]] : [];
		}
		case "format":
			return [...new Set(b.files.map((f) => f.format))].map((f) => [f, f]);
	}
}

function fieldsOf(r: Rule): string[] {
	return [...new Set([r.field ?? "", ...(r.all ?? []).flatMap(fieldsOf), ...(r.any ?? []).flatMap(fieldsOf)])]
		.filter(Boolean);
}

function matches(b: BookDetail, r: Rule): boolean {
	if (r.all) {
		return r.all.every((c) => matches(b, c));
	}
	if (r.any) {
		return r.any.some((c) => matches(b, c));
	}
	if (r.not) {
		return !matches(b, r.not);
	}
	if (!r.field) {
		return true;
	}
	const have = valuesOf(b, r.field).map(([v]) => v);
	if (r.op === "between") {
		const year = Number(b.published?.slice(0, 4));
		const [from, to] = r.values ?? [];
		return !!b.published && (!from || year >= Number(from)) && (!to || year <= Number(to));
	}
	return (r.values ?? []).some((v) => have.includes(r.field === "format" ? v : nameKey(v)));
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
