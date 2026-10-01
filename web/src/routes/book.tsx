import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import {
	type BookDetail,
	type BookFile,
	bookQuery,
	downloadUrl,
} from "@/books/api";
import { Cover } from "@/books/cover";
import { useRouteContext } from "@tanstack/react-router";
import { can } from "@/auth/session";
import { FormError } from "@/components/form";
import { useRereadFile } from "@/jobs/api";
import { type MessageKey, t } from "@/i18n";
import { formatDuration, formatLanguage, formatSize } from "@/lib/format";

const ROLES: Record<string, MessageKey> = {
	translator: "credit.translator",
	editor: "credit.editor",
	illustrator: "credit.illustrator",
};

const back = (
	<Link
		to="/"
		className="self-start text-sm text-brand-strong underline underline-offset-4 dark:text-brand"
	>
		{t("book.back")}
	</Link>
);

export function Book({ id }: { id: string }) {
	const book = useQuery(bookQuery(id));

	if (book.error instanceof ApiError && book.error.status === 404) {
		return (
			<div className="flex flex-col gap-4">
				{back}
				<p className="text-slate-600 dark:text-slate-400">
					{t("book.notFound")}
				</p>
			</div>
		);
	}
	if (!book.data) {
		return (
			<div className="flex flex-col gap-4">
				{back}
				{book.isPending && <p className="text-slate-500">{t("loading")}</p>}
				<FormError error={book.error} />
			</div>
		);
	}
	return <BookPage book={book.data} />;
}

function names(book: BookDetail, role: string): string {
	return book.contributors
		.filter((c) => c.role === role)
		.map((c) => c.name)
		.join(", ");
}

function BookPage({ book }: { book: BookDetail }) {
	const { user } = useRouteContext({ from: "/app" });
	const authors = names(book, "author");
	const narrators = names(book, "narrator");
	const others = book.contributors.filter((c) => ROLES[c.role]);
	const details: [MessageKey, string][] = [];
	if (book.publisher) details.push(["book.publisher", book.publisher]);
	if (book.published) details.push(["book.published", book.published]);
	if (book.language)
		details.push(["book.language", formatLanguage(book.language)]);
	if (book.pageCount) details.push(["book.pages", String(book.pageCount)]);
	if (book.durationMs)
		details.push(["book.duration", formatDuration(book.durationMs)]);
	for (const c of others) {
		const role = ROLES[c.role];
		if (role) details.push([role, c.name]);
	}
	if (book.identifiers.length > 0) {
		details.push([
			"book.identifiers",
			book.identifiers
				.map((i) => `${i.type.toUpperCase()} ${i.value}`)
				.join(", "),
		]);
	}

	return (
		<article className="flex flex-col gap-6">
			{back}
			<div className="grid gap-6 sm:grid-cols-[minmax(0,14rem)_1fr] sm:gap-8">
				<div className="mx-auto w-40 sm:w-full">
					<Cover book={book} size="large" />
				</div>
				<div className="flex flex-col gap-4">
					<header className="flex flex-col gap-1">
						<h1 className="text-3xl font-semibold leading-tight">
							{book.title}
						</h1>
						{book.subtitle && (
							<p className="text-lg text-slate-600 dark:text-slate-400">
								{book.subtitle}
							</p>
						)}
						{authors && (
							<p className="text-lg">{t("book.by", { names: authors })}</p>
						)}
						{narrators && (
							<p className="text-slate-600 dark:text-slate-400">
								{t("book.readBy", { names: narrators })}
							</p>
						)}
						{book.series && (
							<p className="text-slate-600 dark:text-slate-400">
								{book.seriesIndex != null
									? t("book.seriesPosition", {
											index: book.seriesIndex,
											series: book.series,
										})
									: t("book.series", { series: book.series })}
							</p>
						)}
						{can(user, "metadata:edit") && (
							<div className="mt-2 flex flex-wrap gap-2">
								<Link
									to="/books/$bookId/edit"
									params={{ bookId: book.id }}
									className="rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
								>
									{t("book.edit")}
								</Link>
								<Link
									to="/books/$bookId/find"
									params={{ bookId: book.id }}
									className="rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
								>
									{t("book.find")}
								</Link>
							</div>
						)}
					</header>

					{book.description && (
						<section aria-label={t("book.description")}>
							<p className="max-w-prose whitespace-pre-line leading-relaxed">
								{book.description}
							</p>
						</section>
					)}

					{details.length > 0 && (
						<section aria-labelledby="book-details">
							<h2 id="book-details" className="sr-only">
								{t("book.details")}
							</h2>
							<dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
								{details.map(([label, value]) => (
									<div key={`${label}${value}`} className="contents">
										<dt className="text-slate-500 dark:text-slate-400">
											{t(label)}
										</dt>
										<dd className="break-words">{value}</dd>
									</div>
								))}
							</dl>
						</section>
					)}

					{book.tags.length > 0 && (
						<section
							aria-labelledby="book-tags"
							className="flex flex-col gap-2"
						>
							<h2 id="book-tags" className="text-sm font-medium">
								{t("book.tags")}
							</h2>
							<ul className="flex flex-wrap gap-2">
								{book.tags.map((tag) => (
									<li
										key={tag}
										className="rounded-full bg-slate-100 px-3 py-0.5 text-sm dark:bg-slate-800"
									>
										{tag}
									</li>
								))}
							</ul>
						</section>
					)}

					<Files files={book.files} />
				</div>
			</div>
		</article>
	);
}

