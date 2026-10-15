import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import {
	type BookDetail,
	type BookFile,
	bookQuery,
	downloadUrl,
} from "@/books/api";
import { Cover } from "@/books/cover";
import { CollectionPicker } from "@/books/collection-picker";
import { EReaderFindings } from "@/books/ereader";
import { SendToDevice } from "@/books/deliveries";
import { ReadingControls } from "@/books/reading-controls";
import { SimilarBooks } from "@/books/similar";
import { FollowButtons } from "@/releases/follow";
import { readable } from "@/routes/read";
import { usePlayer } from "@/player/player";
import { useRouteContext } from "@tanstack/react-router";
import { can } from "@/auth/session";
import { FormError } from "@/components/form";
import { button, Skeleton } from "@/components/ui";
import { useRereadFile } from "@/jobs/api";
import { RereadText } from "@/jobs/search-controls";
import { librariesQuery } from "@/libraries/api";
import { useTrashFile } from "@/trash/api";
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
		className="self-start text-sm text-brand-strong hover:underline underline-offset-4 dark:text-brand"
	>
		<span aria-hidden="true">← </span>
		{t("book.back")}
	</Link>
);

export function Book({ id }: { id: string }) {
	const book = useQuery(bookQuery(id));
	const navigate = useNavigate();
	const survivor = book.data?.id;
	// A merged book's ID leads to the book it was merged into, whose own
	// address the page then takes.
	useEffect(() => {
		if (survivor && survivor !== id) {
			navigate({ to: "/books/$bookId", params: { bookId: survivor }, replace: true });
		}
	}, [survivor, id, navigate]);

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
				{book.isPending && (
					<div className="grid gap-6 sm:grid-cols-[14rem_1fr] sm:gap-10">
						<Skeleton className="mx-auto aspect-[2/3] w-40 sm:w-full" />
						<div className="flex flex-col gap-3">
							<Skeleton className="h-9 w-2/3" />
							<Skeleton className="h-5 w-1/3" />
							<span className="sr-only">{t("loading")}</span>
						</div>
					</div>
				)}
				<FormError error={book.error} />
			</div>
		);
	}
	return <BookPage book={book.data} />;
}

