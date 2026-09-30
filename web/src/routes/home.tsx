import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";

export function Home() {
	const version = useQuery({
		queryKey: ["version"],
		queryFn: async () => (await api.GET("/version")).data,
	});

	return (
		<main className="mx-auto flex min-h-dvh max-w-2xl flex-col items-center justify-center gap-4 px-4 text-center">
			<h1 className="text-5xl font-bold tracking-tight">
				<span className="text-brand">GO</span>tome
			</h1>
			<p className="text-slate-400">
				Your library is on its way. Nothing to browse yet.
			</p>
			<p className="text-sm text-slate-500" aria-live="polite">
				{version.isPending && "Asking the server…"}
				{version.isError &&
					`The server does not answer: ${version.error.message}`}
				{version.data && `Server version ${version.data.version}`}
			</p>
		</main>
	);
}