function fileNotes(file: BookFile): string[] {
	const notes: string[] = [];
	if (file.part != null)
		notes.push(t("book.file.part", { number: file.part + 1 }));
	if (file.durationMs) notes.push(formatDuration(file.durationMs));
	if (file.drm) notes.push(t("book.file.drm"));
	if (file.hasText === false && file.kind === "ebook")
		notes.push(t("book.file.noText"));
	if (file.missing) notes.push(t("book.file.missing"));
	if (file.extractState === "pending") notes.push(t("book.file.pending"));
	return notes;
}

function Files({ files }: { files: BookFile[] }) {
	const { user } = useRouteContext({ from: "/app" });
	const reread = useRereadFile();
	const canReread = can(user, "index:rebuild");
	if (files.length === 0) {
		return null;
	}
	return (
		<section aria-labelledby="book-files" className="flex flex-col gap-2">
			<h2 id="book-files" className="text-lg font-medium">
				{t("book.files")}
			</h2>
			<ul className="divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
				{files.map((file) => (
					<li
						key={file.id}
						className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2 text-sm"
					>
						<span className="w-12 font-semibold uppercase">{file.format}</span>
						<span className="min-w-0 flex-1 break-all">
							{file.name}
							{fileNotes(file).length > 0 && (
								<span className="block text-slate-500 dark:text-slate-400">
									{fileNotes(file).join(" · ")}
								</span>
							)}
							{file.extractState === "failed" && (
								<span className="block text-red-700 dark:text-red-400">
									{file.extractError
										? t("book.file.failedWhy", { error: file.extractError })
										: t("book.file.failed")}
								</span>
							)}
						</span>
						{canReread && file.extractState === "failed" && !file.missing && (
							<button
								type="button"
								disabled={reread.isPending}
								onClick={() => reread.mutate(file.id)}
								aria-label={t("book.file.rereadNamed", { name: file.name })}
								className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800"
							>
								{t("book.file.reread")}
							</button>
						)}
						<span className="text-slate-600 dark:text-slate-400">
							{formatSize(file.size)}
						</span>
						{!file.missing && (
							<a
								href={downloadUrl(file)}
								download
								aria-label={t("book.file.downloadNamed", { name: file.name })}
								className="rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
							>
								{t("book.file.download")}
							</a>
						)}
					</li>
				))}
			</ul>
			<FormError error={reread.error} />
		</section>
	);
}
