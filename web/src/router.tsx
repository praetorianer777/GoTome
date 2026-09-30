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
import { currentUserQuery, setupNeededQuery } from "@/auth/session";
import { Shell } from "@/components/shell";
import { t } from "@/i18n";
import { Library } from "@/routes/library";
import { Login } from "@/routes/login";
import { NotFound } from "@/routes/not-found";
import { RouteError } from "@/routes/route-error";
import { Setup } from "@/routes/setup";

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

const libraryRoute = createRoute({
	getParentRoute: () => appRoute,
	path: "/",
	component: Library,
});

const routeTree = rootRoute.addChildren([
	setupRoute,
	loginRoute,
	appRoute.addChildren([libraryRoute]),
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