/** Loads the book into the player and goes to its page. */
function ListenButton({ bookId }: { bookId: string }) {
	const player = usePlayer();
	return (
		<Link
			to="/books/$bookId/listen"
			params={{ bookId }}
			onClick={() => player.open(bookId)}
			className={button("primary", "md")}
		>
			{t("book.listen")}
		</Link>
	);
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

	const readableFile = book.files.find((f) => !f.missing && readable(f.format));
	const listenable = book.files.some((f) => f.kind === "audio" && !f.missing);
	const personal = can(user, "personal:manage");

	return (
		<article className="flex flex-col gap-8">
			{back}
			<div className="grid gap-6 sm:grid-cols-[14rem_1fr] sm:gap-10">
				<div className="mx-auto w-44 sm:w-full">
					<Cover
						book={book}
						size="large"
						named
						className={book.reading.status === "reading" ? "shadow-candle" : ""}
					/>
				</div>
				<div className="flex min-w-0 flex-col gap-5">
					<header className="flex flex-col gap-1.5">
						{book.series && (
							<p className="text-sm font-medium tracking-wide text-brand-strong dark:text-brand">
								{book.seriesIndex != null
									? t("book.seriesPosition", {
											index: book.seriesIndex,
											series: book.series,
										})
									: t("book.series", { series: book.series })}
							</p>
						)}
						<h1 className="text-3xl leading-tight font-semibold sm:text-4xl">
							{book.title}
						</h1>
						{book.subtitle && (
							<p className="font-serif text-xl text-slate-600 dark:text-slate-400">
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
					</header>

					{(readableFile || listenable || can(user, "metadata:edit")) && (
						<div className="flex flex-wrap items-center gap-2">
							{readableFile && (
								<Link
									to="/books/$bookId/read/$fileId"
									params={{ bookId: book.id, fileId: readableFile.id }}
									className={button("primary", "md")}
								>
									{t("book.file.read")}
								</Link>
							)}
							{listenable && <ListenButton bookId={book.id} />}
							{can(user, "metadata:edit") && (
								<>
									<Link
										to="/books/$bookId/edit"
										params={{ bookId: book.id }}
										className={button("secondary", "md")}
									>
										{t("book.edit")}
									</Link>
									<Link
										to="/books/$bookId/find"
										params={{ bookId: book.id }}
										className={button("secondary", "md")}
									>
										{t("book.find")}
									</Link>
								</>
							)}
						</div>
					)}
					{personal && <FollowButtons book={book} />}

					{book.placeholder && (
						<p className="rounded-md border border-sky-200 bg-sky-50 px-3 py-2 text-sm dark:border-sky-900 dark:bg-sky-950">
							{t("book.placeholder")}
						</p>
					)}

					{personal && <ReadingControls book={book} />}

					{book.description && (
						<section aria-label={t("book.description")}>
							<p className="max-w-prose leading-relaxed whitespace-pre-line">
								{book.description}
							</p>
						</section>
					)}

					{book.tags.length > 0 && (
						<section
							aria-labelledby="book-tags"
							className="flex flex-col gap-2"
						>
							<h2 id="book-tags" className="sr-only">
								{t("book.tags")}
							</h2>
							<ul className="flex flex-wrap gap-2">
								{book.tags.map((tag) => (
									<li
										key={tag}
										className="rounded-full border border-slate-200 bg-white px-3 py-0.5 text-sm dark:border-slate-700 dark:bg-slate-900"
									>
										{tag}
									</li>
								))}
							</ul>
						</section>
					)}
				</div>
			</div>

			<div className="grid gap-8 lg:grid-cols-[1fr_18rem]">
				<div className="flex min-w-0 flex-col gap-6">
					<Files bookId={book.id} libraryId={book.libraryId} files={book.files} />
					{personal && <CollectionPicker bookId={book.id} />}
					{personal &&
						book.files.some((f) => f.kind === "ebook" && !f.missing) && (
							<SendToDevice bookId={book.id} />
						)}
				</div>
				{details.length > 0 && (
					<section
						aria-labelledby="book-details"
						className="self-start rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-950"
					>
						<h2 id="book-details" className="mb-3 text-lg font-medium">
							{t("book.details")}
						</h2>
						<dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
							{details.map(([label, value]) => (
								<div key={`${label}${value}`} className="contents">
									<dt className="text-slate-600 dark:text-slate-400">
										{t(label)}
									</dt>
									<dd className="break-words">{value}</dd>
								</div>
							))}
						</dl>
					</section>
				)}
			</div>

			<SimilarBooks bookId={book.id} />
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

function Files({
	bookId,
	libraryId,
	files,
}: {
	bookId: string;
	libraryId: string;
	files: BookFile[];
}) {
	const { user } = useRouteContext({ from: "/app" });
	const reread = useRereadFile();
	const canReread = can(user, "index:rebuild");
	const trash = useTrashFile();
	const libraries = useQuery(librariesQuery);
	// Files go to the trash only in a library GOtome writes to.
	const canTrash =
		can(user, "metadata:edit") &&
		Boolean(libraries.data?.find((l) => l.id === libraryId)?.writable);
	if (files.length === 0) {
		return null;
	}
	return (
		<section aria-labelledby="book-files" className="flex flex-col gap-2">
			<h2 id="book-files" className="text-lg font-medium">
				{t("book.files")}
			</h2>
			<ul className="divide-y divide-slate-200 rounded-lg border border-slate-200 bg-white dark:divide-slate-800 dark:border-slate-800 dark:bg-slate-950">
				{files.map((file) => (
					<li
						key={file.id}
						className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2 text-sm"
					>
						<span className="w-14 rounded bg-slate-100 py-0.5 text-center text-xs font-semibold tracking-wide uppercase dark:bg-slate-800">
							{file.format}
						</span>
						<span className="min-w-[10rem] flex-1 break-words">
							{file.name}
							{fileNotes(file).length > 0 && (
								<span className="block text-slate-500 dark:text-slate-400">
									{fileNotes(file).join(" · ")}
								</span>
							)}
							<EReaderFindings file={file} canFix={canTrash} />
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
								className={button()}
							>
								{t("book.file.reread")}
							</button>
						)}
						<span className="text-slate-600 dark:text-slate-400">
							{formatSize(file.size)}
						</span>
						{!file.missing && readable(file.format) && (
							<Link
								to="/books/$bookId/read/$fileId"
								params={{ bookId, fileId: file.id }}
								aria-label={t("book.file.readNamed", { name: file.name })}
								className={button()}
							>
								{t("book.file.read")}
							</Link>
						)}
						{canTrash && !file.missing && (
							<button
								type="button"
								disabled={trash.isPending}
								onClick={() => trash.mutate(file.id)}
								aria-label={t("book.file.trashNamed", { name: file.name })}
								className={button("danger")}
							>
								{t("book.file.trash")}
							</button>
						)}
						{!file.missing && (
							<a
								href={downloadUrl(file)}
								download
								aria-label={t("book.file.downloadNamed", { name: file.name })}
								className={button()}
							>
								{t("book.file.download")}
							</a>
						)}
					</li>
				))}
			</ul>
			<FormError error={reread.error ?? trash.error} />
			{canReread && files.some((f) => f.hasText) && (
				<RereadText target={{ book: bookId }} label={t("searchIndex.rereadBook")} />
			)}
		</section>
	);
}
