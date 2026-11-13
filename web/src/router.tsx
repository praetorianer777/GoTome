import type { QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import {
	createRootRouteWithContext,
	createRoute,
	createRouter,
	lazyRouteComponent,
	Outlet,
	type RouterHistory,
	redirect,
	type RouteComponent,
	useRouteContext,
} from "@tanstack/react-router";
import { BASE_PATH } from "@/base";
import { can, currentUserQuery, setupNeededQuery } from "@/auth/session";
import { CLEANUP_KINDS, type CleanupKind } from "@/books/cleanup";
import { GENRES, type Genre } from "@/books/genres";
import type { BookSearch } from "@/books/book-browser";
import { picksFrom } from "@/books/filters";
import { isDemo } from "@/demo/mode";
import { Gopher } from "@/components/gopher";
import { Shell } from "@/components/shell";
import { t } from "@/i18n";
import { Book } from "@/routes/book";
import { ReaderRoute, readerSearch } from "@/routes/read";
import { Library, type LibrarySearch } from "@/routes/library";
import { Login } from "@/routes/login";
import { NotFound } from "@/routes/not-found";
import { RouteError } from "@/routes/route-error";
import type { TextSearch } from "@/routes/search";
import type { DuplicatesSearch } from "@/routes/duplicates";

// Pages other than the library, a book and sign-in load with the first visit
// to them, so the first load carries only what most visits show.
const AdminCalibre = lazyRouteComponent(() => import("@/routes/admin-calibre"), "AdminCalibre");
const AdminLibraries = lazyRouteComponent(() => import("@/routes/admin-libraries"), "AdminLibraries");
const AdminSettings = lazyRouteComponent(() => import("@/routes/admin-settings"), "AdminSettings");
const AdminUsers = lazyRouteComponent(() => import("@/routes/admin-users"), "AdminUsers");
const AdminWebhooks = lazyRouteComponent(() => import("@/routes/admin-webhooks"), "AdminWebhooks");
const AuthorPage = lazyRouteComponent(() => import("@/routes/author"), "AuthorPage");
const BulkProgress = lazyRouteComponent(() => import("@/routes/bulk"), "BulkProgress");
const Overview = lazyRouteComponent(() => import("@/routes/overview"), "Overview");
const Stats = lazyRouteComponent(() => import("@/routes/stats"), "Stats");
const CollectionPage = lazyRouteComponent(() => import("@/routes/collection"), "CollectionPage");
const DevicePage = lazyRouteComponent(() => import("@/routes/device"), "DevicePage");
const Collections = lazyRouteComponent(() => import("@/routes/collections"), "Collections");
const NewSmartShelf = lazyRouteComponent(() => import("@/routes/smart-shelf"), "NewSmartShelf");
const SmartShelfPage = lazyRouteComponent(() => import("@/routes/smart-shelf"), "SmartShelfPage");
const Wishlist = lazyRouteComponent(() => import("@/routes/wishlist"), "Wishlist");
const Recommendations = lazyRouteComponent(() => import("@/routes/recommendations"), "Recommendations");
const BookEditPage = lazyRouteComponent(() => import("@/routes/book-edit"), "BookEditPage");
const FindDetails = lazyRouteComponent(() => import("@/routes/book-find"), "FindDetails");
const Curation = lazyRouteComponent(() => import("@/routes/curation"), "Curation");
const Cleanup = lazyRouteComponent(() => import("@/routes/cleanup"), "Cleanup");
const Genres = lazyRouteComponent(() => import("@/routes/genres"), "Genres");
const Review = lazyRouteComponent(() => import("@/routes/review"), "Review");
const Jobs = lazyRouteComponent(() => import("@/routes/jobs"), "Jobs");
const NamesPage = lazyRouteComponent(() => import("@/routes/names"), "NamesPage");
const Profile = lazyRouteComponent(() => import("@/routes/profile"), "Profile");
const SearchPage = lazyRouteComponent(() => import("@/routes/search"), "SearchPage");
const SeriesPage = lazyRouteComponent(() => import("@/routes/series"), "SeriesPage");
const Duplicates = lazyRouteComponent(() => import("@/routes/duplicates"), "Duplicates");
const Setup = lazyRouteComponent(() => import("@/routes/setup"), "Setup");
const Trash = lazyRouteComponent(() => import("@/routes/trash"), "Trash");
const Highlights = lazyRouteComponent(() => import("@/routes/highlights"), "Highlights");
const Discover = lazyRouteComponent(() => import("@/routes/discover"), "Discover");
const PersonPage = lazyRouteComponent(() => import("@/routes/person"), "PersonPage");
const Listen = lazyRouteComponent(() => import("@/player/listen"), "Listen");
const Releases = lazyRouteComponent(() => import("@/routes/releases"), "Releases");
const Upload = lazyRouteComponent(() => import("@/routes/upload"), "Upload");

// The router waits for a route component's preload before it shows the
// route, so a component that only hands a lazy page its props takes the
// page's: otherwise the page would suspend after the route had been shown.
// It is read through, not copied: a lazy page drops its preload once loaded,
// and a copy would import the module again on every navigation, which makes
// even a change of the address's search wait a turn, so a ticked filter
// showed its old state until then (#370).
function loads<P>(page: Pick<RouteComponent, "preload">, component: (props: P) => ReactNode) {
	return Object.defineProperty(component, "preload", { get: () => page.preload }) as
		((props: P) => ReactNode) & Pick<RouteComponent, "preload">;
}

interface RouterContext {
	queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
	component: Outlet,
	notFoundComponent: NotFound,
	errorComponent: RouteError,
	pendingComponent: () => (
		<div className="panel mx-auto my-8 flex w-fit flex-col items-center gap-2 rounded-2xl px-8 py-6 text-center text-slate-600 dark:text-slate-400">
			<Gopher mood="reading" size={72} className="motion-safe:animate-pulse" />
			<p>{t("loading")}</p>
		</div>
	),
});

