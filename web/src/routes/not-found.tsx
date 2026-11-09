import { Link } from "@tanstack/react-router";
import { Gopher } from "@/components/gopher";
import { t } from "@/i18n";

export function NotFound() {
	return (
		<main className="mx-auto flex min-h-dvh max-w-2xl items-center px-4">
			<div className="panel flex w-full flex-col items-center gap-4 rounded-2xl px-6 py-10 text-center">
			<Gopher mood="confused" size={160} />
			<h1 className="text-3xl font-bold">{t("notFound.title")}</h1>
			<p className="text-slate-600 dark:text-slate-400">{t("notFound.body")}</p>
			<Link
				to="/"
				className="text-brand-strong underline underline-offset-4 dark:text-brand"
			>
				{t("notFound.back")}
			</Link>
			</div>
		</main>
	);
}
