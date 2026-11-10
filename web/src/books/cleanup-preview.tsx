import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Cover } from "@/books/cover";
import {
	type CleanupKind,
	type CleanupPreviewBook,
	cleanupPreviewQuery,
} from "@/books/cleanup";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { formatCount } from "@/lib/format";

const SHOWN = 5;
const MOST = 200;

const quiet =
	"rounded-md border border-slate-300 px-3 py-2 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/**
 * What applying a suggestion does to each of its books, as the server would
 * do it, and what the providers are known to say about them. Asked for only
 * while open, so that the list of suggestions costs one request.
 */
export function CleanupPreviewList({
	suggestion,
	to,
	fix,
	open,
	onOpen,
}: {
	suggestion: { kind: CleanupKind; subject: string };
	to?: string;
	/** For a clash that is to choose, the fix to show. */
	fix?: string;
	open: boolean;
	onOpen: (book: string) => void;
}) {
	const [limit, setLimit] = useState(SHOWN);
	const preview = useQuery({
		...cleanupPreviewQuery(suggestion, { to, fix, limit }),
		enabled: open,
	});
	const data = preview.data;
	const rest = data ? data.total - data.books.length : 0;

	if (!open) {
		return null;
	}
	return (
		<div className="flex min-w-0 flex-col gap-2">
					<FormError error={preview.error} />
					{data && (
						<>
							<p className="text-sm font-medium">
								{t("cleanup.preview.books")}: {formatCount(data.total, 0)}
							</p>
							{data.books.length === 0 && (
								<p className="text-sm text-slate-600 dark:text-slate-400">
									{t("cleanup.preview.none")}
								</p>
							)}
							<ul className="flex min-w-0 flex-col gap-3">
								{data.books.map((b) => (
									<PreviewBook key={b.id} book={b} onOpen={onOpen} />
								))}
							</ul>
							{rest > 0 && (
								<button
									type="button"
									className={`${quiet} self-start`}
									disabled={preview.isFetching || limit >= MOST}
									onClick={() => setLimit(Math.min(MOST, data.total))}
								>
									{t("cleanup.preview.rest", { count: rest })}
								</button>
							)}
						</>
					)}
		</div>
	);
}

function PreviewBook({
	book,
	onOpen,
}: {
	book: CleanupPreviewBook;
	onOpen: (book: string) => void;
}) {
	const none = t("cleanup.preview.empty");
	return (
		<li className="grid min-w-0 grid-cols-[2.5rem_minmax(0,1fr)] gap-3">
			<Cover book={book} size="small" />
			<div className="flex min-w-0 flex-col gap-1 break-words text-sm">
				<button
					type="button"
					onClick={() => onOpen(book.id)}
					className="self-start text-left font-medium hover:underline"
				>
					{book.title}
				</button>
				{book.fields.map((f) => (
					<p key={f.name}>
						<span className="text-slate-500">
							{t(`cleanup.preview.field.${f.name}` as "cleanup.preview.field.title")}:
						</span>{" "}
						{f.locked ? (
							<>
								<span>{f.before || none}</span>{" "}
								<span className="text-slate-500">
									({t("cleanup.preview.locked")})
								</span>
							</>
						) : (
							<>
								<del className="text-slate-500">{f.before || none}</del>{" "}
								<span aria-hidden="true">→</span>
								<span className="sr-only">{t("cleanup.preview.becomes")}</span>{" "}
								<ins className="font-medium no-underline">{f.after || none}</ins>
							</>
						)}
					</p>
				))}
				{book.evidence.length > 0 ? (
					<ul className="flex flex-col gap-0.5">
						{book.evidence.map((e) => (
							<li key={`${e.provider}-${e.source}-${e.field}-${e.value}`}>
								<span
									title={
										e.source === "match"
											? t("cleanup.preview.match")
											: t("cleanup.preview.cached")
									}
									className="text-slate-500"
								>
									{t("cleanup.preview.says", { provider: providerName(e.provider) })}
								</span>{" "}
								<span>{e.value}</span>{" "}
								{e.verdict && (
									<span
										className={
											e.verdict === "agrees"
												? "text-emerald-700 dark:text-emerald-400"
												: "text-amber-700 dark:text-amber-400"
										}
									>
										<span aria-hidden="true">
											{e.verdict === "agrees" ? "✓" : "⚠"}
										</span>{" "}
										{e.verdict === "agrees"
											? t("cleanup.preview.agrees")
											: t("cleanup.preview.differs")}
									</span>
								)}
							</li>
						))}
					</ul>
				) : (
					<p className="text-slate-500">{t("cleanup.preview.noevidence")}</p>
				)}
			</div>
		</li>
	);
}

const PROVIDERS = ["openlibrary", "crossref", "hardcover"];

function providerName(name: string): string {
	return PROVIDERS.includes(name)
		? t(`settingChoice.metadata.providers.${name}` as "settingChoice.metadata.providers.hardcover")
		: name;
}
