import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { useState } from "react";
import { ApiError } from "@/api/client";
import { makeRouter } from "@/router";

function makeQueryClient() {
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

export function App({
	router = makeRouter(),
}: {
	router?: ReturnType<typeof makeRouter>;
}) {
	const [queryClient] = useState(makeQueryClient);
	return (
		<QueryClientProvider client={queryClient}>
			<RouterProvider router={router} />
		</QueryClientProvider>
	);
}
