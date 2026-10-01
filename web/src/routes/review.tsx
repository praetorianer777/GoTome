import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { Cover } from "@/books/cover";
import {
	type ReviewBook,
	candidateCoverUrl,
	reviewQuery,
	useAcceptMatch,
	useRejectMatch,
} from "@/books/edit";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { Compare, typing } from "@/routes/book-find";

const quietButton =
	"rounded-md border border-slate-300 px-3 py-2 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

export function Review() {
	const review = useQuery(reviewQuery);
	const [at, setAt] = useState(0);
	const books = review.data?.books ?? [];
	// The list shrinks as books are settled; the place in it stays.
	const index = Math.min(at, Math.max(books.length - 1, 0));
	const current = books[index];

	const move = useRef((step: number) => setAt((i) => i + step));
	move.current = (step: number) =>
		setAt(Math.min(Math.max(index + step, 0), Math.max(books.length - 1, 0)));
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (typing(e) || e.ctrlKey || e.metaKey || e.altKey) return;
			if (e.key === "j" || e.key === "ArrowDown") {
				e.preventDefault();
				move.current(1);
			} else if (e.key === "k" || e.key === "ArrowUp") {
				e.preventDefault();
				move.current(-1);
			}
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, []);

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-semibold">{t("review.title")}</h1>
				<p className="mt-1 max-w-2xl text-slate-600 dark:text-slate-400">
					{t("review.intro")}
				</p>
			</div>
			{review.isPending && <p className="text-slate-500">{t("loading")}</p>}
			<FormError error={review.error} />
			{review.data && books.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("review.none")}</p>
			)}
			{current && (
				<>
					<div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
						<span>
							{t("review.waiting", { count: review.data?.total ?? 0 })}
						</span>
						<span className="text-slate-500">
							{t("review.position", { number: index + 1, count: books.length })}
						</span>
						<button
							type="button"
							className={quietButton}
							disabled={index === 0}
							onClick={() => move.current(-1)}
						>
							{t("review.previous")}
						</button>
						<button
							type="button"
							className={quietButton}
							disabled={index >= books.length - 1}
							onClick={() => move.current(1)}
						>
							{t("review.next")}
						</button>
					</div>
					<p className="text-sm text-slate-500 dark:text-slate-400">
						{t("review.keys")}
					</p>
					<Waiting key={current.book.id} item={current} />
				</>
			)}
		</div>
	);
}

function Waiting({ item }: { item: ReviewBook }) {
	const { book, matches } = item;
	const [picked, setPicked] = useState(0);
	const accept = useAcceptMatch(book.id);
	const reject = useRejectMatch(book.id);
	const match = matches[Math.min(picked, matches.length - 1)];

	const keys = useRef({ pick: setPicked, reject: () => {} });
	keys.current = {
		pick: setPicked,
		reject: () => match && !reject.isPending && reject.mutate(match.matchId),
	};
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (typing(e) || e.ctrlKey || e.metaKey || e.altKey) return;
			const n = Number(e.key);
			if (n >= 1 && n <= matches.length) {
				e.preventDefault();
				keys.current.pick(n - 1);
			} else if (e.key === "r") {
				e.preventDefault();
				keys.current.reject();
			}
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, [matches.length]);

	if (!match) {
		return null;
	}
	return (
		<article className="flex flex-col gap-4 rounded-lg border border-slate-200 p-4 dark:border-slate-800">
			<header className="flex gap-4">
				<div className="w-16 shrink-0">
					<Cover book={book} size="small" />
				</div>
				<div className="flex flex-col gap-1">
					<h2 className="text-xl font-semibold">
						<Link
							to="/books/$bookId"
							params={{ bookId: book.id }}
							className="hover:underline"
						>
							{book.title}
						</Link>
					</h2>
					{book.contributors.length > 0 && (
						<p>{book.contributors.map((c) => c.name).join(", ")}</p>
					)}
				</div>
			</header>
			<fieldset className="flex flex-wrap gap-2">
				<legend className="sr-only">{t("review.matches")}</legend>
				{matches.map((m, i) => (
					<button
						key={m.matchId}
						type="button"
						aria-pressed={m === match}
						onClick={() => setPicked(i)}
						className="flex items-center gap-2 rounded-md border border-slate-300 px-2 py-1 text-left text-sm hover:bg-slate-100 aria-pressed:border-brand-strong aria-pressed:bg-sky-50 dark:border-slate-600 dark:hover:bg-slate-800 dark:aria-pressed:border-brand dark:aria-pressed:bg-slate-900"
					>
						{m.coverToken && (
							<img
								src={candidateCoverUrl(m.coverToken)}
								alt=""
								className="w-8 rounded"
							/>
						)}
						<span>
							<span className="font-medium">
								{i + 1}. {m.title}
							</span>
							<span className="block text-slate-500 dark:text-slate-400">
								{[
									m.contributors.map((c) => c.name).join(", "),
									m.published,
									t("find.match", { percent: Math.round(m.score * 100) }),
								]
									.filter(Boolean)
									.join(" · ")}
							</span>
						</span>
					</button>
				))}
			</fieldset>
			<Compare
				key={match.matchId}
				book={book}
				candidate={match}
				busy={accept.isPending || reject.isPending}
				error={accept.error ?? reject.error}
				onTake={(body) => accept.mutate({ matchId: match.matchId, body })}
				keys
			>
				<button
					type="button"
					className={quietButton}
					disabled={reject.isPending}
					onClick={() => reject.mutate(match.matchId)}
				>
					{t("review.reject")}
				</button>
				<Link
					to="/books/$bookId/find"
					params={{ bookId: book.id }}
					className={quietButton}
				>
					{t("review.searchAgain")}
				</Link>
			</Compare>
		</article>
	);
}
