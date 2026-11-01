import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import type { BookSummary } from "@/books/api";
import { t } from "@/i18n";

const LINK = "hover:text-brand-strong hover:underline dark:hover:text-brand";

export function AuthorLink({ name, authorKey }: { name: string; authorKey: string }) {
	return (
		<Link to="/authors/$authorKey" params={{ authorKey }} className={LINK}>
			{name}
		</Link>
	);
}

export function SeriesLink({ name, seriesKey }: { name: string; seriesKey: string }) {
	return (
		<Link to="/series/$seriesKey" params={{ seriesKey }} className={LINK}>
			{name}
		</Link>
	);
}

/** Names, each a link, written as a list. */
function commaSeparated(links: ReactNode[]): ReactNode[] {
	return links.flatMap((link, i) => (i === 0 ? [link] : [", ", link]));
}

/** A book's authors, each a link to their page. */
export function AuthorLinks({ book }: { book: BookSummary }) {
	return commaSeparated(
		book.authors.map((name, i) => (
			<AuthorLink key={name} name={name} authorKey={book.authorKeys[i] ?? name} />
		)),
	);
}

/** The people given, each a link to their page. */
export function PeopleLinks({ people }: { people: { name: string; key?: string }[] }) {
	return commaSeparated(
		people.map((p) => <AuthorLink key={p.name} name={p.name} authorKey={p.key ?? p.name} />),
	);
}

/** A book's series and its place in it, linked to the series' page. */
export function SeriesLine({ book }: { book: BookSummary }) {
	if (!book.series) {
		return null;
	}
	return (
		<span className="line-clamp-1 block text-sm text-slate-600 dark:text-slate-400">
			{book.seriesKey ? (
				<SeriesLink name={book.series} seriesKey={book.seriesKey} />
			) : (
				book.series
			)}
			{book.seriesIndex != null && ` ${t("series.number", { index: book.seriesIndex })}`}
		</span>
	);
}

/** A sentence with one thing in it that is a link: the marker's place. */
export function Around({ text, children }: { text: string; children: ReactNode }) {
	const [before, after] = text.split(MARK);
	return (
		<>
			{before}
			{children}
			{after}
		</>
	);
}

/** What a sentence is filled in with, to find the place for Around. */
export const MARK = "\u0000";
