import { useInfiniteQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import {
	CLEANUP_KINDS,
	type ChoosableFix,
	type CleanupKind,
	type CleanupSuggestion,
	cleanupQuery,
	useApplyCleanup,
	useDismissCleanup,
} from "@/books/cleanup";
import { BookPanelLayout } from "@/books/book-panel";
import { CleanupPreviewList } from "@/books/cleanup-preview";
import { FormError } from "@/components/form";
import { PageHeader, Loading } from "@/components/ui";
import { t } from "@/i18n";
import { formatCount } from "@/lib/format";

const button =
	"rounded-md bg-brand-strong px-3 py-2 text-sm font-medium text-white hover:bg-sky-800 disabled:opacity-60 dark:bg-brand dark:text-ink dark:hover:bg-sky-300";
const quietButton =
	"rounded-md border border-slate-300 px-3 py-2 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";
const pill =
	"rounded-full border border-slate-300 px-3 py-1 hover:bg-slate-100 aria-pressed:border-brand-strong aria-pressed:bg-brand-strong aria-pressed:text-white dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:border-brand dark:aria-pressed:bg-brand dark:aria-pressed:text-ink";

/** What looks wrong in how the books are described, with a fix for each. */
export function Cleanup({
	kind,
	onKind,
	book,
	onBook,
}: {
	kind: CleanupKind;
	onKind: (kind: CleanupKind) => void;
	book?: string;
	onBook: (id: string | undefined, replace?: boolean) => void;
}) {
	const navigate = useNavigate();
	const list = useInfiniteQuery(cleanupQuery(kind));
	const apply = useApplyCleanup();
	const applyOne = useApplyCleanup();
	const dismissOne = useDismissCleanup();
	const [applied, setApplied] = useState<{ id: string; total: number }>();
	// A fix is made by a job: until it has run, the server still suggests
	// it, so the page leaves out what was applied here. A suggestion goes
	// as soon as it is clicked and comes back with the error if the server
	// refuses.
	const [gone, setGone] = useState<Set<string>>(new Set());
	const [failed, setFailed] = useState<Map<string, unknown>>(new Map());
	const [sure, setSure] = useState(false);

	const pages = list.data?.pages ?? [];
	const counts = pages[0]?.counts;
	const total = counts?.[kind] ?? 0;
	const suggestions = pages
		.flatMap((p) => p?.suggestions ?? [])
		.filter((s) => !gone.has(`${s.kind}:${s.subject}`));

	const act = (
		s: CleanupSuggestion,
		run: () => Promise<{ id: string; total: number } | void | undefined>,
	) => {
		const key = `${s.kind}:${s.subject}`;
		setGone((g) => new Set(g).add(key));
		setFailed((f) => {
			const next = new Map(f);
			next.delete(key);
			return next;
		});
		run().then(
			(result) => {
				if (result) {
					setApplied(result);
				}
			},
			(error) => {
				setGone((g) => {
					const next = new Set(g);
					next.delete(key);
					return next;
				});
				setFailed((f) => new Map(f).set(key, error));
			},
		);
	};
	const onApply = (
		s: CleanupSuggestion,
		picks: { subject: string; to?: string; fix?: ChoosableFix }[],
	) => act(s, () => applyOne.mutateAsync({ kind: s.kind, picks }));
	const onDismiss = (s: CleanupSuggestion) =>
		act(s, () => dismissOne.mutateAsync({ kind: s.kind, subject: s.subject }));

	return (
		<BookPanelLayout bookId={book} onClose={() => onBook(undefined, true)}>
		<div className="flex flex-col gap-6">
			<PageHeader title={t("cleanup.title")} intro={t("cleanup.intro")} />
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
					</Link>{" "}
					<Link to="/curation" className="underline">
						{t("cleanup.changes")}
					</Link>
				</p>
			)}
			{list.isPending && <Loading />}
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
						error={failed.get(`${s.kind}:${s.subject}`)}
						onApply={onApply}
						onDismiss={onDismiss}
						onOpen={(id) => onBook(id)}
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
		</BookPanelLayout>
	);
}

