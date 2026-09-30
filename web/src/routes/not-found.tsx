import { Link } from "@tanstack/react-router";
import { t } from "@/i18n";

export function NotFound() {
	return (
		<main className="mx-auto flex min-h-dvh max-w-2xl flex-col items-center justify-center gap-4 px-4 text-center">
			<h1 className="text-3xl font-bold">{t("notFound.title")}</h1>
			<p className="text-slate-600 dark:text-slate-400">{t("notFound.body")}</p>
			<Link
				to="/"
				className="text-brand-strong underline underline-offset-4 dark:text-brand"
			>
				{t("notFound.back")}
			</Link>
		</main>
	);
}
