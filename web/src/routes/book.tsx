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
import { ContinueOffers } from "@/books/continue-offer";
import { EReaderFindings, useConvertFile } from "@/books/ereader";
import { SendToDevice } from "@/books/deliveries";
import { SendToKindle } from "@/books/kindle";
import { JoinedNotice, JoinToLibraryBook } from "@/books/join-placeholder";
import { ReadingControls } from "@/books/reading-controls";
import { SimilarBooks } from "@/books/similar";
import { Around, MARK, PeopleLinks, SeriesLink } from "@/books/name-links";
import { FollowButtons } from "@/releases/follow";
import { KeepAudioButton, KeepFileButton } from "@/offline/keep-button";
import { readable } from "@/routes/read";
import { usePlayer } from "@/player/player";
import { QueueButtons } from "@/player/up-next";
import { useRouteContext } from "@tanstack/react-router";
import { can } from "@/auth/session";
import { useUndoJoin } from "@/duplicates/api";
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
		className="inline-flex min-h-6 items-center self-start text-sm text-brand-strong hover:underline underline-offset-4 dark:text-brand"
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
	const details: [MessageKey, string | string[]][] = [];
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
			book.identifiers.map((i) => `${i.type.toUpperCase()} ${i.value}`),
		]);
	}

	const readableFile = book.files.find((f) => !f.missing && readable(f.format));
	const listenable = book.files.some((f) => f.kind === "audio" && !f.missing);
	const personal = can(user, "personal:manage");

	return (
		// Names, file names and identifiers may be words longer than a line:
		// "anywhere" lets grid and flex items shrink below them, which
		// "break-word" does not.
		<article className="flex flex-col gap-8 wrap-anywhere">
			{back}
			<div className="grid gap-6 sm:grid-cols-[14rem_minmax(0,1fr)] sm:gap-10">
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
								<Around
									text={
										book.seriesIndex != null
											? t("book.seriesPosition", {
													index: book.seriesIndex,
													series: MARK,
												})
											: t("book.series", { series: MARK })
									}
								>
									<SeriesLink name={book.series} seriesKey={book.seriesKey ?? book.series} />
								</Around>
							</p>
						)}
						<h1 className="text-3xl leading-tight font-semibold sm:text-4xl">
							{book.title}
						</h1>
						{book.subtitle && (
							<p className="font-serif text-lg text-slate-600 dark:text-slate-400">
								{book.subtitle}
							</p>
						)}
						{authors && (
							<p className="text-lg">
								<Around text={t("book.by", { names: MARK })}>
									<PeopleLinks people={book.contributors.filter((c) => c.role === "author")} />
								</Around>
							</p>
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
							{personal && (readableFile || listenable) && <ContinueOffers bookId={book.id} />}
							{listenable && personal && (
								<QueueButtons bookId={book.id} className={button("secondary", "md")} />
							)}
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

					{personal && book.placeholder && <JoinToLibraryBook placeholder={book.id} />}
					{personal && <JoinedNotice book={book.id} />}

					{personal && <ReadingControls book={book} />}

					{book.description && (
						<section aria-label={t("book.description")}>
							<p className="max-w-prose leading-normal whitespace-pre-line">
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

			<div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]">
				<div className="flex min-w-0 flex-col gap-6">
					<Files book={book} />
					{personal && <CollectionPicker bookId={book.id} />}
					{personal &&
						book.files.some((f) => f.kind === "ebook" && !f.missing) && (
							<SendToDevice bookId={book.id} />
						)}
					{personal &&
						book.files.some(
							(f) => f.format === "epub" && !f.missing,
						) && <SendToKindle bookId={book.id} />}
				</div>
				{details.length > 0 && (
					<section
						aria-labelledby="book-details"
						className="self-start rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-950"
					>
						<h2 id="book-details" className="mb-3 text-lg font-medium">
							{t("book.details")}
						</h2>
						<dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-sm">
							{details.map(([label, value]) => (
								<div key={`${label}${value}`} className="contents">
									<dt className="text-slate-600 dark:text-slate-400">
										{t(label)}
									</dt>
									<dd className="min-w-0">
										{Array.isArray(value)
											? value.map((v) => (
													<span key={v} className="block">
														{v}
													</span>
												))
											: value}
									</dd>
								</div>
							))}
						</dl>
					</section>
				)}
			</div>

			{book.recordings.length > 0 && (
				<section aria-labelledby="recordings-heading" className="flex flex-col gap-2">
					<h2 id="recordings-heading" className="font-serif text-lg">
						{t("book.recordings")}
					</h2>
					<ul className="flex flex-col gap-1">
						{book.recordings.map((r) => (
							<li key={r.id}>
								<Link
									to="/books/$bookId"
									params={{ bookId: r.id }}
									className="text-brand-strong hover:underline underline-offset-4 dark:text-brand"
								>
									{r.title}
								</Link>
								{r.narrators.length > 0 && (
									<span className="text-slate-600 dark:text-slate-400">
										{" · "}
										{t("book.readBy", { names: r.narrators.join(", ") })}
									</span>
								)}
							</li>
						))}
					</ul>
				</section>
			)}

			{book.forms.length > 0 && (
				<section aria-labelledby="forms-heading" className="flex flex-col gap-2">
					<h2 id="forms-heading" className="font-serif text-lg">
						{t("book.forms")}
					</h2>
					<ul className="flex flex-col gap-1">
						{book.forms.map((f) => (
							<li key={f.id}>
								<Link
									to="/books/$bookId"
									params={{ bookId: f.id }}
									className="text-brand-strong hover:underline underline-offset-4 dark:text-brand"
								>
									{f.title}
								</Link>
								<span className="text-slate-600 dark:text-slate-400">
									{" · "}
									{f.kinds
										.map((k) => t(k === "audio" ? "book.form.audio" : "book.form.ebook"))
										.join(", ")}
								</span>
							</li>
						))}
					</ul>
				</section>
			)}

			{book.joins.length > 0 && (
				<Joins joins={book.joins} canSplit={can(user, "metadata:edit")} />
			)}

			<SimilarBooks bookId={book.id} />
		</article>
	);
}

function Joins({ joins, canSplit }: { joins: BookDetail["joins"]; canSplit: boolean }) {
	const undo = useUndoJoin();
	return (
		<section aria-labelledby="joins-heading" className="flex flex-col gap-2">
			<h2 id="joins-heading" className="font-serif text-lg">
				{t("book.joins")}
			</h2>
			<ul className="flex flex-col gap-2">
				{joins.map((j) => (
					<li key={j.id} className="flex flex-wrap items-center gap-3">
						<span>{t("book.joined", { title: j.title })}</span>
						{canSplit && (
							<button
								type="button"
								disabled={undo.isPending}
								onClick={() => undo.mutate(j.id)}
								className="rounded-md border border-slate-300 px-2 py-1 text-sm hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
							>
								{t("book.split")}
							</button>
						)}
					</li>
				))}
			</ul>
			<FormError error={undo.error} />
		</section>
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

/** The formats GOtome writes an EPUB from. */
const KINDLE_FORMATS = ["mobi", "azw", "azw3"];

function Files({ book }: { book: BookDetail }) {
	const { id: bookId, libraryId, files } = book;
	const { user } = useRouteContext({ from: "/app" });
	const reread = useRereadFile();
	const canReread = can(user, "index:rebuild");
	const trash = useTrashFile();
	const convert = useConvertFile();
	const libraries = useQuery(librariesQuery);
	// Files go to the trash only in a library GOtome writes to.
	const canTrash =
		can(user, "metadata:edit") &&
		Boolean(libraries.data?.find((l) => l.id === libraryId)?.writable);
	const readOnly =
		can(user, "metadata:edit") &&
		libraries.data?.find((l) => l.id === libraryId)?.writable === false;
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
						className="flex flex-wrap items-center gap-x-4 gap-y-2 px-3 py-2 text-sm"
					>
						<span className="w-14 rounded bg-slate-100 py-0.5 text-center text-xs font-semibold tracking-wide uppercase dark:bg-slate-800">
							{file.format}
						</span>
						<span className="min-w-[10rem] flex-1">
							{file.name}
							{fileNotes(file).length > 0 && (
								<span className="block text-slate-500 dark:text-slate-400">
									{fileNotes(file).join(" · ")}
								</span>
							)}
							<EReaderFindings file={file} canFix={canTrash} readOnly={readOnly} />
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
						{canTrash && !file.missing && !file.drm && KINDLE_FORMATS.includes(file.format) && (
							<button
								type="button"
								disabled={convert.isPending}
								onClick={() => convert.mutate(file.id)}
								aria-label={t("book.file.convertNamed", { name: file.name })}
								className={button()}
							>
								{t("book.file.convert")}
							</button>
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
						<KeepFileButton book={book} file={file} />
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
			<div className="flex">
				<KeepAudioButton book={book} />
			</div>
			<FormError error={reread.error ?? trash.error ?? convert.error} />
			{canReread && files.some((f) => f.hasText) && (
				<RereadText target={{ book: bookId }} label={t("searchIndex.rereadBook")} />
			)}
		</section>
	);
}
