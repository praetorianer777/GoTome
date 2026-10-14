import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { useState } from "react";
import { ApiError } from "@/api/client";
import { useLocale } from "@/i18n";
import { makeRouter } from "@/router";

export function makeQueryClient() {
	return new QueryClient({
		defaultOptions: {
			queries: {
				// A refusal will be the same refusal a moment later; only failures
				// that may pass, such as a server restarting, are worth another try.
				retry: (failures, error) =>
					failures < 2 && !(error instanceof ApiError && error.status < 500),
				staleTime: 30_000,
			},
		},
	});
}

interface AppProps {
	/** Tests pass a client without retries and a router on a memory history. */
	queryClient?: QueryClient;
	router?: ReturnType<typeof makeRouter>;
}

export function App(props: AppProps) {
	const [queryClient] = useState(() => props.queryClient ?? makeQueryClient());
	const [router] = useState(() => props.router ?? makeRouter(queryClient));
	// Texts are looked up as components render, so another language renders
	// the whole app again; what the queries hold stays.
	const locale = useLocale();
	return (
		<QueryClientProvider client={queryClient}>
			<RouterProvider key={locale} router={router} />
		</QueryClientProvider>
	);
}
