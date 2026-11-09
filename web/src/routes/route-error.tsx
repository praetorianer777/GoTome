import { useRouter } from "@tanstack/react-router";
import { Gopher } from "@/components/gopher";
import { errorMessage } from "@/components/form";
import { t } from "@/i18n";

/** Shown when a page could not even start: the server was not reachable, say. */
export function RouteError({ error }: { error: unknown }) {
	const router = useRouter();
	return (
		<main className="mx-auto flex min-h-dvh max-w-2xl items-center px-4">
			<div className="panel flex w-full flex-col items-center gap-4 rounded-2xl px-6 py-10 text-center">
			<Gopher mood="confused" size={160} />
			<h1 className="text-2xl font-bold">{t("error.loading")}</h1>
			<p role="alert" className="text-slate-600 dark:text-slate-400">
				{errorMessage(error)}
			</p>
			<button
				type="button"
				onClick={() => router.invalidate()}
				className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
			>
				{t("error.retry")}
			</button>
			</div>
		</main>
	);
}
