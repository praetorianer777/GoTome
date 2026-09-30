import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type CurrentUser, can } from "@/auth/session";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { librariesQuery } from "@/libraries/api";

export function Library({ user }: { user: CurrentUser }) {
	const libraries = useQuery(librariesQuery);

	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-semibold">{t("library.title")}</h1>
			{libraries.isPending && <p className="text-slate-500">{t("loading")}</p>}
			<FormError error={libraries.error} />

			{libraries.data?.length === 0 && (
				<div className="rounded-lg border border-dashed border-slate-300 px-6 py-16 text-center dark:border-slate-700">
					<h2 className="text-lg font-medium">{t("library.none.title")}</h2>
					<p className="mx-auto mt-2 max-w-md text-slate-600 dark:text-slate-400">
						{t(
							can(user, "storage:manage")
								? "library.none.admin"
								: "library.none.other",
						)}
					</p>
					{can(user, "storage:manage") && (
						<Link
							to="/admin/libraries"
							className="mt-4 inline-block rounded-md bg-brand-strong px-4 py-2 font-medium text-white hover:bg-sky-800"
						>
							{t("library.none.add")}
						</Link>
					)}
				</div>
			)}

			{libraries.data?.map((library) => (
				<section
					key={library.id}
					aria-labelledby={`library-${library.id}`}
					className="flex flex-col gap-3"
				>
					<h2 id={`library-${library.id}`} className="text-lg font-medium">
						{library.name}
					</h2>
					<div className="rounded-lg border border-dashed border-slate-300 px-6 py-10 text-center dark:border-slate-700">
						<h3 className="font-medium">{t("library.empty.title")}</h3>
						<p className="mx-auto mt-2 max-w-md text-slate-600 dark:text-slate-400">
							{t("library.empty.body")}
						</p>
					</div>
				</section>
			))}
		</div>
	);
}