// Which page a person lands on is decided here, before anything renders:
// setup while there is no account, sign-in while nobody is signed in, the app
// otherwise. Each guard sends the other two cases where they belong.

const setupRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/setup",
	beforeLoad: async ({ context }) => {
		if (!(await context.queryClient.ensureQueryData(setupNeededQuery))) {
			throw redirect({ to: "/login" });
		}
	},
	component: Setup,
});

const loginRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/login",
	validateSearch: (search): { redirect?: string; sso?: string } => ({
		...(typeof search.redirect === "string" ? { redirect: search.redirect } : {}),
		...(typeof search.sso === "string" ? { sso: search.sso } : {}),
	}),
	beforeLoad: async ({ context }) => {
		if (await context.queryClient.ensureQueryData(setupNeededQuery)) {
			throw redirect({ to: "/setup" });
		}
		if (await context.queryClient.ensureQueryData(currentUserQuery)) {
			throw redirect({ to: "/" });
		}
	},
	component: function LoginPage() {
		const { redirect: to, sso } = loginRoute.useSearch();
		return <Login redirect={to} sso={sso} />;
	},
});

const appRoute = createRoute({
	getParentRoute: () => rootRoute,
	id: "app",
	beforeLoad: async ({ context, location }) => {
		if (await context.queryClient.ensureQueryData(setupNeededQuery)) {
			throw redirect({ to: "/setup" });
		}
		const user = await context.queryClient.ensureQueryData(currentUserQuery);
		if (!user) {
			// Remember where the person was going, to take them there after.
			throw redirect({
				to: "/login",
				search: location.href === "/" ? {} : { redirect: location.href },
			});
		}
		return { user };
	},
	component: function AppLayout() {
		const { user } = useRouteContext({ from: "/app" });
		return <Shell user={user} />;
	},
});

const SORTS = ["title", "author", "added"] as const;
const AUTHOR_SORTS = ["series", "title", "added"] as const;
const VIEWS = ["grid", "list"] as const;

function oneOf<T extends string>(values: readonly T[], value: unknown): T | undefined {
	return values.find((v) => v === value);
}

const libraryRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/",
	validateSearch: (search): LibrarySearch => ({
		...picksFrom(search),
		library: typeof search.library === "string" ? search.library : undefined,
		q: typeof search.q === "string" && search.q.trim() ? search.q : undefined,
		sort: oneOf(SORTS, search.sort),
		view: oneOf(VIEWS, search.view),
	}),
	component: function LibraryPage() {
		const navigate = libraryRoute.useNavigate();
		return (
			<Library
				user={useRouteContext({ from: "/app" }).user}
				search={libraryRoute.useSearch()}
				onSearch={(change) =>
					navigate({ search: (previous) => ({ ...previous, ...change }), replace: true })
				}
			/>
		);
	},
});

const searchRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/search",
	validateSearch: (search): TextSearch => ({
		...picksFrom(search),
		q: typeof search.q === "string" && search.q ? search.q : undefined,
		library: typeof search.library === "string" ? search.library : undefined,
		mode: search.mode === "description" ? "description" : undefined,
	}),
	component: loads(SearchPage, function SearchRoute() {
		const navigate = searchRoute.useNavigate();
		return (
			<SearchPage
				search={searchRoute.useSearch()}
				onSearch={(change) =>
					navigate({ search: (previous) => ({ ...previous, ...change }) })
				}
			/>
		);
	}),
});

const KINDS = ["sha256", "content", "isbn", "title_author", "overlap"] as const;

const duplicatesRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/duplicates",
	validateSearch: (search): DuplicatesSearch => {
		const least = Number(search.least);
		return {
			state: search.state === "kept_both" ? "kept_both" : undefined,
			contained: search.contained === true || search.contained === "true" ? true : undefined,
			kind: oneOf(KINDS, search.kind),
			least: Number.isInteger(least) && least > 0 && least <= 100 ? least : undefined,
			library: typeof search.library === "string" ? search.library : undefined,
		};
	},
	component: loads(Duplicates, function DuplicatesRoute() {
		const navigate = duplicatesRoute.useNavigate();
		return (
			<Duplicates
				user={useRouteContext({ from: "/app" }).user}
				search={duplicatesRoute.useSearch()}
				onSearch={(change) => navigate({ search: (previous) => ({ ...previous, ...change }) })}
			/>
		);
	}),
});

const authorsRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/authors",
	component: loads(NamesPage, () => <NamesPage kind="authors" />),
});

const allSeriesRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/series",
	component: loads(NamesPage, () => <NamesPage kind="series" />),
});

const authorRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/authors/$authorKey",
	validateSearch: (search): BookSearch => ({
		...picksFrom(search),
		sort: oneOf(AUTHOR_SORTS, search.sort),
		view: oneOf(VIEWS, search.view),
	}),
	component: loads(AuthorPage, function AuthorRoute() {
		const navigate = authorRoute.useNavigate();
		return (
			<AuthorPage
				user={useRouteContext({ from: "/app" }).user}
				authorKey={authorRoute.useParams().authorKey}
				search={authorRoute.useSearch()}
				onSearch={(change) =>
					navigate({ search: (previous) => ({ ...previous, ...change }), replace: true })
				}
			/>
		);
	}),
});

const seriesRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/series/$seriesKey",
	component: loads(SeriesPage, function SeriesRoute() {
		return (
			<SeriesPage
				user={useRouteContext({ from: "/app" }).user}
				seriesKey={seriesRoute.useParams().seriesKey}
			/>
		);
	}),
});

const bookRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/books/$bookId",
	component: function BookRoute() {
		return <Book id={bookRoute.useParams().bookId} />;
	},
});

const bookEditRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/books/$bookId/edit",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(BookEditPage, function BookEditRoute() {
		return <BookEditPage id={bookEditRoute.useParams().bookId} />;
	}),
});

const bookFindRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/books/$bookId/find",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(FindDetails, function BookFindRoute() {
		return <FindDetails id={bookFindRoute.useParams().bookId} />;
	}),
});

const uploadRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/upload",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "books:upload")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(Upload, function UploadPage() {
		return <Upload user={useRouteContext({ from: "/app" }).user} />;
	}),
});

const adminLibrariesRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/admin/libraries",
	// The server refuses what the person may not do; this only keeps them
	// from landing on a page whose every request would be refused.
	beforeLoad: ({ context }) => {
		if (!can(context.user, "storage:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: AdminLibraries,
});

const adminCalibreRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/admin/calibre",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "storage:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: AdminCalibre,
});

const adminSettingsRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/admin/settings",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "settings:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: AdminSettings,
});

const reviewRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/review",
	validateSearch: (search): { book?: string } => ({
		book: typeof search.book === "string" && search.book ? search.book : undefined,
	}),
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(Review, function ReviewPage() {
		const navigate = reviewRoute.useNavigate();
		return (
			<Review
				book={reviewRoute.useSearch().book}
				onBook={(book, replace) => navigate({ search: { book }, replace })}
			/>
		);
	}),
});

const cleanupRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/cleanup",
	validateSearch: (search): { kind?: CleanupKind; book?: string } => ({
		kind: oneOf(CLEANUP_KINDS, search.kind),
		book: typeof search.book === "string" && search.book ? search.book : undefined,
	}),
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(Cleanup, function CleanupPage() {
		const navigate = cleanupRoute.useNavigate();
		return (
			<Cleanup
				kind={cleanupRoute.useSearch().kind ?? "authors"}
				onKind={(kind) => navigate({ search: { kind } })}
				book={cleanupRoute.useSearch().book}
				onBook={(book, replace) =>
					navigate({ search: (previous) => ({ ...previous, book }), replace })
				}
			/>
		);
	}),
});

const genresRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/genres",
	validateSearch: (search): { genre?: Genre; book?: string } => ({
		genre: oneOf(GENRES, search.genre),
		book: typeof search.book === "string" && search.book ? search.book : undefined,
	}),
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(Genres, function GenresPage() {
		const navigate = genresRoute.useNavigate();
		return (
			<Genres
				genre={genresRoute.useSearch().genre}
				onGenre={(genre) => navigate({ search: { genre } })}
				book={genresRoute.useSearch().book}
				onBook={(book, replace) =>
					navigate({ search: (previous) => ({ ...previous, book }), replace })
				}
			/>
		);
	}),
});

const readPdfRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/books/$bookId/read/$fileId",
	validateSearch: readerSearch,
	component: ReaderRoute,
});

const listenRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/books/$bookId/listen",
	component: loads(Listen, function ListenRoute() {
		return <Listen bookId={listenRoute.useParams().bookId} />;
	}),
});

const wishlistRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/wishlist",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "personal:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: Wishlist,
});

const recommendationsRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/recommendations",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "personal:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: Recommendations,
});

const discoverRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/discover",
	validateSearch: (search): { book?: string } => ({
		book: typeof search.book === "string" && search.book ? search.book : undefined,
	}),
	beforeLoad: ({ context }) => {
		if (!can(context.user, "personal:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(Discover, function DiscoverRoute() {
		const navigate = discoverRoute.useNavigate();
		return (
			<Discover
				open={discoverRoute.useSearch().book}
				onOpen={(book) => navigate({ search: book ? { book } : {} })}
			/>
		);
	}),
});

const releasesRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/releases",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "personal:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: Releases,
});

const highlightsRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/highlights",
	validateSearch: (search): { q?: string } => ({
		q: typeof search.q === "string" && search.q ? search.q : undefined,
	}),
	beforeLoad: ({ context }) => {
		if (!can(context.user, "personal:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(Highlights, function HighlightsPage() {
		const navigate = highlightsRoute.useNavigate();
		return (
			<Highlights
				q={highlightsRoute.useSearch().q}
				onSearch={(q) => navigate({ search: { q } })}
			/>
		);
	}),
});

const overviewRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/overview",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "library:read")) {
			throw redirect({ to: "/" });
		}
	},
	component: Overview,
});

const statsRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/stats",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "personal:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: Stats,
});

const collectionsRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/collections",
	component: Collections,
});

const personRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/people/$username",
	component: loads(PersonPage, function PersonRoute() {
		return <PersonPage username={personRoute.useParams().username} />;
	}),
});

const collectionRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/collections/$collectionId",
	component: loads(CollectionPage, function CollectionRoute() {
		return <CollectionPage collectionId={collectionRoute.useParams().collectionId} />;
	}),
});

const newSmartShelfRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/smart-shelves/new",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "personal:manage")) {
			throw redirect({ to: "/collections" });
		}
	},
	component: NewSmartShelf,
});

const smartShelfRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/smart-shelves/$shelfId",
	component: loads(SmartShelfPage, function SmartShelfRoute() {
		return <SmartShelfPage shelfId={smartShelfRoute.useParams().shelfId} />;
	}),
});

const bulkRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/bulk/$bulkId",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(BulkProgress, function BulkRoute() {
		return <BulkProgress id={bulkRoute.useParams().bulkId} />;
	}),
});

const jobsRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/jobs",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "index:rebuild")) {
			throw redirect({ to: "/" });
		}
	},
	component: Jobs,
});

const curationRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/curation",
	validateSearch: (search): { run?: string } => ({
		run: typeof search.run === "string" && search.run ? search.run : undefined,
	}),
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(Curation, function CurationPage() {
		return <Curation run={curationRoute.useSearch().run} />;
	}),
});

const trashRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/trash",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(Trash, function TrashPage() {
		return <Trash user={useRouteContext({ from: "/app" }).user} />;
	}),
});

const adminUsersRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/admin/users",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "users:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(AdminUsers, function AdminUsersPage() {
		return <AdminUsers user={useRouteContext({ from: "/app" }).user} />;
	}),
});

const adminWebhooksRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/admin/webhooks",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "settings:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: loads(AdminWebhooks, function AdminWebhooksPage() {
		return <AdminWebhooks />;
	}),
});

const deviceRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/devices/$deviceId",
	component: loads(DevicePage, function DeviceRoute() {
		return <DevicePage deviceId={deviceRoute.useParams().deviceId} />;
	}),
});

const profileRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/profile",
	validateSearch: (search): { sso?: string } =>
		typeof search.sso === "string" ? { sso: search.sso } : {},
	component: loads(Profile, function ProfilePage() {
		return (
			<Profile
				user={useRouteContext({ from: "/app" }).user}
				sso={profileRoute.useSearch().sso}
			/>
		);
	}),
});

const routeTree = rootRoute.addChildren([
	setupRoute,
	loginRoute,
	appRoute.addChildren([libraryRoute, authorsRoute, allSeriesRoute, authorRoute, seriesRoute, searchRoute, duplicatesRoute, bookRoute, bookEditRoute, bookFindRoute, uploadRoute, reviewRoute, cleanupRoute, curationRoute, genresRoute, readPdfRoute, listenRoute, wishlistRoute, recommendationsRoute, releasesRoute, highlightsRoute, discoverRoute, overviewRoute, statsRoute, collectionsRoute, collectionRoute, personRoute, newSmartShelfRoute, smartShelfRoute, bulkRoute, jobsRoute, trashRoute, adminLibrariesRoute, adminCalibreRoute, adminUsersRoute, adminWebhooksRoute, adminSettingsRoute, profileRoute, deviceRoute]),
]);

/** history is for tests, which navigate in memory rather than in a browser. */
export function makeRouter(queryClient: QueryClient, history?: RouterHistory) {
	// Back from a book returns to where the list was scrolled to: its pages
	// are still cached, so the list is as long as it was.
	return createRouter({
		routeTree,
		context: { queryClient },
		history,
		defaultPreload: false,
		scrollRestoration: true,
		// The demo lives under the Pages project's path, not at the root.
		basepath: isDemo ? import.meta.env.BASE_URL : BASE_PATH === "/" ? undefined : BASE_PATH,
	});
}

// Registers the router's type, so links and params are checked against the
// routes that exist.
declare module "@tanstack/react-router" {
	interface Register {
		router: ReturnType<typeof makeRouter>;
	}
}
