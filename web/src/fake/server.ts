import type { components } from "@/api/schema";
import type { CurrentUser, SignInMethods } from "@/auth/session";
import type { BookDetail, BookSummary, ListedBook, NamedEntry, NameHit, TextHit } from "@/books/api";
import type {
	Collection,
	CollectionAdd,
	CollectionRequest,
} from "@/books/collections";
import type {
	Notification,
	NotificationSetting,
} from "@/notifications/api";
import type { SmartShelf, SmartShelfRequest } from "@/books/smart-shelves";
import type { BulkRequest, BulkResult, BulkStatus } from "@/books/bulk";
import type {
	CleanupKind,
	CleanupPreviewBook,
	CleanupSuggestion,
} from "@/books/cleanup";
import type { GenreSuggestion } from "@/books/genres";
import type { Continuation } from "@/books/continue-offer";
import type { Progress, ProgressState, ProgressUpdate } from "@/books/progress";
import type { ReadingStats } from "@/books/stats";
import type { ReadingImport } from "@/books/history";
import type { RecommendationList } from "@/books/recommendations";
import type { WishRequest } from "@/books/wishes";
import type { Timeline } from "@/player/timeline";
import type { ReadingBulk, ReadingChange, ReadingStatus } from "@/books/reading";
import type {
	BookEdit,
	Candidate,
	CandidateApply,
	ReviewBook,
} from "@/books/edit";
import type { Uploaded } from "@/books/upload";
import type { Webhook, WebhookDelivery, WebhookRequest } from "@/webhooks/api";
import type { Library, Scan } from "@/libraries/api";
import type { AutoCleanupCounts, AutoMatchCounts } from "@/curation/auto";
import type { Setting } from "@/settings/api";
import type { Job } from "@/jobs/api";
import type { CurationAction, CurationRun, DuplicateClassCount } from "@/curation/api";
import { type DuplicatePair, overlapOf } from "@/duplicates/api";
import type { TrashedFile } from "@/trash/api";
import type { LinkedIdentity } from "@/users/api";
import type { Feed, FeedItem, Release, Tracker } from "@/releases/api";
import type { SystemCheck } from "@/system/checks";
import type { Health } from "@/system/health";
import type { ReleaseStatus } from "@/system/update";

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
	/** Folders a library cannot be made writable on, as a :ro mount. */
	readOnlyFolders: string[] = [];
	/** The user IDs of each private library's members, by library ID. */
	members = new Map<string, string[]>();
	books: BookDetail[] = [];
	listed: (ListedBook & { series: string })[] = [];
	/** What the next scan of a library finds; a scan is finished at once. */
	scanFinds: Partial<Scan> = { filesSeen: 0 };
	/** Set to make every request fail as if the server were unreachable. */
	down = false;
	/** The build the server says it runs. */
	build: { version: string; commit?: string } = { version: "0.5.0", commit: "8c98a9d0e1f2" };
	/**
	 * E-readers asking to be added. Accepting one adds it to the devices at
	 * once, as its app would come for the token.
	 */
	deviceRequests: {
		id: string;
		deviceName: string;
		code: string;
		address: string;
		createdAt: string;
		expiresAt: string;
	}[] = [];
	/** The signed-in person's e-readers, as the profile page lists them. */
	devices: {
		id: string;
		name: string;
		createdAt: string;
		lastUsedAt?: string;
		canUpload?: boolean;
	}[] = [];
	/** The signed-in person's API keys. */
	apiKeys: {
		id: string;
		name: string;
		prefix: string;
		scopes: string[];
		createdAt: string;
		lastUsedAt?: string;
		expiresAt?: string;
	}[] = [];
	/** Books sent to the person's e-readers. */
	deliveries: {
		id: string;
		bookId: string;
		deviceId: string;
		deviceName: string;
		queuedAt: string;
		deliveredAt?: string;
		/** When a Kobo reader's sync told it of the book. */
		syncedAt?: string;
		sentFixed?: boolean;
	}[] = [];
	/** Sending to a Kindle: the mail server's state, and what was sent. */
	kindle = {
		available: false,
		sender: "library@example.org",
		address: "",
		maxBytes: 50 * 1024 * 1024,
		format: "epub",
	};
	kindleSends: { id: string; bookId: string; state: string; message?: string; address: string; createdAt: string; finishedAt?: string }[] = [];
	/** The signed-in person's sessions, as the profile page lists them. */
	sessions = [
		{
			id: "s-1",
			userAgent:
				"Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0",
			createdAt: "2026-01-01T08:00:00Z",
			lastSeenAt: "2026-01-03T08:00:00Z",
			current: true,
		},
		{
			id: "s-2",
			userAgent:
				"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile Safari/604.1",
			createdAt: "2026-01-01T08:00:00Z",
			lastSeenAt: "2026-01-02T08:00:00Z",
			current: false,
		},
	];
	/** The jobs the jobs page lists, newest first. */
	jobs: Job[] = [];
	/** The books whose matches wait for review. */
	review: ReviewBook[] = [];
	/** What the metadata providers know, by book. */
	candidates: Record<
		string,
		{
			candidates: Candidate[];
			failures: { provider: string; message: string }[];
		}
	> = {};
	/** How far the books are embedded, as the jobs page shows it. */
	embedding = {
		model: "intfloat/multilingual-e5-small",
		enabled: true,
		books: 120,
		withText: 100,
		metadata: 80,
		content: 60,
		/** Left out while passages are off, as the server does. */
		passages: undefined as { withText: number; withPassages: number; bytes: number } | undefined,
	};
	/** The models embedding.model may name, as the settings page shows them. */
	embeddingModels = {
		books: 120,
		withText: 100,
		models: [
			{
				name: "multilingual-e5-small",
				model: "intfloat/multilingual-e5-small",
				weights: "int8",
				dimensions: 384,
				diskBytes: 135_429_554,
				memoryBytes: 734_003_200,
				readiness: { state: "ready" as const, onServer: false },
				chosen: true,
				metadata: 80,
				content: 60,
				storedBytes: 135_429_554,
				databaseBytes: 260_000,
				unusedSince: undefined as string | undefined,
				removeAt: undefined as string | undefined,
			},
			{
				name: "multilingual-e5-small-fp32",
				model: "intfloat/multilingual-e5-small",
				weights: "fp32",
				dimensions: 384,
				diskBytes: 487_351_240,
				memoryBytes: 1_153_433_600,
				readiness: { state: "download" as const, onServer: false },
				chosen: false,
				metadata: 0,
				content: 0,
				storedBytes: 0,
				databaseBytes: 0,
				unusedSince: undefined as string | undefined,
				removeAt: undefined as string | undefined,
			},
			{
				name: "bge-m3",
				model: "BAAI/bge-m3",
				weights: "bge-m3",
				dimensions: 1024,
				diskBytes: 2_284_693_904,
				memoryBytes: 3_221_225_472,
				readiness: { state: "notDownloaded" as const, onServer: false },
				chosen: false,
				metadata: 110,
				content: 95,
				storedBytes: 2_284_693_904,
				databaseBytes: 1_100_000,
				unusedSince: "2026-10-05T08:00:00Z" as string | undefined,
				removeAt: "2026-10-06T08:00:00Z" as string | undefined,
			},
		],
	};
	/** The models whose removal was asked for, in order. */
	removedModels: string[] = [];
	/** What the overview page counts. */
	figures = {
		books: 1200,
		files: 1500,
		bytes: 3_400_000_000,
		audiobooks: 12,
		audioHours: 140,
		pages: 350_000,
		authors: 480,
		series: 90,
		publishers: 60,
		tags: 30,
		withoutCover: 7,
		withoutAuthor: 3,
		withoutText: 15,
		withMissingFiles: 0,
		withEreaderFindings: 0,
		ereaderKinds: [] as { name: string; count: number }[],
		formats: [
			{ name: "epub", count: 1100, bytes: 2_000_000_000 },
			{ name: "pdf", count: 400, bytes: 1_400_000_000 },
		],
		languages: [
			{ name: "de", count: 900 },
			{ name: "", count: 300 },
		],
		months: [{ name: "2026-10", count: 1200 }],
	};
	/** How far the background work is, as the overview page shows it. */
	/** What the administrators' details list. */
	checks: SystemCheck[] = [
		{ name: "database", status: "ok", message: "", value: 1.5, unit: "ms", checkedAt: "2026-11-09T10:00:00Z" },
		{ name: "jobs", status: "ok", message: "", value: 0.2, unit: "ms", checkedAt: "2026-11-09T10:00:00Z" },
		{ name: "outbox", status: "degraded", message: "12 changes wait for the search index, the oldest for 15m0s", value: 900, unit: "s", checkedAt: "2026-11-09T10:00:00Z" },
		{ name: "provider:openlibrary", status: "ok", message: "", checkedAt: "2026-11-09T10:00:00Z" },
	];
	update: ReleaseStatus = {
		current: "0.5.0",
		enabled: true,
		security: false,
		available: false,
	};
	health: Health = {
		memory: {
			machineBytes: 8_000_000_000,
			limited: false,
			cpus: 4,
			appBytes: 536_870_912,
			embeddingModel: "intfloat/multilingual-e5-small",
			embeddingBytes: 734_003_200,
			needBytes: 1_600_000_000,
			budgetBytes: 6_400_000_000,
		},
		postgres: {
			sharedBuffersBytes: 134_217_728,
			workMemBytes: 4_194_304,
			maintenanceWorkMemBytes: 67_108_864,
			effectiveCacheSizeBytes: 4_294_967_296,
			maxConnections: 100,
			connections: 16,
			maxParallelWorkers: 4,
			maxParallelWorkersPerGather: 2,
			maxParallelMaintenanceWorkers: 2,
			needBytes: 268_435_456,
		},
		disk: {
			path: "/data",
			freeBytes: 80_000_000_000,
			totalBytes: 110_000_000_000,
			databaseBytes: 2_000_000_000,
			indexBytes: 300_000_000,
			needFreeBytes: 11_000_000_000,
		},
		watches: {
			watches: 120,
			limit: 65_536,
			notified: 1,
			polled: 0,
			limitReached: false,
		},
		warnings: [],
	};
	work = {
		files: { done: 1300, total: 1500, lastHour: 400, minutesLeft: 30 },
		filesFailed: 4,
		texts: { done: 1000, total: 1000, lastHour: 0 },
		model: "intfloat/multilingual-e5-small",
		vectors: { done: 600, total: 1200, lastHour: 120, minutesLeft: 300 },
		content: { done: 500, total: 1000, lastHour: 0 },
		lookups: { waiting: 0, lastHour: 0 },
		matchesPending: 5,
		checks: { waiting: 30, lastHour: 60, minutesLeft: 30 },
		pairsOpen: 2,
	};
	/** Whether the embedding model runs, for the search by description. */
	describable = true;
	/** What the clean-up page suggests. */
	cleanup: CleanupSuggestion[] = [];
	/** What an automatic clean-up would apply, by kind. */
	autoCleanup: AutoCleanupCounts = { kinds: [] };
	/** What a rule over the matches would do, and the groups of the rest. */
	autoMatches: AutoMatchCounts = { accept: 0, settle: 0, dismiss: 0, books: 0, capped: false, groups: [] };
	/** What applying a clean-up suggestion does to its books, by `kind:subject`. */
	cleanupPreviews: Record<string, CleanupPreviewBook[]> = {};
	/** The genres suggested for books that wait for a person. */
	genreSuggestions: GenreSuggestion[] = [];
	/** The bulk changes asked for, each finished as soon as it is asked for. */
	bulks: BulkStatus[] = [];
	/** Where the signed-in person is in each book. */
	progress: Record<string, ProgressState> = {};
	/** What each book offers to go on with in the other medium; none by default. */
	continuations: Record<string, Continuation> = {};
	/**
	 * The passages of the books' text the full-text search finds: one is
	 * found when its text holds the words, its book with the library's
	 * filter.
	 */
	passages: { bookId: string; hit: TextHit }[] = [];
	/** The pairs of books that look like one, strongest first. */
	pairs: DuplicatePair[] = [];
	/** Books merged away, and the book each lives on as. */
	mergedInto: Record<string, string> = {};
	/** Set to refuse replacing, as a folder GOtome cannot write to does. */
	replaceRefused?: string;
	/** The classes of pairs the rules resolve, with their open pairs. */
	autoClasses: DuplicateClassCount[] = [
		{ class: "same_text", pairs: 3, merges: true, daily: false },
		{ class: "overlap_high", pairs: 5, merges: true, daily: true },
		{ class: "overlap_mid", pairs: 0, merges: true, daily: false },
		{ class: "contained", pairs: 2, merges: false, daily: false },
		{ class: "part_inside", pairs: 1, merges: false, daily: false },
	];
	/** The runs of curation, the newest first, and each one's actions. */
	runs: CurationRun[] = [];
	runActions: Record<string, CurationAction[]> = {};

	/** Adds an open pair of two of the books, with its evidence. */
	withPair(id: string, a: string, b: string, evidence: DuplicatePair["evidence"]): this {
		const books = [a, b].map((bookId) => {
			const book = this.books.find((x) => x.id === bookId);
			if (!book) throw new Error(`no book ${bookId}`);
			return summaryOf(book);
		});
		const score = Math.max(...evidence.map((e) => e.score));
		const contained = evidence.every(
			(e) => e.kind === "overlap" && (overlapOf(e.detail)?.jaccard ?? 1) < 0.5,
		);
		this.pairs.push({ id, state: "open", score, contained, foundAt: "2026-01-02T00:00:00Z", books, evidence });
		return this;
	}

	/** The files in the trash, with the record each came from. */
	trashed: { view: TrashedFile; file: BookDetail["files"][number] }[] = [];
	/** How much of the text search knows; rebuilding and rereading change it. */
	searchStatus = { files: 4, indexed: 4, rebuilding: false, engine: "0.25.11" };
	/** The signed-in person's statistics, the same whatever the window. */
	stats?: ReadingStats;
	/** The signed-in person's play queue. */
	queue: string[] = [];
	/** What a Goodreads or StoryGraph export would do, as the preview of an import says it. */
	importPreview?: ReadingImport;
	/** What the person is recommended; none, with no profile, unless a test says. */
	recommendations?: RecommendationList;
	/** The audio of each book, as the player asks for it. */
	audio: Record<string, Timeline> = {};
	/** The IDs of the books like each book, the nearest first. */
	similar: Record<string, string[]> = {};
	/** A further position another device wrote, which the next save meets. */
	furtherProgress?: Progress;
	/** The collections the signed-in person sees, with their books in order. */
	collections: (Omit<Collection, "books" | "hasBook"> & { books: string[] })[] =
		[];
	/** The smart shelves the signed-in person sees; what is on them is worked out when asked. */
	smartShelves: Omit<SmartShelf, "books">[] = [];
	/** The signed-in person's notifications, newest first. */
	notifications: Notification[] = [];
	/** What the signed-in person chose to be told of, as a reader may. */
	notificationSettings: NotificationSetting[] = [
		{ kind: "books.added", app: true },
		{ kind: "wish.fulfilled", app: true },
	];
	/** The webhooks, the secrets they were made with, and what was sent. */
	webhooks: Webhook[] = [];
	webhookSecrets = new Map<string, string>();
	webhookDeliveries = new Map<string, WebhookDelivery[]>();
	/** The authors and series the signed-in person follows. */
	trackers: Tracker[] = [];
	/** The new books of what they follow, as the server would list them. */
	releases: { upcoming: Release[]; recent: Release[] } = { upcoming: [], recent: [] };
	/** The feeds the signed-in person follows, and their matching items. */
	feeds: Feed[] = [];
	feedItems: FeedItem[] = [];
	/** How one may sign in; a test gives `sso` to offer an identity provider. */
	signInMethods: SignInMethods = { password: true };
	/** The signed-in person's accounts at the identity provider. */
	identities: LinkedIdentity[] = [];
	/** Files somebody asked to have read again. */
	reread: string[] = [];
	/** The fixes asked for, by file. */
	fixes: { file: string; findings: string[] }[] = [];
	/** The secrets as they were sent, which the fake keeps and never sends back. */
	secrets = new Map<string, string>();
	private settings: Setting[] = [
		{ key: "metadata.language", kind: "text", value: "en", isSet: false },
		{ key: "metadata.googleBooksKey", kind: "secret", isSet: false },
		{ key: "metadata.hardcoverToken", kind: "secret", isSet: false },
		{
			key: "metadata.providers",
			kind: "text",
			value: "openlibrary,crossref,hardcover",
			isSet: false,
			choices: ["openlibrary", "crossref", "hardcover"],
			multiple: true,
		},
		{
			key: "embedding.model",
			kind: "text",
			value: "multilingual-e5-small",
			isSet: false,
			choices: ["bge-m3", "multilingual-e5-small", "multilingual-e5-small-fp32"],
		},
		{
			key: "genres.suggest",
			kind: "text",
			value: "off",
			isSet: false,
			choices: ["off", "tags", "vector", "neighbours", "combined"],
		},
		{
			key: "auth.passwords",
			kind: "text",
			value: "on",
			isSet: false,
			choices: ["on", "off"],
		},
	];
	/** The match score as the settings page shows it; a run starts waiting and is finished by the test. */
	scoring: components["schemas"]["ScoringStatus"] = {
		weights: "learned",
		using: "handSet",
		booksWithIsbn: 1200,
		learnFrom: 500,
	};
	/** The weights imported, as the page sent them. */
	importedWeights: unknown;
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
			joinFormats: false,
			watch: true,
			fulltext: true,
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
	withBook(
		title: string,
		overrides: Partial<BookDetail> = {},
		libraryName?: string,
	): this {
		const library =
			this.libraries.find((l) => l.name === libraryName) ?? this.libraries[0];
		const n = this.books.length + 1;
		this.books.push({
			id: `book-${n}`,
			libraryId: library?.id ?? "lib-1",
			title,
			tags: [],
			identifiers: [],
			files: [],
			fields: {},
			reading: { status: "unread" },
			placeholder: false,
			recordings: [],
			forms: [],
			joins: [],
			addedAt: `2026-01-01T00:00:${String(n % 60).padStart(2, "0")}Z`,
			...overrides,
			contributors: (overrides.contributors ?? []).map((c) => ({
				...c,
				key: personKey(c.name),
			})),
			seriesKey: overrides.series ? nameKey(overrides.series) : undefined,
		});
		return this;
	}

	/** Has the sources list a book of the series that the library lacks. */
	withListed(series: string, book: Partial<ListedBook> & { title: string }): this {
		this.listed.push({
			series,
			authors: [],
			source: "openlibrary",
			isbns: [],
			wishable: true,
			wished: false,
			...book,
		});
		return this;
	}

	signedInAs(username: string): this {
		this.session = this.accounts.get(username.toLowerCase())?.user ?? null;
		return this;
	}

	/** Answers one request as the server would; the tests and the demo both route fetch here. */
	async handle(input: RequestInfo | URL): Promise<Response> {
		if (this.down) {
			throw new TypeError("Failed to fetch");
		}
		const request =
			input instanceof Request
				? input
				: new Request(new URL(String(input), window.location.href));
		const url = new URL(request.url);
		const path = url.pathname.replace(/^\/api\/v1/, "");
		const body: unknown =
			request.method === "GET"
				? undefined
				: await request.json().catch(() => undefined);
		this.requests.push({
			method: request.method,
			path: path + url.search,
			body,
		});
		if (path === "/search") {
			return this.searchText(url.searchParams);
		}
		if (path === "/search/similar") {
			if (!this.describable) {
				return Response.json(
					{ error: { code: "unavailable", message: "Searching by description needs the embedding model, which cannot run here yet." } },
					{ status: 503 },
				);
			}
			const words = (url.searchParams.get("q") ?? "")
				.toLowerCase()
				.split(/\s+/)
				.filter((w) => w.length > 2);
			const nearness = (b: BookDetail) =>
				words.filter((w) => `${b.title} ${b.description ?? ""}`.toLowerCase().includes(w)).length;
			return Response.json({
				books: this.books
					.filter((b) => !b.placeholder && nearness(b) > 0)
					.sort((a, b) => nearness(b) - nearness(a))
					.map(summaryOf),
			});
		}
		if (path === "/duplicates") {
			const q = url.searchParams;
			const least = Number(q.get("least") ?? 0) / 100;
			const pairs = this.pairs.filter(
				(p) =>
					p.state === (q.get("state") ?? "open") &&
					p.score >= least &&
					(!q.get("contained") || p.contained === (q.get("contained") === "true")) &&
					(!q.get("kind") || p.evidence.some((e) => e.kind === q.get("kind"))),
			);
			return Response.json({ pairs });
		}
		if (path === "/curation/duplicates") {
			if (request.method === "GET") {
				return Response.json({ classes: this.autoClasses });
			}
			const asked = body as { classes: DuplicateClassCount["class"][]; dryRun: boolean };
			const pairs = this.autoClasses
				.filter((c) => asked.classes.includes(c.class))
				.reduce((n, c) => n + c.pairs, 0);
			const id = `run-${this.runs.length + 1}`;
			this.runs.unshift({
				id, kind: "duplicates", rule: "auto-duplicates", dryRun: asked.dryRun, automatic: true,
				state: "done", createdAt: "2026-11-10T08:00:00Z", actions: pairs, undone: 0, skipped: 0, undoing: false,
			});
			this.runActions[id] = Array.from({ length: pairs }, (_, i) => ({
				id: `${id}-action-${i + 1}`, runId: id, kind: asked.dryRun ? "replace" : "merge", rule: asked.classes[0],
				reason: "same bytes or same text; kept one book because it is older", state: "done", trashedFiles: 0,
				createdAt: "2026-11-10T08:00:00Z",
				books: [{ id: "book-1", title: "Emma", changes: asked.dryRun ? [] : [{ field: "files", before: "1", after: "2" }] }],
			}));
			return Response.json({ runId: id, dryRun: asked.dryRun, pairs }, { status: 202 });
		}
		if (path === "/curation/runs") {
			return Response.json({ runs: this.runs });
		}
		const runPath = /^\/curation\/runs\/([^/]+)(\/undo)?$/.exec(path);
		if (runPath) {
			const run = this.runs.find((r) => r.id === runPath[1]);
			if (!run) {
				return Response.json({ error: { code: "not_found", message: "There is no such run." } }, { status: 404 });
			}
			if (runPath[2]) {
				run.undoing = true;
				return Response.json({ runId: run.id }, { status: 202 });
			}
			return Response.json({ run, actions: this.runActions[run.id] ?? [] });
		}
		const replacing = /^\/duplicates\/([^/]+)\/replace$/.exec(path);
		if (replacing && request.method === "POST") {
			const pair = this.pairs.find((p) => p.id === replacing[1]);
			const keepID = (body as { keep: string }).keep;
			const keep = this.books.find((b) => b.id === keepID);
			const goneID = pair?.books.find((b) => b.id !== keepID)?.id;
			const gone = this.books.find((b) => b.id === goneID);
			if (!pair || !keep || !gone) {
				return Response.json({ error: { code: "not_found", message: "There is no such pair." } }, { status: 404 });
			}
			if (this.replaceRefused) {
				return Response.json({ error: { code: "conflict", message: this.replaceRefused } }, { status: 409 });
			}
			for (const file of gone.files) {
				this.trashed.push({
					file,
					view: {
						id: file.id, name: file.name, format: file.format, size: file.size,
						bookId: keep.id, bookTitle: keep.title, libraryId: keep.libraryId, library: "",
						trashedAt: "2026-10-05T10:00:00Z", purgeAt: "2026-11-04T10:00:00Z",
					},
				});
			}
			this.books = this.books.filter((b) => b !== gone);
			this.mergedInto[gone.id] = keep.id;
			pair.state = "replaced";
			return new Response(null, { status: 204 });
		}
		const pairState = /^\/duplicates\/([^/]+)\/state$/.exec(path);
		if (pairState && request.method === "PUT") {
			const pair = this.pairs.find((p) => p.id === pairState[1]);
			if (!pair) {
				return Response.json({ error: { code: "not_found", message: "There is no such pair." } }, { status: 404 });
			}
			pair.state = (body as { state: DuplicatePair["state"] }).state;
			return new Response(null, { status: 204 });
		}
		if (path === "/trash") {
			return Response.json({ files: this.trashed.map((t) => t.view) });
		}
		const fix = /^\/files\/([^/]+)\/fix$/.exec(path);
		if (fix && request.method === "POST") {
			const findings = (body as { findings: string[] }).findings;
			const file = this.books.flatMap((b) => b.files).find((f) => f.id === fix[1]);
			if (!file?.ereader) {
				return Response.json({ error: { code: "not_found", message: "There is no such file." } }, { status: 404 });
			}
			this.fixes.push({ file: file.id, findings });
			const fixed = findings.map((f) => (f === "subset-fonts" ? "large-fonts" : f));
			file.ereader.findings = file.ereader.findings.filter((f) => !fixed.includes(f));
			return Response.json({
				documentsSplit: findings.includes("large-document") ? 1 : 0,
				partsAdded: findings.includes("large-document") ? 4 : 0,
				imagesScaled: findings.includes("large-image") ? 1 : 0,
				fontsSubset: findings.includes("subset-fonts") ? 8 : 0,
				fontsRemoved: findings.includes("large-fonts") ? 8 : 0,
				itemsRemoved: findings.includes("missing-resources") ? 1 : 0,
				documentsRepaired: findings.includes("malformed-document") ? 2 : 0,
				tocEntriesRemoved: findings.includes("broken-toc") ? 1 : 0,
				tocEntriesRepointed: findings.includes("broken-toc") ? 3 : 0,
			});
		}
		const fileAction = /^\/files\/([^/]+)(?:\/(trash|restore))?$/.exec(path);
		if (fileAction && (fileAction[2] || request.method === "DELETE")) {
			return this.trashAction(fileAction[1] ?? "", fileAction[2] ?? "purge");
		}
		if (path === "/search/status") {
			return Response.json(this.searchStatus);
		}
		if (path === "/embedding/status") {
			return Response.json(this.embedding);
		}
		const removeModel = /^\/embedding\/models\/([^/]+)\/remove$/.exec(path);
		if (removeModel && request.method === "POST") {
			const name = decodeURIComponent(removeModel[1] ?? "");
			const model = this.embeddingModels.models.find((m) => m.name === name);
			if (!model) {
				return Response.json(
					{ error: { code: "not_found", message: "There is no such model." } },
					{ status: 404 },
				);
			}
			if (model.chosen) {
				return Response.json(
					{ error: { code: "conflict", message: "The model is the one books are embedded with; choose another first." } },
					{ status: 409 },
				);
			}
			this.removedModels.push(name);
			return new Response(null, { status: 202 });
		}
		if (path === "/embedding/models") {
			if (!this.session?.permissions.includes("settings:manage")) {
				return Response.json(
					{ error: { code: "forbidden", message: "You do not have permission to do that." } },
					{ status: 403 },
				);
			}
			return Response.json(this.embeddingModels);
		}
		if (path === "/system/checks") {
			if (!this.session?.permissions.includes("settings:manage")) {
				return Response.json(
					{ error: { code: "forbidden", message: "You do not have permission to do that." } },
					{ status: 403 },
				);
			}
			return Response.json({ checks: this.checks });
		}
		if (path === "/system/update") {
			if (!this.session?.permissions.includes("settings:manage")) {
				return Response.json(
					{ error: { code: "forbidden", message: "You do not have permission to do that." } },
					{ status: 403 },
				);
			}
			return Response.json(this.update);
		}
		if (path === "/system/health") {
			if (!this.session?.permissions.includes("settings:manage")) {
				return Response.json(
					{ error: { code: "forbidden", message: "You do not have permission to do that." } },
					{ status: 403 },
				);
			}
			return Response.json(this.health);
		}
		if (path === "/statistics") {
			return Response.json(this.figures);
		}
		if (path === "/statistics/work") {
			if (!this.session?.permissions.includes("index:rebuild")) {
				return Response.json(
					{ error: { code: "forbidden", message: "You do not have permission to do that." } },
					{ status: 403 },
				);
			}
			return Response.json(this.work);
		}
		if (path === "/search/reread") {
			const target = body as { library?: string; book?: string };
			const files = target.book ? 1 : target.library ? 2 : this.searchStatus.files;
			this.searchStatus = { ...this.searchStatus, indexed: this.searchStatus.indexed - files };
			return Response.json({ files }, { status: 202 });
		}
		if (path === "/search/rebuild") {
			const queued = !this.searchStatus.rebuilding;
			this.searchStatus = { ...this.searchStatus, rebuilding: true };
			return Response.json({ queued }, { status: 202 });
		}
		if (path === "/metadata/search") {
			return Response.json(
				this.candidates.search ?? { candidates: [], failures: [] },
			);
		}
		if (path === "/me/queue") {
			if (request.method === "PUT") {
				const wanted = (body as { books: string[] }).books;
				this.queue = [...new Set(wanted)].filter((id) =>
					this.books.some(
						(b) => b.id === id && b.files.some((f) => f.kind === "audio"),
					),
				);
			}
			return Response.json({ books: this.queue });
		}
		if (path === "/me/recommendations") {
			return Response.json(this.recommendations ?? { hasProfile: false, books: [] });
		}
		const dismissed = /^\/me\/recommendations\/([^/]+)\/dismiss$/.exec(path);
		if (dismissed && this.recommendations) {
			this.recommendations = {
				...this.recommendations,
				books: this.recommendations.books.filter((r) => r.book.id !== dismissed[1]),
			};
			return new Response(null, { status: 204 });
		}
		if (path === "/me/reading") {
			return this.answerHistory(url.searchParams);
		}
		const imported = /^\/me\/reading-imports\/([^/]+)(?:\/(start)|\/rows\/(\d+))?$/.exec(path);
		if (imported && this.importPreview) {
			return this.answerImport(request.method, imported[2] === "start", Number(imported[3]), body);
		}
		if (path === "/me/stats") {
			return Response.json(
				this.stats ?? {
					days: [],
					pagesPerDay: 0,
					readingMinutesPerDay: 0,
					listeningMinutesPerDay: 0,
					totalPages: 0,
					totalReadingMinutes: 0,
					totalListeningMinutes: 0,
					pagesEstimated: false,
					years: [],
					undated: 0,
					history: [],
				},
			);
		}
		if (path === "/curation/cleanup" || path === "/curation/matches") {
			return this.answerAuto(path, request.method, body);
		}
		if (path === "/cleanup" || path.startsWith("/cleanup/")) {
			return this.answerCleanup(path, url.searchParams, body);
		}
		if (path === "/genres/suggestions" || /^\/books\/[^/]+\/genres\/[^/]+$/.test(path)) {
			return this.answerGenres(path, url.searchParams, body);
		}
		if (path === "/books/bulk" || path.startsWith("/bulk/")) {
			return this.answerBulk(path, body as BulkRequest);
		}
		if (path === "/notifications") {
			return Response.json({
				notifications: this.notifications,
				unread: this.unread(),
			});
		}
		if (path === "/trackers" || path.startsWith("/trackers/")) {
			return this.answerTrackers(request.method, path, body as { kind: Tracker["kind"]; name: string });
		}
		if (path === "/feeds/items") {
			return Response.json({ items: this.feedItems });
		}
		if (path === "/feeds" || path.startsWith("/feeds/")) {
			return this.answerFeeds(request.method, path, body as { url: string; keywords?: string[]; authors?: string[] });
		}
		if (path === "/releases") {
			return Response.json({
				releases: this.releases[url.searchParams.get("when") === "recent" ? "recent" : "upcoming"],
			});
		}
		if (path === "/me/notification-settings") {
			if (request.method === "PUT") {
				for (const c of (body as { kinds: NotificationSetting[] }).kinds) {
					const s = this.notificationSettings.find((x) => x.kind === c.kind);
					if (s) s.app = c.app;
				}
			}
			return Response.json({ kinds: this.notificationSettings });
		}
		if (path === "/notifications/read") {
			const req = body as { ids?: string[]; all?: boolean };
			for (const n of this.notifications) {
				if (req.all || req.ids?.includes(n.id))
					n.readAt ??= "2026-01-03T00:00:00Z";
			}
			return Response.json({ unread: this.unread() });
		}
		if (/^\/files\/[^/]+\/download$/.test(path)) {
			return new Response(`the bytes of ${path}`);
		}
		if (path === "/smart-shelves" || path.startsWith("/smart-shelves/")) {
			return this.answerSmartShelves(
				request.method,
				path,
				body as SmartShelfRequest,
			);
		}
		if (path === "/collections" || path.startsWith("/collections/")) {
			return this.answerCollections(
				request.method,
				path,
				url.searchParams,
				body,
			);
		}
		if (path.startsWith("/matches")) {
			return this.answerMatches(request.method, path, body as CandidateApply);
		}
		if (path === "/me/kindle") {
			if (request.method === "PUT") {
				const address = String((body as { address?: string }).address ?? "").trim().toLowerCase();
				if (address && !address.endsWith("@kindle.com")) {
					return Response.json(
						{ error: { code: "validation_failed", message: "Check the highlighted fields.", fields: { address: "Give the address of your Kindle, such as name@kindle.com." } } },
						{ status: 422 },
					);
				}
				this.kindle = { ...this.kindle, address };
			}
			return Response.json(this.kindle);
		}
		if (path === "/kindle/sends" && request.method === "POST") {
			const books = (body as { books?: string[] }).books ?? this.books.map((b) => b.id);
			let queued = 0;
			const skipped: { bookId: string; reason: string }[] = [];
			for (const bookId of books) {
				const book = this.books.find((b) => b.id === bookId);
				if (!book?.files.some((f) => f.format === "epub")) {
					skipped.push({ bookId, reason: "unavailable" });
				} else if (this.kindleSends.some((s) => s.bookId === bookId && s.state === "queued")) {
					skipped.push({ bookId, reason: "pending" });
				} else {
					this.kindleSends.unshift({
						id: `ks-${this.kindleSends.length + 1}`, bookId, state: "queued",
						address: this.kindle.address, createdAt: "2026-01-05T08:00:00Z",
					});
					queued++;
				}
			}
			return Response.json({ queued, skipped });
		}
		const bookKindle = /^\/books\/([^/]+)\/kindle$/.exec(path);
		if (bookKindle) {
			return Response.json({ sends: this.kindleSends.filter((s) => s.bookId === bookKindle[1]) });
		}
		const bookDeliveries = /^\/books\/([^/]+)\/deliveries$/.exec(path);
		if (bookDeliveries) {
			return Response.json({
				deliveries: this.deliveries.filter((d) => d.bookId === bookDeliveries[1]),
			});
		}
		const sending = /^\/devices\/([^/]+)\/books$/.exec(path);
		if (sending && request.method === "GET") {
			const device = this.devices.find((d) => d.id === sending[1]);
			if (!device) {
				return Response.json({ error: { code: "not_found", message: "There is no such device." } }, { status: 404 });
			}
			const books = this.deliveries
				.filter((d) => d.deviceId === device.id)
				.slice()
				.reverse()
				.flatMap((d) => {
					const book = this.books.find((b) => b.id === d.bookId);
					if (!book) return [];
					const state = d.deliveredAt ? "delivered" : d.syncedAt ? "synced" : "waiting";
					return [{
						book: summaryOf(book), deliveryId: d.id, state, queuedAt: d.queuedAt,
						deliveredAt: d.deliveredAt, syncedAt: d.syncedAt,
						removable: !d.deliveredAt || !!d.syncedAt, sentFixed: !!d.sentFixed,
					}];
				});
			return Response.json({ device, books });
		}
		if (sending && request.method === "POST") {
			const device = this.devices.find((d) => d.id === sending[1]);
			if (!device) {
				return Response.json({ error: { code: "not_found", message: "There is no such device." } }, { status: 404 });
			}
			const books = (body as { books?: string[] }).books ?? this.books.map((b) => b.id);
			let queued = 0;
			for (const bookId of books) {
				const had = this.deliveries.find((d) => d.bookId === bookId && d.deviceId === device.id);
				if (had && !had.deliveredAt) continue;
				this.deliveries = this.deliveries.filter((d) => d !== had);
				this.deliveries.push({
					id: `dl-${this.deliveries.length + 1}`, bookId, deviceId: device.id,
					deviceName: device.name, queuedAt: "2026-01-05T08:00:00Z",
				});
				queued++;
			}
			return Response.json({ queued });
		}
		const takeBack = /^\/deliveries\/([^/]+)$/.exec(path);
		if (takeBack && request.method === "DELETE") {
			this.deliveries = this.deliveries.filter((d) => d.id !== takeBack[1]);
			return new Response(null, { status: 204 });
		}
		if (path === "/authors" || path === "/series") {
			return this.nameList(path, url.searchParams);
		}
		if (path.startsWith("/authors/")) {
			return this.author(decodeURIComponent(path.slice("/authors/".length)));
		}
		if (path.startsWith("/series/")) {
			return this.seriesOf(decodeURIComponent(path.slice("/series/".length)));
		}
		if (path === "/books" || path.startsWith("/books/")) {
			return this.answerBooks(request.method, path, url.searchParams, body);
		}
		return this.answer(
			request.method,
			path,
			body as Record<string, string> | undefined,
		);
	}

	/** A stand-in for XMLHttpRequest, which uploads use for their progress. */
	uploadRequest(): new () => object {
		const isDown = () => this.down;
		const record = (method: string, path: string, name: string) =>
			this.requests.push({ method, path, body: { file: name } });
		const answer = (path: string, file: File) =>
			path === "/me/reading-imports"
				? Promise.resolve(
						this.importPreview
							? Response.json(this.importPreview, { status: 201 })
							: Response.json({ error: { code: "validation", message: "This is not a Goodreads or StoryGraph export." } }, { status: 422 }),
					)
				: this.answerUpload(path, file);
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
		return FakeXMLHttpRequest;
	}

	private answerHistory(query: URLSearchParams): Response {
		const statuses = query.get("status") ? [query.get("status")] : ["reading", "completed", "abandoned"];
		const year = query.get("year");
		const stars = query.get("stars");
		const words = (query.get("q") ?? "").toLowerCase();
		const day = (b: BookDetail) => b.reading.finishedOn ?? b.reading.startedOn ?? "";
		const mine = this.books
			.filter((b) => statuses.includes(b.reading.status))
			.sort((a, b) => day(b).localeCompare(day(a)));
		const years = [...new Set(mine.map((b) => Number(day(b).slice(0, 4))).filter(Boolean))].sort((a, b) => b - a);
		const entries = mine
			.filter((b) => !year || day(b).startsWith(year))
			.filter((b) => !stars || b.reading.rating === Number(stars))
			.filter(
				(b) =>
					!words ||
					`${b.title} ${b.contributors.map((c) => c.name).join(" ")}`.toLowerCase().includes(words),
			)
			.map((b) => ({
				book: summaryOf(b),
				startedOn: b.reading.startedOn,
				finishedOn: b.reading.finishedOn,
			}));
		return Response.json({ entries, years });
	}

	private answerImport(method: string, start: boolean, row: number, body: unknown): Response {
		const preview = this.importPreview as ReadingImport;
		if (start) {
			this.importPreview = { ...preview, state: "done", outcomes: { created: preview.new, changed: preview.matched } };
			return Response.json(this.importPreview, { status: 202 });
		}
		if (method === "PUT") {
			const choice = body as { book?: string; skip?: boolean };
			const answered = (a: ReadingImport["ambiguities"][number]) =>
				a.row === row ? { ...a, choice: choice.book, skip: Boolean(choice.skip) } : a;
			preview.ambiguities = preview.ambiguities.map(answered);
			preview.maybes = preview.maybes.map(answered);
			return new Response(null, { status: 204 });
		}
		return Response.json(preview);
	}

	private async answerUpload(path: string, file: File): Promise<Response> {
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
		if (!this.session) {
			return refuse(401, "unauthorized", "Sign in to continue.");
		}
		if (!this.session.permissions.includes("books:upload")) {
			return refuse(403, "forbidden", "You do not have permission to do that.");
		}
		const library = this.libraries.find(
			(l) => path === `/libraries/${l.id}/uploads`,
		);
		if (!library) {
			return refuse(404, "not_found", "There is no such library.");
		}
		if (library.mode !== "managed") {
			return refuse(
				409,
				"conflict",
				"Books can only be uploaded into a managed library.",
			);
		}
		const dot = file.name.lastIndexOf(".");
		const format = dot < 0 ? "" : file.name.slice(dot + 1).toLowerCase();
		if (
			![
				"epub",
				"pdf",
				"mobi",
				"azw",
				"azw3",
				"m4b",
				"m4a",
				"mp3",
				"flac",
				"ogg",
				"opus",
			].includes(format)
		) {
			return refuse(422, "validation_failed", "Some fields need attention.", {
				file: "GOtome does not read this kind of file.",
			});
		}
		const content = await file.text();
		const known = this.uploaded.get(content);
		if (known) {
			return Response.json({
				...known,
				outcome: "duplicate",
				fileId: undefined,
			});
		}
		const me = this.accounts.get(this.session.username.toLowerCase());
		if (
			me?.quotaBytes !== undefined &&
			(me.usedBytes ?? 0) + file.size > me.quotaBytes
		) {
			return refuse(
				413,
				"over_quota",
				"This file does not fit into your storage.",
			);
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

	private answer(
		method: string,
		path: string,
		body: Record<string, string> = {},
	): Response {
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

		if (
			path === "/jobs" ||
			path.startsWith("/jobs/") ||
			path.startsWith("/files/") ||
			/^\/libraries\/[^/]+\/extractions$/.test(path)
		) {
			if (!this.session) {
				return refuse(401, "unauthorized", "Sign in to continue.");
			}
			if (!this.session.permissions.includes("index:rebuild")) {
				return refuse(
					403,
					"forbidden",
					"You do not have permission to do that.",
				);
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
				for (const f of this.books.flatMap((b) => b.files)) {
					if (f.id === file[1]) {
						f.extractState = "pending";
						f.extractError = undefined;
					}
				}
				return Response.json({ queued: 1 }, { status: 202 });
			}
			const library = this.libraries.find(
				(l) => path === `/libraries/${l.id}/extractions`,
			);
			if (library) {
				this.reread.push(library.id);
				library.filesFailed = 0;
				return Response.json({ queued: 1 }, { status: 202 });
			}
			return refuse(404, "not_found", "There is nothing at this address.");
		}
		if (path === "/libraries" || path.startsWith("/libraries/")) {
			return this.answerLibraries(
				method,
				path.slice("/libraries".length + 1),
				body,
				refuse,
			);
		}

		if (
			path === "/users" ||
			path.startsWith("/users/") ||
			path.startsWith("/auth/sessions") ||
			path.startsWith("/auth/devices") ||
			path.startsWith("/auth/api-keys") ||
			path.startsWith("/auth/device-requests") ||
			path === "/auth/password" ||
			path === "/auth/storage"
		) {
			return this.answerAccounts(
				method,
				path,
				body as Record<string, unknown>,
				refuse,
			);
		}
		if (path.startsWith("/metadata/scoring")) {
			return this.answerScoring(path, method, body, refuse);
		}
		if (path === "/webhooks" || path.startsWith("/webhooks/")) {
			if (!this.session?.permissions.includes("settings:manage")) {
				return refuse(403, "forbidden", "You do not have permission to do that.");
			}
			return this.answerWebhooks(method, path, body as unknown as WebhookRequest, refuse);
		}

		if (path === "/settings") {
			return this.answerSettings(
				method,
				body as unknown as { values?: Record<string, string | null> },
				refuse,
			);
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
				if (
					!account ||
					account.password !== body.password ||
					account.disabled
				) {
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
			case "GET /version":
				return Response.json(this.build);
			case "GET /auth/methods":
				return Response.json(this.signInMethods);
			case "POST /auth/oidc/start":
				if (!this.signInMethods.sso) {
					return refuse(409, "conflict", "Single sign-on is not set up.");
				}
				return Response.json({
					url: `https://id.example/authorize?returnTo=${encodeURIComponent(
						(body as { returnTo?: string }).returnTo ?? "/",
					)}`,
				});
			case "GET /auth/identities":
				return this.session
					? Response.json({ items: this.identities })
					: refuse(401, "unauthorized", "Sign in to continue.");
			case "POST /auth/identities":
				return this.session
					? Response.json({ url: "https://id.example/authorize?link=1" })
					: refuse(401, "unauthorized", "Sign in to continue.");
			case "GET /auth/me":
				return this.session
					? Response.json(this.session)
					: refuse(401, "unauthorized", "Sign in to continue.");
			default: {
				const identity = path.match(/^\/auth\/identities\/([^/]+)$/);
				if (identity && method === "DELETE" && this.session) {
					this.identities = this.identities.filter((i) => i.id !== identity[1]);
					return new Response(null, { status: 204 });
				}
				return refuse(404, "not_found", "There is nothing at this address.");
			}
		}
	}
	private answerBooks(
		method: string,
		path: string,
		query: URLSearchParams,
		body: unknown,
	): Response {
		if (!this.session) {
			return Response.json(
				{ error: { code: "unauthorized", message: "Sign in to continue." } },
				{ status: 401 },
			);
		}
		const merging = /^\/books\/([^/]+)\/merge$/.exec(path);
		if (merging && method === "POST") {
			const { from, take } = body as { from: string; take: string[] };
			const into = this.books.find((b) => b.id === merging[1]);
			const gone = this.books.find((b) => b.id === from);
			if (!into || !gone) {
				return Response.json({ error: { code: "not_found", message: "There is no such book." } }, { status: 404 });
			}
			if (take.includes("title")) into.title = gone.title;
			into.files.push(...gone.files);
			this.books = this.books.filter((b) => b !== gone);
			this.mergedInto[gone.id] = into.id;
			for (const p of this.pairs) {
				if (p.books.some((b) => b.id === gone.id)) p.state = "merged";
			}
			return Response.json(into);
		}
		const joining = /^\/books\/([^/]+)\/join-library-book$/.exec(path);
		if (joining && method === "POST") {
			const { book } = body as { book: string };
			const placeholder = this.books.find((b) => b.id === joining[1] && b.placeholder);
			const into = this.books.find((b) => b.id === book && !b.placeholder);
			if (!placeholder || !into) {
				return Response.json({ error: { code: "not_found", message: "There is no such book." } }, { status: 404 });
			}
			into.reading = placeholder.reading;
			this.books = this.books.filter((b) => b !== placeholder);
			this.mergedInto[placeholder.id] = into.id;
			return Response.json({ book: into.id, actionId: `join-${placeholder.id}` });
		}
		if (/^\/placeholder-joins\/[^/]+\/undo$/.test(path) && method === "POST") {
			return Response.json({ actionId: path.split("/")[2], state: "undone" });
		}
		const old = /^\/books\/([^/]+)$/.exec(path);
		if (method === "GET" && old?.[1] && this.mergedInto[old[1]]) {
			return this.answerBooks(method, `/books/${this.mergedInto[old[1]]}`, query, body);
		}
		if (path === "/books/names") {
			return Response.json({
				names: this.names(query.get("kind") ?? "", query.get("q") ?? ""),
			});
		}
		const saving = /^\/books\/([^/]+)\/progress\/(ebook|audio)$/.exec(path);
		if (saving) {
			const update = body as ProgressUpdate;
			const further = this.furtherProgress;
			if (further && !update.force && further.fraction > update.fraction) {
				return Response.json({ saved: false, progress: further });
			}
			const { basedOn: _, force: __, ...rest } = update;
			const written = {
				...rest,
				updatedAt: `2026-01-03T00:00:${String(this.requests.length % 60).padStart(2, "0")}Z`,
			};
			const id = saving[1] ?? "";
			const book = this.books.find((b) => b.id === id);
			if (book && ["unread", "wishlist"].includes(book.reading.status)) {
				book.reading = { ...book.reading, status: "reading" };
			}
			this.progress[id] = {
				finishes: 0,
				...this.progress[id],
				[saving[2] ?? "ebook"]: written,
			};
			return Response.json({ saved: true, progress: written });
		}
		const similar = /^\/books\/([^/]+)\/similar$/.exec(path);
		if (similar) {
			const books = (this.similar[similar[1] ?? ""] ?? [])
				.map((id) => this.books.find((b) => b.id === id))
				.filter((b) => b !== undefined)
				.map(summaryOf);
			return Response.json({ books });
		}
		const audio = /^\/books\/([^/]+)\/audio$/.exec(path);
		if (audio) {
			return Response.json(
				this.audio[audio[1] ?? ""] ?? {
					parts: [],
					chapters: [],
					durationMs: 0,
					complete: true,
				},
			);
		}
		const continuation = /^\/books\/([^/]+)\/continuation$/.exec(path);
		if (continuation) {
			return Response.json(this.continuations[continuation[1] ?? ""] ?? {});
		}
		const progress = /^\/books\/([^/]+)\/progress$/.exec(path);
		if (progress) {
			return Response.json(this.progress[progress[1] ?? ""] ?? { finishes: 0 });
		}
		const noted = /^\/books\/([^/]+)\/note$/.exec(path);
		if (noted) {
			const book = this.books.find((b) => b.id === noted[1]);
			if (book) {
				book.reading = { ...book.reading, note: (body as { note: string }).note || undefined };
			}
			return new Response(null, { status: 204 });
		}
		if (path === "/books/wishes") {
			const wish = body as WishRequest;
			this.withBook(wish.title ?? "", {
				libraryId: wish.library ?? this.libraries[0]?.id,
				contributors: wish.contributors ?? [],
				publisher: wish.publisher,
				placeholder: true,
				reading: {
					status: (wish.status ?? "wishlist") as ReadingStatus,
					rating: wish.rating || undefined,
					finishedOn: wish.finishedOn,
				},
			});
			for (const listed of this.listed) {
				listed.wished ||= listed.title === wish.title;
			}
			return Response.json(this.books.at(-1), { status: 201 });
		}
		if (path === "/books/reading") {
			const req = body as ReadingBulk;
			const chosen = this.books.filter((b) =>
				req.books?.length
					? req.books.includes(b.id)
					: (!req.library || b.libraryId === req.library) &&
						(!req.filter || matches(b, JSON.parse(req.filter))),
			);
			for (const b of chosen) {
				b.reading = {
					...b.reading,
					...(req.status ? { status: req.status } : {}),
				};
			}
			return Response.json({ changed: chosen.length });
		}
		const reading = /^\/books\/([^/]+)\/reading$/.exec(path);
		if (reading) {
			const book = this.books.find((b) => b.id === reading[1]);
			if (!book) {
				return Response.json(
					{ error: { code: "not_found", message: "There is no such book." } },
					{ status: 404 },
				);
			}
			const change = body as ReadingChange;
			book.reading = { ...book.reading, ...change };
			if (change.rating === 0) {
				book.reading.rating = undefined;
			}
			return Response.json(book.reading);
		}
		const asked = /^\/books\/([^/]+)\/candidates(\/apply)?$/.exec(path);
		if (asked && !asked[2]) {
			return Response.json(
				this.candidates[asked[1] ?? ""] ?? { candidates: [], failures: [] },
			);
		}
		if (asked?.[2]) {
			return this.applyCandidate(asked[1] ?? "", body as CandidateApply);
		}
		if (method !== "GET") {
			return this.editBook(method, path, body as BookEdit | undefined);
		}
		if (path === "/books/count") {
			const tree = JSON.parse(query.get("filter") || "{}") as Rule;
			const unknown = fieldsOf(tree).find(
				(f) => f !== "words" && !FIELDS.includes(f as Field),
			);
			if (unknown) {
				return Response.json(
					{
						error: {
							code: "validation",
							message: "Some fields need attention.",
							fields: { filter: `Books cannot be filtered by "${unknown}".` },
						},
					},
					{ status: 422 },
				);
			}
			return Response.json({
				count: this.books.filter((b) => !b.placeholder && matches(b, tree))
					.length,
			});
		}
		if (path === "/books/search") {
			return Response.json({
				books: this.search(query.get("q") ?? ""),
				names: this.searchNames(query.get("q") ?? ""),
			});
		}
		if (path === "/books/facets") {
			return Response.json({ facets: this.facets(query) });
		}
		if (path !== "/books") {
			const book = this.books.find((b) => `/books/${b.id}` === path);
			return book
				? Response.json(book)
				: Response.json(
						{ error: { code: "not_found", message: "There is no such book." } },
						{ status: 404 },
					);
		}
		const authorOf = (b: BookDetail) =>
			b.contributors.find((c) => c.role === "author")?.name ?? "";
		const sort = query.get("sort") ?? "title";
		const key = (b: BookDetail) =>
			sort === "author"
				? `${authorOf(b)}\u0000${b.title}`
				: sort === "added"
					? b.addedAt
					: sort === "series"
						? `${b.series ? 0 : 1}${b.series ?? ""}\u0000${String(b.seriesIndex ?? 1e6).padStart(8, "0")}\u0000${b.title}`
						: b.title;
		const tree = query.get("filter");
		const sorted = this.books
			.filter(
				(b) => !query.get("library") || b.libraryId === query.get("library"),
			)
			.filter((b) => !b.placeholder || query.get("placeholders") === "include")
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
			books: page.map(summaryOf),
			nextCursor:
				start + limit < sorted.length ? String(start + limit) : undefined,
		});
	}

	private nameList(path: string, query: URLSearchParams): Response {
		const found = new Map<string, NamedEntry>();
		const add = (name: string, key: string, sort: string) => {
			const hit = found.get(key);
			if (hit) {
				hit.books += 1;
			} else {
				found.set(key, { name, key, sort, books: 1 });
			}
		};
		for (const b of this.books.filter((x) => !x.placeholder)) {
			if (path === "/series") {
				if (b.series) add(b.series, nameKey(b.series), nameKey(b.series));
				continue;
			}
			for (const c of b.contributors.filter((x) => x.role === "author")) {
				add(c.name, personKey(c.name), c.name.split(" ").at(-1) ?? c.name);
			}
		}
		const sorted = [...found.values()].sort((a, b) => a.sort.localeCompare(b.sort));
		const start = Number(query.get("after") || 0);
		const limit = Number(query.get("limit") || 50);
		return Response.json({
			names: sorted.slice(start, start + limit),
			nextCursor: start + limit < sorted.length ? String(start + limit) : undefined,
		});
	}

	private author(key: string): Response {
		const spellings = this.books.flatMap((b) =>
			b.placeholder
				? []
				: b.contributors
						.filter((c) => c.role === "author" && personKey(c.name) === personKey(key))
						.map((c) => c.name),
		);
		const name = spellings[0];
		if (!name) {
			return Response.json(
				{ error: { code: "not_found", message: "There is no such author." } },
				{ status: 404 },
			);
		}
		return Response.json({
			name,
			key: personKey(key),
			spellings: [...new Set(spellings)],
			books: spellings.length,
		});
	}

	private seriesOf(key: string): Response {
		const books = this.books
			.filter((b) => !b.placeholder && b.seriesKey === key)
			.sort((a, b) => (a.seriesIndex ?? 1e6) - (b.seriesIndex ?? 1e6));
		const first = books[0];
		if (!first) {
			return Response.json(
				{ error: { code: "not_found", message: "There is no such series." } },
				{ status: 404 },
			);
		}
		const next = books.find((b) => b.reading.status === "unread");
		const listed = this.listed.filter((l) => nameKey(l.series) === key);
		const places = [...books, ...listed].flatMap((b) => b.seriesIndex ?? []);
		const missing = [];
		for (let place = 1; place < Math.max(0, ...places); place++) {
			const last = missing.at(-1);
			if (places.includes(place)) continue;
			if (last?.to === place - 1) {
				last.to = place;
			} else {
				missing.push({ from: place, to: place });
			}
		}
		return Response.json({
			name: first.series,
			key,
			books: books.length,
			read: books.filter((b) => b.reading.status === "completed").length,
			next: next && summaryOf(next),
			missing,
			listed: listed.map(({ series: _, ...book }) => book),
			library: first.libraryId,
		});
	}

	/** Tells the person something, as a finished bulk change would. */
	notify(n: Partial<Notification> = {}): this {
		this.notifications.unshift({
			id: `note-${this.notifications.length + 1}`,
			kind: "bulk.finished",
			data: { action: "edit", outcomes: { changed: 2, failed: 1 } },
			link: "/bulk/bulk-1",
			createdAt: "2026-01-02T10:00:00Z",
			...n,
		});
		return this;
	}

	unread(): number {
		return this.notifications.filter((n) => !n.readAt).length;
	}

	withCollection(
		name: string,
		books: string[],
		overrides: Partial<Collection> = {},
	): this {
		this.collections.push({
			id: `col-${this.collections.length + 1}`,
			name,
			visibility: "private",
			ownerId: this.session?.id ?? "u-1",
			ownerName: this.session?.username ?? "admin",
			mine: true,
			updatedAt: "2026-01-01T00:00:00Z",
			...overrides,
			books,
		});
		return this;
	}

	/** Collections as the server keeps them, for one person who owns those marked mine. */
	private answerCollections(
		method: string,
		path: string,
		query: URLSearchParams,
		body: unknown,
	): Response {
		const notFound = () =>
			Response.json(
				{
					error: { code: "not_found", message: "There is no such collection." },
				},
				{ status: 404 },
			);
		const view = (c: (typeof this.collections)[number]) => {
			const { books, ...rest } = c;
			return { ...rest, books: books.length };
		};
		const detail = (c: (typeof this.collections)[number], status = 200) =>
			Response.json(
				{
					...view(c),
					bookList: c.books.flatMap((id) =>
						this.books.filter((b) => b.id === id).map(summaryOf),
					),
				},
				{ status },
			);
		if (path === "/collections") {
			if (method === "POST") {
				const req = body as CollectionRequest;
				this.withCollection(req.name, [], {
					visibility: req.visibility ?? "private",
					description: req.description,
				});
				return detail(
					this.collections.at(-1) as (typeof this.collections)[number],
					201,
				);
			}
			const book = query.get("book");
			return Response.json({
				collections: this.collections.map((c) => ({
					...view(c),
					...(book ? { hasBook: c.books.includes(book) } : {}),
				})),
			});
		}
		const [, id, part, bookId] =
			/^\/collections\/([^/]+)(?:\/(books|order))?(?:\/([^/]+))?$/.exec(path) ??
			[];
		const c = this.collections.find((x) => x.id === id);
		if (!c) {
			return notFound();
		}
		if (method !== "GET" && !c.mine) {
			return Response.json(
				{
					error: {
						code: "forbidden",
						message: "Only its owner changes a collection.",
					},
				},
				{ status: 403 },
			);
		}
		if (part === "books" && method === "POST") {
			const req = body as CollectionAdd;
			const chosen = req.books?.length
				? req.books
				: this.books
						.filter(
							(b) =>
								!b.placeholder && (!req.library || b.libraryId === req.library),
						)
						.filter((b) => !req.filter || matches(b, JSON.parse(req.filter)))
						.map((b) => b.id);
			const added = chosen.filter((b) => !c.books.includes(b));
			c.books.push(...added);
			return Response.json({ added: added.length });
		}
		if (part === "books" && method === "DELETE") {
			c.books = c.books.filter((b) => b !== bookId);
			return new Response(null, { status: 204 });
		}
		if (part === "order") {
			c.books = (body as { books: string[] }).books;
			return detail(c);
		}
		if (method === "PUT") {
			Object.assign(c, body as CollectionRequest);
			return detail(c);
		}
		if (method === "DELETE") {
			this.collections = this.collections.filter((x) => x !== c);
			return new Response(null, { status: 204 });
		}
		return detail(c);
	}

	withSmartShelf(
		name: string,
		filter: object,
		overrides: Partial<SmartShelf> = {},
	): this {
		this.smartShelves.push({
			id: `smart-${this.smartShelves.length + 1}`,
			name,
			filter: JSON.stringify(filter),
			visibility: "private",
			ownerId: this.session?.id ?? "u-1",
			ownerName: this.session?.username ?? "admin",
			mine: true,
			updatedAt: "2026-01-01T00:00:00Z",
			...overrides,
		});
		return this;
	}

	/** Smart shelves, matched against the books as they are now; a rule on an unknown field is refused. */
	private answerSmartShelves(
		method: string,
		path: string,
		body: SmartShelfRequest,
	): Response {
		const on = (s: Omit<SmartShelf, "books">) =>
			this.books.filter(
				(b) => !b.placeholder && matches(b, JSON.parse(s.filter)),
			);
		const view = (s: Omit<SmartShelf, "books">) => ({
			...s,
			books: on(s).length,
		});
		const refuse = () => {
			const unknown = fieldsOf(JSON.parse(body.filter || "{}")).find(
				(f) => !FIELDS.includes(f as Field),
			);
			return unknown
				? Response.json(
						{
							error: {
								code: "validation",
								message: "Some fields need attention.",
								fields: { filter: `Books cannot be filtered by "${unknown}".` },
							},
						},
						{ status: 422 },
					)
				: undefined;
		};
		if (path === "/smart-shelves") {
			if (method === "POST") {
				const refused = refuse();
				if (refused) {
					return refused;
				}
				this.withSmartShelf(body.name, JSON.parse(body.filter), {
					visibility: body.visibility ?? "private",
				});
				return Response.json(
					view(this.smartShelves.at(-1) as Omit<SmartShelf, "books">),
					{ status: 201 },
				);
			}
			return Response.json({ shelves: this.smartShelves.map(view) });
		}
		const [, id, books] =
			/^\/smart-shelves\/([^/]+)(\/books)?$/.exec(path) ?? [];
		const shelf = this.smartShelves.find((s) => s.id === id);
		if (!shelf) {
			return Response.json(
				{
					error: {
						code: "not_found",
						message: "There is no such smart shelf.",
					},
				},
				{ status: 404 },
			);
		}
		if (books) {
			return Response.json({
				books: on(shelf)
					.sort((a, b) => a.title.localeCompare(b.title))
					.map(summaryOf),
			});
		}
		if (method !== "GET" && !shelf.mine) {
			return Response.json(
				{
					error: {
						code: "forbidden",
						message: "Only its owner changes a smart shelf.",
					},
				},
				{ status: 403 },
			);
		}
		if (method === "PUT") {
			const refused = refuse();
			if (refused) {
				return refused;
			}
			Object.assign(shelf, body);
		}
		if (method === "DELETE") {
			this.smartShelves = this.smartShelves.filter((s) => s !== shelf);
			return new Response(null, { status: 204 });
		}
		return Response.json(view(shelf));
	}

	/** Names in use on the books, by the beginning of a word. */
	private names(kind: string, typed: string): string[] {
		const all = this.books.flatMap((b) =>
			kind === "author"
				? b.contributors.map((c) => c.name)
				: kind === "tag"
					? b.tags
					: [kind === "series" ? b.series : b.publisher].filter(
							(n): n is string => !!n,
						),
		);
		const q = typed.toLowerCase();
		return [...new Set(all)].filter((n) =>
			n
				.toLowerCase()
				.split(/\s+/)
				.some((w) => w.startsWith(q)),
		);
	}

	private answerWebhooks(
		method: string,
		path: string,
		body: WebhookRequest,
		refuse: (status: number, code: string, message: string, fields?: Record<string, string>) => Response,
	): Response {
		const kinds = ["books.added", "scan.failed", "duplicates.found", "release.out"];
		if (path === "/webhooks" && method === "GET") {
			return Response.json({ webhooks: this.webhooks, kinds });
		}
		const check = (creating: boolean) => {
			const fields: Record<string, string> = {};
			if (!body.name?.trim()) {
				fields.name = "Give the webhook a name of up to 80 characters.";
			}
			if (!/^https?:\/\/[^/]+/.test(body.url ?? "")) {
				fields.url = "Give an http or https address without a user name.";
			}
			if ((creating || body.secret !== undefined) && (body.secret ?? "").length < 16) {
				fields.secret = "Give a secret of 16 to 200 characters.";
			}
			return fields;
		};
		if (path === "/webhooks" && method === "POST") {
			const fields = check(true);
			if (Object.keys(fields).length > 0) {
				return refuse(422, "validation_failed", "Some fields need attention.", fields);
			}
			const hook: Webhook = {
				id: `hook-${this.webhooks.length + 1}`,
				name: body.name,
				url: body.url,
				kinds: body.kinds,
				secretSet: true,
				trusted: body.trusted,
				enabled: body.enabled,
				createdAt: "2026-01-03T00:00:00Z",
			};
			this.webhooks.push(hook);
			this.webhookSecrets.set(hook.id, body.secret ?? "");
			return Response.json(hook, { status: 201 });
		}
		const [, id, rest] = /^\/webhooks\/([^/]+)(?:\/(.*))?$/.exec(path) ?? [];
		const hook = this.webhooks.find((h) => h.id === id);
		if (!hook) {
			return refuse(404, "not_found", "There is no such webhook.");
		}
		if (rest === "test" && method === "POST") {
			const log = this.webhookDeliveries.get(hook.id) ?? [];
			log.unshift({
				id: `delivery-${log.length + 1}`,
				kind: "webhook.test",
				status: "pending",
				attempts: 0,
				createdAt: "2026-01-03T00:00:00Z",
				payload: { kind: "webhook.test", data: {}, libraryIds: [] },
			});
			this.webhookDeliveries.set(hook.id, log);
			return new Response(null, { status: 202 });
		}
		if (rest === "deliveries") {
			return Response.json({ deliveries: this.webhookDeliveries.get(hook.id) ?? [] });
		}
		if (method === "PUT") {
			const fields = check(false);
			if (Object.keys(fields).length > 0) {
				return refuse(422, "validation_failed", "Some fields need attention.", fields);
			}
			Object.assign(hook, {
				name: body.name,
				url: body.url,
				kinds: body.kinds,
				trusted: body.trusted,
				enabled: body.enabled,
			});
			if (body.secret !== undefined) {
				this.webhookSecrets.set(hook.id, body.secret);
			}
			return Response.json(hook);
		}
		this.webhooks = this.webhooks.filter((h) => h.id !== id);
		return new Response(null, { status: 204 });
	}

	/**
	 * A bulk edit sets the series and adds and removes tags, leaving locked
	 * fields unless told otherwise; the other actions change nothing here.
	 */
	private answerTrackers(method: string, path: string, body: { kind: Tracker["kind"]; name: string }): Response {
		if (method === "GET") {
			return Response.json({ trackers: this.trackers });
		}
		if (method === "POST") {
			if (body.name.trim() === "") {
				return Response.json(
					{ error: { code: "validation", message: "Invalid", fields: { name: "Name the author or the series to follow." } } },
					{ status: 422 },
				);
			}
			const tracker: Tracker = {
				id: `tracker-${this.trackers.length + 1}`,
				kind: body.kind,
				name: body.name,
				createdAt: "2026-01-03T00:00:00Z",
			};
			this.trackers.push(tracker);
			return Response.json(tracker, { status: 201 });
		}
		const id = path.slice("/trackers/".length);
		if (!this.trackers.some((tr) => tr.id === id)) {
			return Response.json({ error: { code: "not_found", message: "You do not follow that." } }, { status: 404 });
		}
		this.trackers = this.trackers.filter((tr) => tr.id !== id);
		return new Response(null, { status: 204 });
	}

	/** Feeds are followed, fetched again and ended; an address must be of the web. */
	private answerFeeds(method: string, path: string, body: { url: string; keywords?: string[]; authors?: string[] }): Response {
		if (method === "GET") {
			return Response.json({ feeds: this.feeds });
		}
		if (method === "POST" && path === "/feeds") {
			if (!/^https?:\/\/./.test(body.url)) {
				return Response.json(
					{ error: { code: "validation_failed", message: "Invalid", fields: { url: "Give the feed's http or https address." } } },
					{ status: 422 },
				);
			}
			const feed: Feed = {
				id: `feed-${this.feeds.length + 1}`,
				url: body.url,
				keywords: body.keywords ?? [],
				authors: body.authors ?? [],
				failures: 0,
				nextFetchAt: "2026-01-03T00:00:00Z",
				createdAt: "2026-01-03T00:00:00Z",
			};
			this.feeds.push(feed);
			return Response.json(feed, { status: 201 });
		}
		const id = path.slice("/feeds/".length).replace(/\/fetch$/, "");
		const feed = this.feeds.find((f) => f.id === id);
		if (!feed) {
			return Response.json({ error: { code: "not_found", message: "There is no such feed." } }, { status: 404 });
		}
		if (method === "POST") {
			feed.nextFetchAt = "2026-01-03T00:00:00Z";
			return Response.json(feed);
		}
		this.feeds = this.feeds.filter((f) => f.id !== id);
		return new Response(null, { status: 204 });
	}

	private answerBulk(path: string, req: BulkRequest): Response {
		if (!this.session?.permissions.includes("metadata:edit")) {
			return Response.json(
				{ error: { code: "forbidden", message: "You may not do this." } },
				{ status: 403 },
			);
		}
		if (path !== "/books/bulk") {
			const status = this.bulks.find((b) => `/bulk/${b.id}` === path);
			return status
				? Response.json(status)
				: Response.json(
						{
							error: {
								code: "not_found",
								message: "There is no such bulk change.",
							},
						},
						{ status: 404 },
					);
		}
		const chosen = this.books.filter((b) =>
			req.books?.length
				? req.books.includes(b.id)
				: (!req.library || b.libraryId === req.library) &&
					(!req.filter || matches(b, JSON.parse(req.filter))),
		);
		const change = req.change ?? {};
		const results: BulkResult[] = chosen.map((book) => {
			const skipped = (["series", "tags"] as const).filter(
				(f) =>
					(f === "series"
						? change.series !== undefined
						: !!change.addTags || !!change.removeTags) &&
					book.fields[f]?.locked &&
					!change.includeLocked,
			);
			let changed = false;
			if (
				change.series !== undefined &&
				!skipped.includes("series") &&
				book.series !== change.series
			) {
				book.series = change.series || undefined;
				book.fields.series = { source: "manual", locked: true };
				changed = true;
			}
			if ((change.addTags || change.removeTags) && !skipped.includes("tags")) {
				const tags = [...book.tags, ...(change.addTags ?? [])].filter(
					(t) => !change.removeTags?.includes(t),
				);
				changed ||= tags.join() !== book.tags.join();
				book.tags = [...new Set(tags)];
				book.fields.tags = { source: "manual", locked: true };
			}
			if (req.action === "fix") {
				const fixed = (req.findings ?? []).map((x) => (x === "subset-fonts" ? "large-fonts" : x));
				for (const f of book.files) {
					if (f.ereader?.findings.some((x) => fixed.includes(x))) {
						f.ereader.findings = f.ereader.findings.filter((x) => !fixed.includes(x));
						changed = true;
					}
				}
			}
			const outcome = changed
				? "changed"
				: skipped.length > 0
					? "locked"
					: "unchanged";
			return { bookId: book.id, title: book.title, outcome, skipped };
		});
		const counts: Record<string, number> = {};
		for (const r of results) {
			counts[r.outcome ?? ""] = (counts[r.outcome ?? ""] ?? 0) + 1;
		}
		const id = `bulk-${this.bulks.length + 1}`;
		this.bulks.push({
			id,
			action: req.action,
			createdAt: "2026-01-04T00:00:00Z",
			finishedAt: "2026-01-04T00:00:01Z",
			total: results.length,
			done: results.length,
			counts,
			books: results,
		});
		return Response.json({ id, total: results.length }, { status: 202 });
	}

	private answerGenres(path: string, query: URLSearchParams, body: unknown): Response {
		if (!this.session?.permissions.includes("metadata:edit")) {
			return Response.json({ error: { code: "forbidden", message: "You may not do this." } }, { status: 403 });
		}
		if (path === "/genres/suggestions") {
			const waiting: { genre: string; waiting: number }[] = [];
			for (const s of this.genreSuggestions) {
				const w = waiting.find((x) => x.genre === s.genre);
				if (w) w.waiting++;
				else waiting.push({ genre: s.genre, waiting: 1 });
			}
			const genre = query.get("genre");
			return Response.json({
				suggestions: this.genreSuggestions.filter((s) => !genre || s.genre === genre),
				waiting,
			});
		}
		const [, , bookId, , genre] = path.split("/");
		const found = this.genreSuggestions.find((s) => s.book.id === bookId && s.genre === genre);
		if (!found || !["accepted", "rejected"].includes((body as { state?: string })?.state ?? "")) {
			return Response.json(
				{ error: { code: "not_found", message: "The book has no such genre." } },
				{ status: 404 },
			);
		}
		this.genreSuggestions = this.genreSuggestions.filter((s) => s !== found);
		return new Response(null, { status: 204 });
	}

	private answerAuto(path: string, method: string, body: unknown): Response {
		if (!this.session?.permissions.includes("metadata:edit")) {
			return Response.json({ error: { code: "forbidden", message: "You may not do this." } }, { status: 403 });
		}
		if (method === "GET") {
			return Response.json(path === "/curation/cleanup" ? this.autoCleanup : this.autoMatches);
		}
		const dryRun = (body as { dryRun?: boolean } | undefined)?.dryRun === true;
		return Response.json({ runId: "run-1", dryRun }, { status: 202 });
	}

	private answerCleanup(path: string, query: URLSearchParams, body: unknown): Response {
		if (!this.session?.permissions.includes("metadata:edit")) {
			return Response.json({ error: { code: "forbidden", message: "You may not do this." } }, { status: 403 });
		}
		const counts = { authors: 0, placeholders: 0, clashes: 0, titles: 0, series: 0 };
		for (const s of this.cleanup) counts[s.kind]++;
		if (path === "/cleanup") {
			const kind = query.get("kind") ?? "authors";
			return Response.json({ suggestions: this.cleanup.filter((s) => s.kind === kind), counts });
		}
		if (path === "/cleanup/preview") {
			const kind = query.get("kind") as CleanupKind;
			const subject = query.get("subject") ?? "";
			const suggestion = this.cleanup.find((s) => s.kind === kind && s.subject === subject);
			if (!suggestion) {
				return Response.json({ error: { code: "not_found", message: "There is no such suggestion; it may have been dealt with." } }, { status: 404 });
			}
			const fix = query.get("fix");
			const books =
				this.cleanupPreviews[`${kind}:${subject}:${fix}`] ??
				this.cleanupPreviews[`${kind}:${subject}`] ??
				[];
			const offset = Number(query.get("offset") ?? 0);
			const limit = Number(query.get("limit") ?? 5);
			return Response.json({ suggestion, total: books.length, books: books.slice(offset, offset + limit) });
		}
		const req = body as { kind: CleanupKind; subject?: string; picks?: { subject: string; to?: string; fix?: string }[] };
		const chosen = this.cleanup.filter(
			(s) =>
				s.kind === req.kind &&
				(path === "/cleanup/dismiss"
					? s.subject === req.subject
					: !req.picks || req.picks.some((p) => p.subject === s.subject)),
		);
		if (chosen.length === 0) {
			return Response.json(
				{ error: { code: "not_found", message: "There is no such suggestion; it may have been dealt with." } },
				{ status: 404 },
			);
		}
		this.cleanup = this.cleanup.filter((s) => !chosen.includes(s));
		if (path === "/cleanup/dismiss") {
			return new Response(null, { status: 204 });
		}
		const total = chosen.reduce((n, s) => n + s.books, 0);
		const id = `bulk-${this.bulks.length + 1}`;
		this.bulks.push({
			id,
			action: "edit",
			createdAt: "2026-01-04T00:00:00Z",
			finishedAt: "2026-01-04T00:00:01Z",
			total,
			done: total,
			counts: { changed: total },
			books: [],
		});
		return Response.json({ id, total }, { status: 202 });
	}

	private answerMatches(
		method: string,
		path: string,
		body: CandidateApply,
	): Response {
		if (!this.session?.permissions.includes("metadata:edit")) {
			return Response.json(
				{ error: { code: "forbidden", message: "You may not do this." } },
				{ status: 403 },
			);
		}
		if (method === "GET") {
			return Response.json({ books: this.review, total: this.review.length });
		}
		const [, , id, action] = path.split("/");
		const item = this.review.find((b) =>
			b.matches.some((m) => m.matchId === id),
		);
		const match = item?.matches.find((m) => m.matchId === id);
		if (!item || !match) {
			return Response.json(
				{
					error: {
						code: "not_found",
						message: "There is no such match waiting.",
					},
				},
				{ status: 404 },
			);
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
			return Response.json(
				{ error: { code: "not_found", message: "There is no such book." } },
				{ status: 404 },
			);
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
			book.fields.cover = {
				source: "provider",
				detail: provider,
				locked: false,
			};
		}
		return Response.json(book);
	}

	private editBook(
		method: string,
		path: string,
		edit: BookEdit | undefined,
	): Response {
		const refuse = (
			status: number,
			code: string,
			message: string,
			fields?: Record<string, string>,
		) => Response.json({ error: { code, message, fields } }, { status });
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
			book.coverKey =
				method === "PUT" ? `cover-${this.requests.length}` : undefined;
			typed("cover");
			return Response.json(book);
		}
		const e = edit ?? {};
		const problems: Record<string, string> = {};
		if (e.title !== undefined && e.title.trim() === "")
			problems.title = "A book needs a title.";
		if (e.language && !/^[a-z]{2,3}(-[a-z0-9]{2,8})*$/i.test(e.language)) {
			problems.language = "Give a language code such as en, de or pt-BR.";
		}
		if (Object.keys(problems).length > 0) {
			return refuse(
				422,
				"validation_failed",
				"Some fields need attention.",
				problems,
			);
		}
		const text = [
			"title",
			"subtitle",
			"description",
			"language",
			"published",
			"publisher",
		] as const;
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
		refuse: (
			status: number,
			code: string,
			message: string,
			fields?: Record<string, string>,
		) => Response,
	): Response {
		if (!this.session) {
			return refuse(401, "unauthorized", "Sign in to continue.");
		}
		const me = this.accounts.get(this.session.username.toLowerCase());
		if (path === "/auth/password") {
			if (!me || body.currentPassword !== me.password) {
				return refuse(422, "validation_failed", "Some fields need attention.", {
					currentPassword: "That is not your current password.",
				});
			}
			if (String(body.newPassword ?? "").length < 8) {
				return refuse(422, "validation_failed", "Some fields need attention.", {
					newPassword: "A password has at least 8 characters.",
				});
			}
			me.password = String(body.newPassword);
			this.sessions = this.sessions.filter((s) => s.current);
			return new Response(null, { status: 204 });
		}
		if (path === "/auth/storage") {
			return Response.json({
				usedBytes: me?.usedBytes ?? 0,
				quotaBytes: me?.quotaBytes,
			});
		}
		if (path === "/auth/sessions") {
			return Response.json({ sessions: this.sessions });
		}
		if (path === "/auth/device-requests" && method === "GET") {
			return Response.json({ requests: this.deviceRequests });
		}
		const decided = /^\/auth\/device-requests\/([^/]+)\/(accept|decline)$/.exec(path);
		if (decided && method === "POST") {
			const request = this.deviceRequests.find((r) => r.id === decided[1]);
			if (!request) return refuse(404, "not_found", "There is no such request waiting.");
			this.deviceRequests = this.deviceRequests.filter((r) => r !== request);
			if (decided[2] === "accept") {
				this.devices.push({
					id: `d-${this.devices.length + 1}`,
					name: request.deviceName,
					createdAt: "2026-01-04T08:00:00Z",
					canUpload: body.canUpload === true,
				});
			}
			return new Response(null, { status: 204 });
		}
		if (path === "/auth/api-keys" && method === "GET") {
			return Response.json({ keys: this.apiKeys });
		}
		if (path === "/auth/api-keys" && method === "POST") {
			const scopes = (body.scopes as string[]) ?? [];
			if (scopes.length === 0) {
				return refuse(422, "validation_failed", "Some fields are not valid.", {
					scopes: "Choose at least one scope.",
				});
			}
			const key = {
				id: `k-${this.apiKeys.length + 1}`,
				name: String(body.name ?? "").trim(),
				prefix: "gotome_AbCdE",
				scopes,
				createdAt: "2026-01-04T08:00:00Z",
				expiresAt: body.expiresAt as string | undefined,
			};
			this.apiKeys.push(key);
			return Response.json(
				{ key, token: `gotome_${"x".repeat(43)}` },
				{ status: 201 },
			);
		}
		if (path.startsWith("/auth/api-keys/") && method === "DELETE") {
			this.apiKeys = this.apiKeys.filter(
				(k) => `/auth/api-keys/${k.id}` !== path,
			);
			return new Response(null, { status: 204 });
		}
		if (path === "/auth/devices" && method === "GET") {
			return Response.json({ devices: this.devices });
		}
		if (path === "/auth/devices" && method === "POST") {
			const name = String(body.name ?? "").trim();
			if (!name) {
				return refuse(422, "validation_failed", "Some fields are not valid.", {
					name: "Give the device a name.",
				});
			}
			const device = {
				id: `d-${this.devices.length + 1}`,
				name,
				createdAt: "2026-01-04T08:00:00Z",
				canUpload: body.canUpload === true,
			};
			this.devices.push(device);
			return Response.json(
				{ device, token: "abcd-efgh-ijkm-npqr-stuv", catalog: "/api/v1/opds", addresses: ["http://192.168.1.5:8080", "https://books.example.org"] },
				{ status: 201 },
			);
		}
		if (path.startsWith("/auth/devices/") && method === "DELETE") {
			this.devices = this.devices.filter(
				(d) => `/auth/devices/${d.id}` !== path,
			);
			return new Response(null, { status: 204 });
		}
		if (path.startsWith("/auth/sessions/") && method === "DELETE") {
			this.sessions = this.sessions.filter(
				(s) => `/auth/sessions/${s.id}` !== path,
			);
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
				return refuse(
					422,
					"validation_failed",
					"Some fields need attention.",
					fields,
				);
			}
			if (this.accounts.has(String(body.username).toLowerCase())) {
				return refuse(
					409,
					"conflict",
					"That user name or e-mail address is already in use.",
				);
			}
			this.withAccount(
				String(body.username),
				String(body.password),
				body.role as "admin" | "editor" | "reader",
			);
			return Response.json(
				view(this.accounts.get(String(body.username).toLowerCase()) as Account),
				{ status: 201 },
			);
		}
		const target = [...this.accounts.values()].find((a) =>
			path.startsWith(`/users/${a.user.id}`),
		);
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
		const role =
			(body.role as CurrentUser["role"] | undefined) ?? target.user.role;
		const disabled = (body.disabled as boolean | undefined) ?? target.disabled;
		const admins = [...this.accounts.values()].filter(
			(a) => a.user.role === "admin" && !a.disabled,
		);
		if (
			admins.length === 1 &&
			admins[0] === target &&
			(role !== "admin" || disabled)
		) {
			return refuse(
				409,
				"conflict",
				"This is the last administrator who can sign in. Make another account an administrator first.",
			);
		}
		if (body.password !== undefined) {
			if (String(body.password).length < 8) {
				return refuse(422, "validation_failed", "Some fields need attention.", {
					password: "A password has at least 8 characters.",
				});
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
		refuse: (
			status: number,
			code: string,
			message: string,
			fields?: Record<string, string>,
		) => Response,
	): Response {
		if (!this.session) {
			return refuse(401, "unauthorized", "Sign in to continue.");
		}
		if (!this.session.permissions.includes("settings:manage")) {
			return refuse(403, "forbidden", "You do not have permission to do that.");
		}
		if (method === "PATCH") {
			const values = body.values ?? {};
			const embedding = values["embedding.enabled"];
			if (embedding) {
				this.embedding.enabled = embedding === "on";
			}
			const model = values["embedding.model"];
			if (model) {
				for (const m of this.embeddingModels.models) {
					m.chosen = m.name === model;
				}
			}
			const language = values["metadata.language"];
			if (language && !/^[a-z]{2,3}$/i.test(language.trim())) {
				return refuse(422, "validation_failed", "Some fields need attention.", {
					"metadata.language":
						"Give a language as its two-letter code, such as en or de.",
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

	private answerScoring(
		path: string,
		method: string,
		body: unknown,
		refuse: (
			status: number,
			code: string,
			message: string,
			fields?: Record<string, string>,
		) => Response,
	): Response {
		if (!this.session?.permissions.includes("settings:manage")) {
			return refuse(403, "forbidden", "You do not have permission to do that.");
		}
		const now = new Date().toISOString();
		switch (`${method} ${path}`) {
			case "GET /metadata/scoring":
				return Response.json(this.scoring);
			case "POST /metadata/scoring/measure":
			case "POST /metadata/scoring/learn": {
				const run = { state: "queued", done: 0, total: 0, estimateSeconds: 0, startedAt: now };
				if (path.endsWith("measure")) {
					this.scoring.measure = run;
				} else {
					this.scoring.learn = run;
				}
				return new Response(null, { status: 202 });
			}
			case "PUT /metadata/scoring/import": {
				const file = body as { featureVersion?: number };
				if (file.featureVersion !== 1) {
					return refuse(422, "validation_failed", "Some fields need attention.", {
						file: "the weights are of another version of the features: it is of version 2, this GOtome uses 1.",
					});
				}
				this.importedWeights = body;
				this.scoring.imported = {
					learnedAt: now,
					books: 800,
					gotomeVersion: "0.19.0",
					languages: {},
					providers: {},
				};
				return new Response(null, { status: 204 });
			}
			case "DELETE /metadata/scoring/import":
				this.scoring.imported = undefined;
				return new Response(null, { status: 204 });
		}
		return refuse(404, "not_found", "There is nothing here.");
	}

	/**
	 * Titles and authors that contain the words, or a word one letter off:
	 * close enough to the server's trigrams for what the tests type.
	 */
	private searchNames(words: string) {
		const key = nameKey(words);
		const found = new Map<string, NameHit>();
		const add = (kind: NameHit["kind"], name: string, k: string) => {
			if (!k.includes(key)) return;
			const hit = found.get(`${kind} ${k}`);
			if (hit) {
				hit.books += 1;
			} else {
				found.set(`${kind} ${k}`, { kind, name, key: k, books: 1 });
			}
		};
		for (const b of this.books.filter((x) => !x.placeholder)) {
			for (const c of b.contributors.filter((x) => x.role === "author")) {
				add("author", c.name, personKey(c.name));
			}
			if (b.series) {
				add("series", b.series, nameKey(b.series));
			}
		}
		return [...found.values()].slice(0, 3);
	}

	private search(words: string) {
		return this.books.flatMap((b) => {
			const match = quickMatch(b, words);
			return match
				? [
						{
							id: b.id,
							libraryId: b.libraryId,
							title: b.title,
							authors: b.contributors
								.filter((c) => c.role === "author")
								.map((c) => c.name),
							formats: [],
							addedAt: b.addedAt,
							match,
						},
					]
				: [];
		});
	}

	private searchText(query: URLSearchParams) {
		const words = (query.get("q") ?? "")
			.toLowerCase()
			.split(/\s+/)
			.filter(Boolean);
		if (words.length === 0 || query.get("offset") === "0") {
			return Response.json(
				{
					error: {
						code: "validation",
						message: "Some fields need attention.",
						fields: { q: "Name a word to look for." },
					},
				},
				{ status: 422 },
			);
		}
		const tree = query.get("filter");
		const books = this.books
			.filter(
				(b) => !query.get("library") || b.libraryId === query.get("library"),
			)
			.filter((b) => !tree || matches(b, JSON.parse(tree)))
			.flatMap((b) => {
				const hits = this.passages
					.filter((p) => p.bookId === b.id)
					.map((p) => p.hit)
					.filter((h) =>
						words.every((w) => snippetText(h).toLowerCase().includes(w)),
					);
				return hits.length > 0 ? [{ book: summaryOf(b), hits }] : [];
			});
		const library = query.get("library");
		const notSearched = this.libraries
			.filter((l) => !l.fulltext && (!library || l.id === library))
			.map((l) => ({ id: l.id, name: l.name }));
		return Response.json({ books, more: false, notSearched });
	}

	private trashAction(fileID: string, action: string) {
		const refuse = (status: number, message: string) =>
			Response.json({ error: { code: "refused", message } }, { status });
		if (action === "purge" && !this.session?.permissions.includes("storage:manage")) {
			return refuse(403, "You do not have permission to do that.");
		}
		if (action === "trash") {
			const book = this.books.find((b) => b.files.some((f) => f.id === fileID));
			const file = book?.files.find((f) => f.id === fileID);
			if (!book || !file) return refuse(404, "There is no such file.");
			book.files = book.files.filter((f) => f.id !== fileID);
			this.trashed.push({
				file,
				view: {
					id: file.id, name: file.name, format: file.format, size: file.size,
					bookId: book.id, bookTitle: book.title, libraryId: book.libraryId,
					library: this.libraries.find((l) => l.id === book.libraryId)?.name ?? "",
					trashedAt: "2026-10-05T10:00:00Z", purgeAt: "2026-11-04T10:00:00Z",
					trashedBy: this.session?.username,
				},
			});
			return new Response(null, { status: 204 });
		}
		const at = this.trashed.findIndex((t) => t.view.id === fileID);
		const entry = this.trashed[at];
		if (!entry) return refuse(404, "There is no such file in the trash.");
		this.trashed.splice(at, 1);
		if (action === "restore") {
			this.books.find((b) => b.id === entry.view.bookId)?.files.push(entry.file);
		}
		return new Response(null, { status: 204 });
	}

	/** The facets the server would count, from the same books. */
	private facets(query: URLSearchParams) {
		const tree: Rule = JSON.parse(query.get("filter") || "{}");
		return FIELDS.map((field) => {
			// A field's own rules do not narrow its counts.
			const without: Rule = {
				all: (tree.all ?? []).filter((r) => fieldsOf(r).join() !== field),
			};
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
				values: [...counts]
					.map(([value, { label, count }]) => ({ value, label, count }))
					.sort((a, b) => b.count - a.count || a.value.localeCompare(b.value)),
			};
		});
	}

	private answerLibraries(
		method: string,
		id: string,
		body: Record<string, unknown>,
		refuse: (
			status: number,
			code: string,
			message: string,
			fields?: Record<string, string>,
		) => Response,
	): Response {
		if (!this.session) {
			return refuse(401, "unauthorized", "Sign in to continue.");
		}
		const manages = this.session.permissions.includes("storage:manage");
		// What a reader is shown leaves out where the files lie.
		const shown = (library: Library): Library =>
			manages
				? library
				: { ...library, rootPath: undefined, ownerId: undefined };

		if (method === "GET" && id === "") {
			return Response.json({ libraries: this.libraries.map(shown) });
		}
		if (method === "POST" && id.endsWith("/scans")) {
			const library = this.libraries.find((l) => `${l.id}/scans` === id);
			if (!this.session.permissions.includes("index:rebuild")) {
				return refuse(
					403,
					"forbidden",
					"You do not have permission to do that.",
				);
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
			this.libraries.some(
				(l) => l.id !== except && l.name.toLowerCase() === name.toLowerCase(),
			);

		if (method === "POST" && id === "") {
			const name = String(body.name ?? "").trim();
			if (name === "") {
				return invalid({ name: "Give the library a name." });
			}
			if (nameTaken(name)) {
				return invalid({ name: "Another library already has this name." });
			}
			if (
				body.mode === "external" &&
				!String(body.rootPath ?? "").startsWith("/")
			) {
				return invalid({
					rootPath: "Give the folder as a full path, such as /books.",
				});
			}
			this.withLibrary(name, {
				mode: body.mode as string,
				visibility: body.visibility as string,
				writable: body.mode === "managed",
				...(body.mode === "external"
					? { rootPath: body.rootPath as string }
					: {}),
			});
			return Response.json(this.libraries.at(-1), { status: 201 });
		}

		const membersPath = /^([^/]+)\/members(?:\/([^/]+))?$/.exec(id);
		if (membersPath) {
			const [, libraryId = "", userId] = membersPath;
			if (!this.libraries.some((l) => l.id === libraryId)) {
				return refuse(404, "not_found", "There is no such library.");
			}
			const ids = this.members.get(libraryId) ?? [];
			if (method === "GET" && userId === undefined) {
				const members = [...this.accounts.values()]
					.filter((a) => ids.includes(a.user.id))
					.map((a) => ({
						id: a.user.id,
						username: a.user.username,
						role: a.user.role,
						addedAt: "2026-01-02T00:00:00Z",
					}))
					.sort((a, b) => a.username.localeCompare(b.username));
				return Response.json({ members });
			}
			if (method === "PUT" && userId !== undefined) {
				if (![...this.accounts.values()].some((a) => a.user.id === userId)) {
					return refuse(404, "not_found", "There is no such user.");
				}
				if (!ids.includes(userId)) {
					this.members.set(libraryId, [...ids, userId]);
				}
				return new Response(null, { status: 204 });
			}
			if (method === "DELETE" && userId !== undefined) {
				this.members.set(
					libraryId,
					ids.filter((m) => m !== userId),
				);
				return new Response(null, { status: 204 });
			}
		}

		const library = this.libraries.find((l) => l.id === id);
		if (!library) {
			return refuse(404, "not_found", "There is no such library.");
		}
		if (method === "PATCH") {
			if (typeof body.name === "string" && nameTaken(body.name.trim(), id)) {
				return invalid({ name: "Another library already has this name." });
			}
			if (body.writable === true && this.readOnlyFolders.includes(library.rootPath ?? "")) {
				return invalid({
					writable: `GOtome cannot write to ${library.rootPath}: read-only file system.`,
				});
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

type Field =
	| "author"
	| "person"
	| "series"
	| "tag"
	| "language"
	| "published"
	| "status"
	| "rating"
	| "format"
	| "files"
	| "ereader"
	| "words";
const FIELDS: Field[] = [
	"author",
	"series",
	"tag",
	"language",
	"published",
	"status",
	"rating",
	"format",
	"files",
	"ereader",
];

interface Rule {
	all?: Rule[];
	any?: Rule[];
	not?: Rule;
	field?: Field;
	op?: string;
	values?: string[];
}

/** A name as the server compares it, near enough for the tests. */
function snippetText(hit: TextHit): string {
	return hit.snippet.map((p) => p.text).join("");
}

function summaryOf(b: BookDetail): BookSummary {
	const authors = b.contributors.filter((c) => c.role === "author");
	return {
		id: b.id,
		libraryId: b.libraryId,
		title: b.title,
		series: b.series,
		seriesKey: b.seriesKey,
		seriesIndex: b.seriesIndex,
		authors: authors.map((c) => c.name),
		authorKeys: authors.map((c) => personKey(c.name)),
		coverKey: b.coverKey,
		formats: [...new Set(b.files.map((f) => f.format))],
		addedAt: b.addedAt,
		status: b.reading.status,
		rating: b.reading.rating,
		placeholder: b.placeholder,
		filesMissing: b.files.length > 0 && b.files.every((f) => f.missing),
		ereaderFindings: [...new Set(b.files.flatMap((f) => f.ereader?.findings ?? []))],
	};
}

function personKey(name: string): string {
	return nameKey(name).split(" ").sort().join(" ");
}

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
			return b.contributors
				.filter((c) => c.role === "author")
				.map((c) => [nameKey(c.name), c.name]);
		case "person":
			return b.contributors
				.filter((c) => c.role === "author")
				.map((c) => [personKey(c.name), c.name]);
		case "series":
			return b.series ? [[nameKey(b.series), b.series]] : [];
		case "tag":
			return b.tags.map((tag) => [nameKey(tag), tag]);
		case "language": {
			const lang = b.language?.split("-")[0]?.toLowerCase();
			return lang ? [[lang, lang]] : [];
		}
		case "published": {
			const decade = b.published
				? String(Math.floor(Number(b.published.slice(0, 4)) / 10) * 10)
				: undefined;
			return decade ? [[decade, decade]] : [];
		}
		case "format":
			return [...new Set(b.files.map((f) => f.format))].map((f) => [f, f]);
		case "status":
			return [[b.reading.status, b.reading.status]];
		case "rating":
			return b.reading.rating
				? [[String(b.reading.rating), String(b.reading.rating)]]
				: [];
		case "files": {
			if (!b.files.some((f) => f.missing)) return [];
			return b.files.every((f) => f.missing)
				? [
						["missing", "missing"],
						["none-on-disk", "none-on-disk"],
					]
				: [["missing", "missing"]];
		}
		case "ereader": {
			const found = new Set(b.files.flatMap((f) => f.ereader?.findings ?? []));
			return [...found].map((v) => [v, v]);
		}
		case "words":
			return [];
	}
}

function fieldsOf(r: Rule): string[] {
	return [
		...new Set([
			r.field ?? "",
			...(r.all ?? []).flatMap(fieldsOf),
			...(r.any ?? []).flatMap(fieldsOf),
			...(r.not ? fieldsOf(r.not) : []),
		]),
	].filter(Boolean);
}

/** What of the book the quick search finds the words in, near enough for the tests. */
function quickMatch(b: BookDetail, words: string): "title" | "author" | undefined {
	const q = nameKey(words);
	if (q.replace(/ /g, "").length < 3) {
		return undefined;
	}
	const near = (text: string) =>
		nameKey(text).includes(q) ||
		nameKey(text)
			.split(" ")
			.some((w) => oneOff(w, q));
	if (near(b.title)) {
		return "title";
	}
	return b.contributors.some((c) => c.role === "author" && near(c.name))
		? "author"
		: undefined;
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
	if (r.field === "words") {
		return quickMatch(b, r.values?.[0] ?? "") !== undefined;
	}
	const have = valuesOf(b, r.field).map(([v]) => v);
	if (r.op === "empty") {
		return have.length === 0;
	}
	if (r.op === "between" && r.field === "rating") {
		const [from, to] = r.values ?? [];
		const stars = b.reading.rating ?? 0;
		return (
			stars > 0 &&
			(!from || stars >= Number(from)) &&
			(!to || stars <= Number(to))
		);
	}
	if (r.op === "between") {
		const year = Number(b.published?.slice(0, 4));
		const [from, to] = r.values ?? [];
		return (
			!!b.published &&
			(!from || year >= Number(from)) &&
			(!to || year <= Number(to))
		);
	}
	return (r.values ?? []).some((v) =>
		have.includes(
			r.field === "format" || r.field === "files" || r.field === "ereader" ? v : nameKey(v),
		),
	);
}
