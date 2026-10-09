import { useInfiniteQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import {
	CLEANUP_KINDS,
	type CleanupKind,
	type CleanupSuggestion,
	cleanupQuery,
	useApplyCleanup,
	useDismissCleanup,
} from "@/books/cleanup";
import { FormError } from "@/components/form";
import { t } from "@/i18n";

const button =
	"rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-60 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-slate-300";
const quietButton =
	"rounded-md border border-slate-300 px-3 py-2 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";
const pill =
	"rounded-full border border-slate-300 px-3 py-1 aria-pressed:border-slate-900 aria-pressed:bg-slate-900 aria-pressed:text-white dark:border-slate-600 dark:aria-pressed:border-slate-100 dark:aria-pressed:bg-slate-100 dark:aria-pressed:text-slate-900";

/** What looks wrong in how the books are described, with a fix for each. */
export function Cleanup({
	kind,
	onKind,
}: {
	kind: CleanupKind;
	onKind: (kind: CleanupKind) => void;
}) {
	const navigate = useNavigate();
	const list = useInfiniteQuery(cleanupQuery(kind));
	const apply = useApplyCleanup();
	const [applied, setApplied] = useState<{ id: string; total: number }>();
	// A fix is made by a job: until it has run, the server still suggests
	// it, so the page leaves out what was applied here.
	const [gone, setGone] = useState<Set<string>>(new Set());
	const [sure, setSure] = useState(false);

	const pages = list.data?.pages ?? [];
	const counts = pages[0]?.counts;
	const total = counts?.[kind] ?? 0;
	const suggestions = pages
		.flatMap((p) => p?.suggestions ?? [])
		.filter((s) => !gone.has(`${s.kind}:${s.subject}`));

	const done = (
		s: CleanupSuggestion,
		result?: { id: string; total: number },
	) => {
		setGone((g) => new Set(g).add(`${s.kind}:${s.subject}`));
		if (result) {
			setApplied(result);
		}
	};

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-semibold">{t("cleanup.title")}</h1>
				<p className="mt-1 max-w-2xl text-slate-600 dark:text-slate-400">
					{t("cleanup.intro")}
				</p>
			</div>
			<fieldset className="flex flex-wrap gap-2 text-sm">
				<legend className="sr-only">{t("cleanup.kinds")}</legend>
				{CLEANUP_KINDS.map((k) => (
					<button
						key={k}
						type="button"
						aria-pressed={k === kind}
						onClick={() => {
							setSure(false);
							onKind(k);
						}}
						className={pill}
					>
						{t(`cleanup.kind.${k}`, { count: counts?.[k] ?? "…" })}
					</button>
				))}
			</fieldset>
			<p className="max-w-2xl">{t(`cleanup.about.${kind}`)}</p>
			{applied && (
				<p role="status" className="text-sm">
					{t("cleanup.applying", { count: applied.total })}{" "}
					<Link
						to="/bulk/$bulkId"
						params={{ bulkId: applied.id }}
						className="underline"
					>
						{t("cleanup.progress")}
					</Link>
				</p>
			)}
			{list.isPending && <p className="text-slate-500">{t("loading")}</p>}
			<FormError error={list.error ?? apply.error} />
			{list.data && suggestions.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">
					{t("cleanup.none")}
				</p>
			)}
			{suggestions.length > 0 && (
				<div className="flex flex-wrap items-center gap-2">
					{sure ? (
						<>
							<span>{t("cleanup.applyAllSure", { count: total })}</span>
							<button
								type="button"
								className={button}
								disabled={apply.isPending}
								onClick={() =>
									apply.mutate(
										{ kind },
										{
											onSuccess: (result) =>
												result &&
												navigate({
													to: "/bulk/$bulkId",
													params: { bulkId: result.id },
												}),
										},
									)
								}
							>
								{t("cleanup.yes")}
							</button>
							<button
								type="button"
								className={quietButton}
								onClick={() => setSure(false)}
							>
								{t("cleanup.no")}
							</button>
						</>
					) : (
						<button
							type="button"
							className={quietButton}
							onClick={() => setSure(true)}
						>
							{t("cleanup.applyAll", { count: total })}
						</button>
					)}
				</div>
			)}
			<ul className="flex flex-col gap-3">
				{suggestions.map((s) => (
					<Suggestion
						key={`${s.kind}:${s.subject}`}
						suggestion={s}
						onDone={done}
					/>
				))}
			</ul>
			{list.hasNextPage && (
				<button
					type="button"
					className={`${quietButton} self-start`}
					disabled={list.isFetchingNextPage}
					onClick={() => list.fetchNextPage()}
				>
					{list.isFetchingNextPage ? t("loading") : t("cleanup.more")}
				</button>
			)}
		</div>
	);
}

