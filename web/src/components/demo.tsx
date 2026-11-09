import { Link } from "@tanstack/react-router";
import { Gopher } from "@/components/gopher";
import { t } from "@/i18n";

const QUICK_START = "https://github.com/praetorianer777/GoTome#running-it";

export function DemoBanner() {
	return (
		<div
			role="note"
			className="flex flex-wrap items-center justify-center gap-x-3 gap-y-1 bg-brand-strong px-4 py-2 text-center text-sm text-white"
		>
			<span>{t("demo.banner")}</span>
			<a href={QUICK_START} className="font-semibold underline underline-offset-4">
				{t("demo.install")}
			</a>
		</div>
	);
}

export function DemoNote() {
	return (
		<div className="flex flex-col items-center gap-4 py-10 text-center">
			<Gopher mood="confused" size={120} />
			<h1 className="text-2xl font-bold">{t("demo.note.title")}</h1>
			<p className="max-w-md text-slate-600 dark:text-slate-400">{t("demo.note.body")}</p>
			<Link to="/" className="text-brand-strong underline underline-offset-4 dark:text-brand">
				{t("demo.note.back")}
			</Link>
		</div>
	);
}
