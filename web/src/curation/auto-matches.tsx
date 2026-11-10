import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useAcceptMatch } from "@/books/edit";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { Compare } from "@/routes/book-find";
import {
	type AutoMatchGroup,
	autoMatchesQuery,
	useStartAutoMatches,
} from "./auto";
import { AutoRun } from "./auto-run";

const quietButton =
	"rounded-md border border-slate-300 px-3 py-2 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** What a run over the matches would do by itself, and the rest in groups. */
export function AutoMatches({ onOpen }: { onOpen: (book: string) => void }) {
	const plan = useQuery(autoMatchesQuery);
	const start = useStartAutoMatches();
	const data = plan.data;
	if (!data) {
		return <FormError error={plan.error} />;
	}
	const takes = data.accept + data.dismiss + data.settle;
	return (
		<section
			aria-label={t("auto.matches.title")}
			className="flex flex-col gap-4 rounded-lg border border-slate-200 p-4 dark:border-slate-800"
		>
			<div className="flex max-w-2xl flex-col gap-2">
				<h2 className="font-semibold">{t("auto.matches.title")}</h2>
				<p className="text-sm">{t("auto.matches.rule")}</p>
				<p>
					{t(data.capped ? "auto.matches.countCapped" : "auto.matches.count", {
						accept: data.accept,
						settle: data.settle,
						dismiss: data.dismiss,
						books: data.books,
					})}
				</p>
				<AutoRun
					count={data.accept + data.dismiss}
					name={t("auto.matches.name")}
					disabled={takes === 0}
					pending={start.isPending}
					error={start.error}
					start={(dryRun) => start.mutateAsync({ dryRun })}
				/>
			</div>
			{data.groups.length > 0 && (
				<div className="flex flex-col gap-3">
					<h3 className="font-semibold">{t("auto.groups.title")}</h3>
					<p className="max-w-2xl text-sm">{t("auto.groups.intro")}</p>
					<ul className="flex flex-col gap-3">
						{data.groups.map((g) => (
							<Group key={`${g.provider}:${g.band}`} group={g} onOpen={onOpen} />
						))}
					</ul>
				</div>
			)}
		</section>
	);
}

export function bandName(band: number): string {
	return t("auto.groups.band", { from: band * 10, to: band === 9 ? 100 : band * 10 + 9 });
}

function Group({
	group: g,
	onOpen,
}: {
	group: AutoMatchGroup;
	onOpen: (book: string) => void;
}) {
	const [shown, setShown] = useState(false);
	const start = useStartAutoMatches();
	const name = `${g.provider}, ${bandName(g.band)}`;
	return (
		<li
			aria-label={name}
			className="flex flex-col gap-3 rounded-lg border border-slate-200 p-3 dark:border-slate-800"
		>
			<div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
				<span className="font-medium">{name}</span>
				<span className="text-sm text-slate-600 dark:text-slate-400">
					{t("auto.groups.matches", { count: g.matches })}
				</span>
			</div>
			<button
				type="button"
				aria-expanded={shown}
				className={`${quietButton} self-start`}
				onClick={() => setShown(!shown)}
			>
				{shown ? t("auto.groups.hide") : t("auto.groups.show")}
			</button>
			{shown && (
				<ul aria-label={t("auto.groups.sample")} className="flex flex-col gap-3">
					{g.sample.map((item) => (
						<SampleRow key={item.matches[0]?.matchId} item={item} onOpen={onOpen} />
					))}
				</ul>
			)}
			<AutoRun
				count={g.matches}
				name={name}
				runLabel={t("auto.groups.accept")}
				runDisabled={!shown}
				pending={start.isPending}
				error={start.error}
				start={(dryRun) => start.mutateAsync({ dryRun, provider: g.provider, band: g.band })}
			/>
			{!shown && (
				<p className="text-sm text-slate-600 dark:text-slate-400">
					{t("auto.groups.lookFirst")}
				</p>
			)}
		</li>
	);
}

function SampleRow({
	item,
	onOpen,
}: {
	item: AutoMatchGroup["sample"][number];
	onOpen: (book: string) => void;
}) {
	const [compare, setCompare] = useState(false);
	const accept = useAcceptMatch(item.book.id);
	const match = item.matches[0];
	if (!match) {
		return null;
	}
	return (
		<li className="flex flex-col gap-2 text-sm">
			<div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
				<button
					type="button"
					onClick={() => onOpen(item.book.id)}
					className="text-left font-medium hover:underline"
				>
					{item.book.title}
				</button>
				<span className="text-slate-600 dark:text-slate-400">
					{[
						match.title,
						match.contributors.map((c) => c.name).join(", "),
						match.published,
						t("find.match", { percent: Math.round(match.score * 100) }),
					]
						.filter(Boolean)
						.join(" · ")}
				</span>
				<button
					type="button"
					aria-expanded={compare}
					className={quietButton}
					onClick={() => setCompare(!compare)}
				>
					{compare ? t("auto.groups.hideCompare") : t("auto.groups.compare")}
				</button>
			</div>
			{compare && (
				<Compare
					book={item.book}
					candidate={match}
					busy={accept.isPending}
					error={accept.error}
					onTake={(body) => accept.mutate({ matchId: match.matchId, body })}
				/>
			)}
		</li>
	);
}
