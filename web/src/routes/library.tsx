import { t } from "@/i18n";

export function Library() {
	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-semibold">{t("library.title")}</h1>
			<div className="rounded-lg border border-dashed border-slate-300 px-6 py-16 text-center dark:border-slate-700">
				<h2 className="text-lg font-medium">{t("library.empty.title")}</h2>
				<p className="mx-auto mt-2 max-w-md text-slate-600 dark:text-slate-400">
					{t("library.empty.body")}
				</p>
			</div>
		</div>
	);
}
