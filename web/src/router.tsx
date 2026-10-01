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
import { picksFrom } from "@/books/filters";
import { Shell } from "@/components/shell";
import { t } from "@/i18n";
import { AdminLibraries } from "@/routes/admin-libraries";
import { AdminSettings } from "@/routes/admin-settings";
import { AdminUsers } from "@/routes/admin-users";
import { Book } from "@/routes/book";
import { BookEditPage } from "@/routes/book-edit";
import { Jobs } from "@/routes/jobs";
import { Library, type LibrarySearch } from "@/routes/library";
import { Login } from "@/routes/login";
import { NotFound } from "@/routes/not-found";
import { Profile } from "@/routes/profile";
import { RouteError } from "@/routes/route-error";
import { Setup } from "@/routes/setup";
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
	validateSearch: (search): { redirect?: string } =>
		typeof search.redirect === "string" ? { redirect: search.redirect } : {},
	beforeLoad: async ({ context }) => {
		if (await context.queryClient.ensureQueryData(setupNeededQuery)) {
			throw redirect({ to: "/setup" });
		}
		if (await context.queryClient.ensureQueryData(currentUserQuery)) {
			throw redirect({ to: "/" });
		}
	},
	component: function LoginPage() {
		return <Login redirect={loginRoute.useSearch().redirect} />;
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
	component: function ProfilePage() {
		return <Profile user={useRouteContext({ from: "/app" }).user} />;
	},
});

const routeTree = rootRoute.addChildren([
	setupRoute,
	loginRoute,
	appRoute.addChildren([libraryRoute, bookRoute, bookEditRoute, uploadRoute, jobsRoute, adminLibrariesRoute, adminUsersRoute, adminSettingsRoute, profileRoute]),
]);

/** history is for tests, which navigate in memory rather than in a browser. */
export function makeRouter(queryClient: QueryClient, history?: RouterHistory) {
	return createRouter({ routeTree, context: { queryClient }, history, defaultPreload: false });
}

// Registers the router's type, so links and params are checked against the
// routes that exist.
declare module "@tanstack/react-router" {
	interface Register {
		router: ReturnType<typeof makeRouter>;
	}
}