function Suggestion({
	suggestion: s,
	onDone,
}: {
	suggestion: CleanupSuggestion;
	onDone: (
		s: CleanupSuggestion,
		applied?: { id: string; total: number },
	) => void;
}) {
	const apply = useApplyCleanup();
	const dismiss = useDismissCleanup();
	const [keep, setKeep] = useState(s.suggested ?? "");
	const [other, setOther] = useState("");
	const [own, setOwn] = useState(false);
	const name = s.found[0]?.name ?? "";
	const to = own ? other : keep;
	const busy = apply.isPending || dismiss.isPending;

	return (
		<li className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800">
			{s.fix === "merge" && (
				<fieldset className="flex flex-col gap-1">
					<legend className="mb-1 font-medium">{t("cleanup.keep")}</legend>
					{s.found.map((f) => (
						<label key={f.name} className="flex items-center gap-2">
							<input
								type="radio"
								name={`keep-${s.subject}`}
								checked={!own && keep === f.name}
								onChange={() => {
									setOwn(false);
									setKeep(f.name);
								}}
							/>
							<span>{f.name}</span>{" "}
							<span className="text-sm text-slate-500">
								{t("cleanup.books", { count: f.books })}
							</span>
						</label>
					))}
					<label className="flex flex-wrap items-center gap-2">
						<input
							type="radio"
							name={`keep-${s.subject}`}
							checked={own}
							onChange={() => setOwn(true)}
						/>
						<span>{t("cleanup.other")}</span>
						<input
							type="text"
							aria-label={t("cleanup.otherName")}
							value={other}
							onChange={(e) => {
								setOther(e.target.value);
								setOwn(true);
							}}
							className="rounded-md border border-slate-300 px-2 py-1 text-sm dark:border-slate-600 dark:bg-slate-900"
						/>
					</label>
				</fieldset>
			)}
			{s.kind === "placeholders" && (
				<p>
					{t("cleanup.placeholder", { name })}{" "}
					<span className="text-sm text-slate-500">
						{t("cleanup.books", { count: s.books })}
					</span>
				</p>
			)}
			{s.kind === "clashes" && (
				<p>
					{t(
						s.fix === "removeAuthor"
							? "cleanup.removeAuthor"
							: "cleanup.clearSeries",
						{ name },
					)}{" "}
					<span className="text-sm text-slate-500">
						{t("cleanup.books", { count: s.books })}
					</span>
				</p>
			)}
			{s.fix === "retitle" && (
				<div className="flex flex-col gap-1">
					{s.book ? (
						<Link
							to="/books/$bookId"
							params={{ bookId: s.book }}
							className="hover:underline"
						>
							{name}
						</Link>
					) : (
						<span>{name}</span>
					)}
					<p>
						<span className="text-sm text-slate-500">
							{t("cleanup.retitle")}
						</span>{" "}
						<span className="font-medium">{s.suggested}</span>
					</p>
				</div>
			)}
			<FormError error={apply.error ?? dismiss.error} />
			<div className="flex flex-wrap gap-2">
				<button
					type="button"
					className={button}
					disabled={busy || (s.fix === "merge" && to.trim() === "")}
					onClick={() =>
						apply.mutate(
							{
								kind: s.kind,
								picks: [
									{ subject: s.subject, ...(s.fix === "merge" ? { to } : {}) },
								],
							},
							{ onSuccess: (result) => onDone(s, result) },
						)
					}
				>
					{t(`cleanup.apply.${s.fix}`)}
				</button>
				<button
					type="button"
					className={quietButton}
					disabled={busy}
					onClick={() =>
						dismiss.mutate(
							{ kind: s.kind, subject: s.subject },
							{ onSuccess: () => onDone(s) },
						)
					}
				>
					{t("cleanup.dismiss")}
				</button>
			</div>
		</li>
	);
}
