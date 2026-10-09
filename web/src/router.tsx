import type { QueryClient } from "@tanstack/react-query";
import {
	createRootRouteWithContext,
	createRoute,
	createRouter,
	Outlet,
	type RouterHistory,
	redirect,
	useRouteContext,
} from "@tanstack/react-router";
import { can, currentUserQuery, setupNeededQuery } from "@/auth/session";
import { CLEANUP_KINDS, type CleanupKind } from "@/books/cleanup";
import { picksFrom } from "@/books/filters";
import { Shell } from "@/components/shell";
import { t } from "@/i18n";
import { AdminLibraries } from "@/routes/admin-libraries";
import { AdminSettings } from "@/routes/admin-settings";
import { AdminUsers } from "@/routes/admin-users";
import { Book } from "@/routes/book";
import { Listen } from "@/player/controls";
import { BulkProgress } from "@/routes/bulk";
import { Stats } from "@/routes/stats";
import { ReaderRoute, readerSearch } from "@/routes/read";
import { CollectionPage } from "@/routes/collection";
import { Collections } from "@/routes/collections";
import { NewSmartShelf, SmartShelfPage } from "@/routes/smart-shelf";
import { Wishlist } from "@/routes/wishlist";
import { BookEditPage } from "@/routes/book-edit";
import { FindDetails } from "@/routes/book-find";
import { Cleanup } from "@/routes/cleanup";
import { Review } from "@/routes/review";
import { Jobs } from "@/routes/jobs";
import { Library, type LibrarySearch } from "@/routes/library";
import { Login } from "@/routes/login";
import { NotFound } from "@/routes/not-found";
import { Profile } from "@/routes/profile";
import { RouteError } from "@/routes/route-error";
import { SearchPage, type TextSearch } from "@/routes/search";
import { Duplicates, type DuplicatesSearch } from "@/routes/duplicates";
import { Setup } from "@/routes/setup";
import { Trash } from "@/routes/trash";
import { Releases } from "@/routes/releases";
import { Upload } from "@/routes/upload";

interface RouterContext {
	queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
	component: Outlet,
	notFoundComponent: NotFound,
	errorComponent: RouteError,
	pendingComponent: () => (
		<p className="p-6 text-center text-slate-500">{t("loading")}</p>
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
	}),
	component: function SearchRoute() {
		const navigate = searchRoute.useNavigate();
		return (
			<SearchPage
				search={searchRoute.useSearch()}
				onSearch={(change) =>
					navigate({ search: (previous) => ({ ...previous, ...change }) })
				}
			/>
		);
	},
});

const KINDS = ["sha256", "content", "isbn", "title_author", "overlap"] as const;

const duplicatesRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/duplicates",
	validateSearch: (search): DuplicatesSearch => {
		const least = Number(search.least);
		return {
			state: search.state === "kept_both" ? "kept_both" : undefined,
			kind: oneOf(KINDS, search.kind),
			least: Number.isInteger(least) && least > 0 && least <= 100 ? least : undefined,
			library: typeof search.library === "string" ? search.library : undefined,
		};
	},
	component: function DuplicatesRoute() {
		const navigate = duplicatesRoute.useNavigate();
		return (
			<Duplicates
				user={useRouteContext({ from: "/app" }).user}
				search={duplicatesRoute.useSearch()}
				onSearch={(change) => navigate({ search: (previous) => ({ ...previous, ...change }) })}
			/>
		);
	},
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
	component: function BookEditRoute() {
		return <BookEditPage id={bookEditRoute.useParams().bookId} />;
	},
});

const bookFindRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/books/$bookId/find",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: function BookFindRoute() {
		return <FindDetails id={bookFindRoute.useParams().bookId} />;
	},
});

const uploadRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/upload",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "books:upload")) {
			throw redirect({ to: "/" });
		}
	},
	component: function UploadPage() {
		return <Upload user={useRouteContext({ from: "/app" }).user} />;
	},
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
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: Review,
});

const cleanupRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/cleanup",
	validateSearch: (search): { kind?: CleanupKind } => ({
		kind: oneOf(CLEANUP_KINDS, search.kind),
	}),
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: function CleanupPage() {
		const navigate = cleanupRoute.useNavigate();
		return (
			<Cleanup
				kind={cleanupRoute.useSearch().kind ?? "authors"}
				onKind={(kind) => navigate({ search: { kind } })}
			/>
		);
	},
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
	component: function ListenRoute() {
		return <Listen bookId={listenRoute.useParams().bookId} />;
	},
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

const collectionRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/collections/$collectionId",
	component: function CollectionRoute() {
		return <CollectionPage collectionId={collectionRoute.useParams().collectionId} />;
	},
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
	component: function SmartShelfRoute() {
		return <SmartShelfPage shelfId={smartShelfRoute.useParams().shelfId} />;
	},
});

const bulkRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/bulk/$bulkId",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: function BulkRoute() {
		return <BulkProgress id={bulkRoute.useParams().bulkId} />;
	},
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

const trashRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/trash",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "metadata:edit")) {
			throw redirect({ to: "/" });
		}
	},
	component: function TrashPage() {
		return <Trash user={useRouteContext({ from: "/app" }).user} />;
	},
});

const adminUsersRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/admin/users",
	beforeLoad: ({ context }) => {
		if (!can(context.user, "users:manage")) {
			throw redirect({ to: "/" });
		}
	},
	component: function AdminUsersPage() {
		return <AdminUsers user={useRouteContext({ from: "/app" }).user} />;
	},
});

const profileRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/profile",
	validateSearch: (search): { sso?: string } =>
		typeof search.sso === "string" ? { sso: search.sso } : {},
	component: function ProfilePage() {
		return (
			<Profile
				user={useRouteContext({ from: "/app" }).user}
				sso={profileRoute.useSearch().sso}
			/>
		);
	},
});

const routeTree = rootRoute.addChildren([
	setupRoute,
	loginRoute,
	appRoute.addChildren([libraryRoute, searchRoute, duplicatesRoute, bookRoute, bookEditRoute, bookFindRoute, uploadRoute, reviewRoute, cleanupRoute, readPdfRoute, listenRoute, wishlistRoute, releasesRoute, statsRoute, collectionsRoute, collectionRoute, newSmartShelfRoute, smartShelfRoute, bulkRoute, jobsRoute, trashRoute, adminLibrariesRoute, adminUsersRoute, adminSettingsRoute, profileRoute]),
]);

/** history is for tests, which navigate in memory rather than in a browser. */
export function makeRouter(queryClient: QueryClient, history?: RouterHistory) {
	// Back from a book returns to where the list was scrolled to: its pages
	// are still cached, so the list is as long as it was.
	return createRouter({ routeTree, context: { queryClient }, history, defaultPreload: false, scrollRestoration: true });
}

// Registers the router's type, so links and params are checked against the
// routes that exist.
declare module "@tanstack/react-router" {
	interface Register {
		router: ReturnType<typeof makeRouter>;
	}
}
