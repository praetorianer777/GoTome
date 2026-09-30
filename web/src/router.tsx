import {
	createRootRoute,
	createRoute,
	createRouter,
	Outlet,
} from "@tanstack/react-router";
import { Home } from "@/routes/home";
import { NotFound } from "@/routes/not-found";

const rootRoute = createRootRoute({
	component: Outlet,
	notFoundComponent: NotFound,
});

const homeRoute = createRoute({
	getParentRoute: () => rootRoute,
	path: "/",
	component: Home,
});

const routeTree = rootRoute.addChildren([homeRoute]);

export function makeRouter() {
	return createRouter({ routeTree });
}

// Registers the router's type, so links and params are checked against the
// routes that exist.
declare module "@tanstack/react-router" {
	interface Register {
		router: ReturnType<typeof makeRouter>;
	}
}