function Suggestion({
	suggestion: s,
	error,
	onApply,
	onDismiss,
	onOpen,
}: {
	suggestion: CleanupSuggestion;
	error?: unknown;
	onApply: (
		s: CleanupSuggestion,
		picks: { subject: string; to?: string; fix?: ChoosableFix }[],
	) => void;
	onDismiss: (s: CleanupSuggestion) => void;
	onOpen: (book: string) => void;
}) {
	const [keep, setKeep] = useState(s.suggested ?? "");
	const [other, setOther] = useState("");
	const [own, setOwn] = useState(false);
	const [showBooks, setShowBooks] = useState(false);
	// A clash that is to choose has no default that takes data away: only
	// what the titles and the providers point to is ticked beforehand.
	const [fix, setFix] = useState<ChoosableFix | "">(
		s.suggested === "nameIsSeries" || s.suggested === "clearSeries"
			? s.suggested
			: "",
	);
	const [showFix, setShowFix] = useState<Record<string, boolean>>({});
	const choose = s.kind === "clashes" && s.fix === "choose";
	const name = s.found[0]?.name ?? "";
	const to = own ? other : keep;

	return (
		<li className="flex flex-col gap-3 rounded-lg border border-slate-200 p-4 md:flex-row md:items-start md:gap-6 dark:border-slate-800">
			<div className="flex min-w-0 flex-1 flex-col gap-3 break-words">
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
			{choose && (
				<fieldset className="flex min-w-0 flex-col gap-3">
					<legend className="mb-1 font-medium">
						{t("cleanup.clash.some", {
							name,
							series: s.books,
							all: s.found[0]?.books ?? s.books,
						})}
					</legend>
					{(["nameIsSeries", "clearSeries"] as const).map((f) => (
						<div key={f} className="flex min-w-0 flex-col gap-2">
							<label className="flex items-start gap-2">
								<input
									type="radio"
									name={`fix-${s.subject}`}
									className="mt-1"
									checked={fix === f}
									onChange={() => setFix(f)}
								/>
								<span>
									{t(`cleanup.clash.${f}`, { name })}
									{s.suggested === f && (
										<>
											{" "}
											<span className="text-sm text-slate-500">
												({t("cleanup.clash.suggested")})
											</span>
										</>
									)}
								</span>
							</label>
							<button
								type="button"
								aria-expanded={!!showFix[f]}
								className={`${quietButton} ml-6 self-start`}
								onClick={() => setShowFix({ ...showFix, [f]: !showFix[f] })}
							>
								{showFix[f]
									? t("cleanup.preview.hide")
									: t("cleanup.preview.show")}
							</button>
							<div className="ml-6 min-w-0">
								<CleanupPreviewList
									suggestion={s}
									fix={f}
									open={!!showFix[f]}
									onOpen={onOpen}
								/>
							</div>
						</div>
					))}
				</fieldset>
			)}
			{s.kind === "clashes" && !choose && (
				<p>
					{t("cleanup.removeAuthor", { name })}{" "}
					<span className="text-sm text-slate-500">
						{t("cleanup.books", { count: s.books })}
					</span>
				</p>
			)}
			{s.fix === "retitle" && (
				<p>
					{s.book ? (
						<button
							type="button"
							onClick={() => s.book && onOpen(s.book)}
							className="text-left hover:underline"
						>
							{name}
						</button>
					) : (
						<span>{name}</span>
					)}{" "}
					<span className="text-sm text-slate-500">{t("cleanup.retitle")}</span>{" "}
					<span className="font-medium">{s.suggested}</span>
				</p>
			)}
			{s.fix === "setSeries" && (
				<div className="flex flex-col gap-2">
					<p className="font-medium">
						{t("cleanup.series", { name: s.suggested ?? "" })}{" "}
						<span className="text-sm font-normal text-slate-500">
							{t("cleanup.books", { count: s.books })}
						</span>
					</p>
					<ul className="flex flex-col gap-1">
						{s.found.map((f) => (
							<li key={f.book ?? f.name}>
								{f.book ? (
									<button
										type="button"
										onClick={() => f.book && onOpen(f.book)}
										className="text-left hover:underline"
									>
										{f.name}
									</button>
								) : (
									<span>{f.name}</span>
								)}{" "}
								{f.position !== undefined && (
									<>
										<span className="text-sm text-slate-500">
											{t("cleanup.retitle")}
										</span>{" "}
										<span className="font-medium">
											{t("cleanup.seriesPlace", {
												position: formatCount(f.position, 2),
												title: f.title ?? "",
											})}
										</span>
									</>
								)}
							</li>
						))}
					</ul>
				</div>
			)}
			{!choose && (
				<CleanupPreviewList
					suggestion={s}
					to={s.fix === "merge" && to.trim() !== "" ? to : undefined}
					open={showBooks}
					onOpen={onOpen}
				/>
			)}
			<FormError error={error} />
			</div>
			<div className="flex flex-wrap gap-2 md:shrink-0 md:justify-end">
				<button
					type="button"
					className={button}
					disabled={(s.fix === "merge" && to.trim() === "") || (choose && fix === "")}
					onClick={() =>
						onApply(s, [
							{
								subject: s.subject,
								...(s.fix === "merge" ? { to } : {}),
								...(choose && fix !== "" ? { fix } : {}),
							},
						])
					}
				>
					{t(`cleanup.apply.${choose && fix !== "" ? fix : s.fix}`)}
				</button>
				{!choose && (
					<button
						type="button"
						aria-expanded={showBooks}
						className={quietButton}
						onClick={() => setShowBooks(!showBooks)}
					>
						{showBooks ? t("cleanup.preview.hide") : t("cleanup.preview.show")}
					</button>
				)}
				<button
					type="button"
					className={quietButton}
					onClick={() => onDismiss(s)}
				>
					{t("cleanup.dismiss")}
				</button>
			</div>
		</li>
	);
}
