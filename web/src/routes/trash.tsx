import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { type CurrentUser, can } from "@/auth/session";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { formatDate, formatSize } from "@/lib/format";
import {
	type TrashedFile,
	trashQuery,
	usePurgeFile,
	useRestoreFile,
} from "@/trash/api";

const button =
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** The files moved to the trash, to restore or, for an administrator, delete. */
export function Trash({ user }: { user: CurrentUser }) {
	const files = useQuery(trashQuery);
	const canPurge = can(user, "storage:manage");
	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-semibold">{t("trash.title")}</h1>
				<p className="mt-1 max-w-2xl text-slate-600 dark:text-slate-400">
					{t("trash.intro")}
				</p>
			</div>
			<FormError error={files.error} />
			{files.isPending && <p className="text-slate-500">{t("loading")}</p>}
			{files.data?.length === 0 && (
				<p role="status" className="text-slate-600 dark:text-slate-400">
					{t("trash.empty")}
				</p>
			)}
			{files.data && files.data.length > 0 && (
				<ul
					aria-label={t("trash.list")}
					className="flex flex-col divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800"
				>
					{files.data.map((file) => (
						<Trashed key={file.id} file={file} canPurge={canPurge} />
					))}
				</ul>
			)}
		</div>
	);
}

function Trashed({ file, canPurge }: { file: TrashedFile; canPurge: boolean }) {
	const restore = useRestoreFile();
	const purge = usePurgeFile();
	const [confirming, setConfirming] = useState(false);
	return (
		<li
			aria-label={file.name}
			className="flex flex-col gap-2 px-4 py-3 text-sm"
		>
			<div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
				<span className="min-w-0 flex-1 break-all font-medium">
					{file.name}
				</span>
				<span className="text-slate-600 dark:text-slate-400">
					{formatSize(file.size)}
				</span>
			</div>
			<p className="text-slate-600 dark:text-slate-400">
				<Link
					to="/books/$bookId"
					params={{ bookId: file.bookId }}
					className="text-brand-strong underline underline-offset-4 dark:text-brand"
				>
					{file.bookTitle}
				</Link>
				{" · "}
				{file.library}
			</p>
			<p className="text-slate-600 dark:text-slate-400">
				{file.trashedBy
					? t("trash.byWhom", {
							when: formatDate(file.trashedAt),
							who: file.trashedBy,
						})
					: t("trash.when", { when: formatDate(file.trashedAt) })}{" "}
				{t("trash.purgeAt", { when: formatDate(file.purgeAt) })}
			</p>
			<div className="flex flex-wrap gap-2">
				<button
					type="button"
					disabled={restore.isPending}
					onClick={() => restore.mutate(file.id)}
					className={button}
				>
					{t("trash.restore")}
				</button>
				{canPurge &&
					(confirming ? (
						<>
							<button
								type="button"
								disabled={purge.isPending}
								onClick={() => purge.mutate(file.id)}
								className="rounded-md bg-red-700 px-3 py-1 text-sm font-medium text-white hover:bg-red-800 disabled:opacity-60"
							>
								{t("trash.purgeConfirm")}
							</button>
							<button
								type="button"
								onClick={() => setConfirming(false)}
								className={button}
							>
								{t("trash.purgeCancel")}
							</button>
						</>
					) : (
						<button
							type="button"
							onClick={() => setConfirming(true)}
							className={button}
						>
							{t("trash.purge")}
						</button>
					))}
			</div>
			<FormError error={restore.error ?? purge.error} />
		</li>
	);
}
