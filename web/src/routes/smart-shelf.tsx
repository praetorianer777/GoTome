import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { ApiError } from "@/api/client";
import { Cover } from "@/books/cover";
import type { Visibility } from "@/books/collections";
import { RuleBuilder } from "@/books/rule-builder";
import { groupOf, newGroup, newRule, treeOf } from "@/books/rules";
import {
	type SmartShelf,
	bookCountQuery,
	shelfProblem,
	smartShelfBooksQuery,
	smartShelfQuery,
	useCreateSmartShelf,
	useDeleteSmartShelf,
	useUpdateSmartShelf,
} from "@/books/smart-shelves";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { VisibilityPicker, button, control } from "@/routes/collections";

/** A smart shelf: what matches its rules for the person looking, now. */
export function SmartShelfPage({ shelfId }: { shelfId: string }) {
	const shelf = useQuery(smartShelfQuery(shelfId));
	const books = useInfiniteQuery(smartShelfBooksQuery(shelfId));
	const [editing, setEditing] = useState(false);
	const s = shelf.data;
	if (shelf.error instanceof ApiError && shelf.error.status === 404) {
		return (
			<p className="text-slate-600 dark:text-slate-400">
				{t("smart.notFound")}
			</p>
		);
	}
	if (!s) {
		return (
			<>
				{shelf.isPending && <p className="text-slate-500">{t("loading")}</p>}
				<FormError error={shelf.error} />
			</>
		);
	}
	const shown = books.data?.pages.flatMap((p) => p.books) ?? [];
	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-wrap items-baseline gap-x-4 gap-y-2">
				<h1 className="text-2xl font-semibold">{s.name}</h1>
				<span className="text-sm text-slate-600 dark:text-slate-400">
					{[
						t("collections.count", { count: s.books }),
						s.mine
							? t(`collections.visibility.${s.visibility}`)
							: t("collections.by", { name: s.ownerName }),
					].join(" · ")}
				</span>
				{s.mine && !editing && (
					<button
						type="button"
						className={button}
						onClick={() => setEditing(true)}
					>
						{t("smart.edit")}
					</button>
				)}
			</div>
			{editing && <SmartShelfForm shelf={s} onDone={() => setEditing(false)} />}
			<FormError error={books.error} />
			{books.isSuccess && shown.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("smart.empty")}</p>
			)}
			<ul
				aria-label={t("smart.books")}
				className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6"
			>
				{shown.map((book) => (
					<li key={book.id}>
						<Link
							to="/books/$bookId"
							params={{ bookId: book.id }}
							className="flex flex-col gap-2"
						>
							<Cover book={book} size="small" />
							<span className="flex flex-col">
								<span className="line-clamp-2 font-medium leading-snug">
									{book.title}
								</span>
								{book.authors.length > 0 && (
									<span className="line-clamp-1 text-sm text-slate-600 dark:text-slate-400">
										{book.authors.join(", ")}
									</span>
								)}
							</span>
						</Link>
					</li>
				))}
			</ul>
			{books.hasNextPage && (
				<button
					type="button"
					className={`${button} self-center`}
					disabled={books.isFetchingNextPage}
					onClick={() => books.fetchNextPage()}
				>
					{t("library.more")}
				</button>
			)}
		</div>
	);
}

/** Makes a smart shelf, starting from one empty rule. */
export function NewSmartShelf() {
	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-semibold">{t("smart.new")}</h1>
			<SmartShelfForm />
		</div>
	);
}

/** The name, visibility and rules of a shelf, with how many books they match as they are typed. */
function SmartShelfForm({
	shelf,
	onDone,
}: {
	shelf?: SmartShelf;
	onDone?: () => void;
}) {
	const [name, setName] = useState(shelf?.name ?? "");
	const [visibility, setVisibility] = useState<Visibility>(
		shelf?.visibility ?? "private",
	);
	const [rules, setRules] = useState(() =>
		shelf ? groupOf(shelf.filter) : newGroup("all", [newRule()]),
	);
	const [deleting, setDeleting] = useState(false);
	const tree = treeOf(rules);
	const [counted, setCounted] = useState(tree);
	useEffect(() => {
		const wait = setTimeout(() => setCounted(tree), 300);
		return () => clearTimeout(wait);
	}, [tree]);
	const count = useQuery(bookCountQuery(counted));
	const create = useCreateSmartShelf();
	const update = useUpdateSmartShelf(shelf?.id ?? "");
	const del = useDeleteSmartShelf(shelf?.id ?? "");
	const save = shelf ? update : create;
	const navigate = useNavigate();
	return (
		<form
			aria-label={shelf ? t("smart.edit") : t("smart.new")}
			className="flex flex-col gap-4 text-sm"
			onSubmit={(e) => {
				e.preventDefault();
				save.mutate(
					{ name, visibility, filter: tree },
					{
						onSuccess: (saved) => {
							if (shelf) {
								onDone?.();
							} else if (saved) {
								navigate({
									to: "/smart-shelves/$shelfId",
									params: { shelfId: saved.id },
								});
							}
						},
					},
				);
			}}
		>
			<div className="flex flex-wrap items-end gap-3">
				<label className="flex flex-col gap-1">
					<span>{t("collections.name")}</span>
					<input
						required
						value={name}
						onChange={(e) => setName(e.target.value)}
						className={control}
					/>
				</label>
				<VisibilityPicker value={visibility} onChange={setVisibility} />
			</div>
			<RuleBuilder value={rules} onChange={setRules} />
			<p role="status" aria-live="polite">
				{count.error
					? shelfProblem(count.error)
					: count.data !== undefined &&
						t("smart.matches", { count: count.data })}
			</p>
			{visibility === "shared" && (
				<p className="text-slate-600 dark:text-slate-400">
					{t("smart.sharedNote")}
				</p>
			)}
			{save.error && (
				<p role="alert" className="text-red-700 dark:text-red-400">
					{shelfProblem(save.error)}
				</p>
			)}
			<FormError error={del.error} />
			<div className="flex flex-wrap gap-2">
				<button type="submit" className={button} disabled={save.isPending}>
					{shelf ? t("collections.save") : t("collections.create")}
				</button>
				{onDone && (
					<button type="button" className={button} onClick={onDone}>
						{t("collections.cancel")}
					</button>
				)}
				<span className="grow" />
				{shelf &&
					(deleting ? (
						<>
							<span className="self-center">{t("smart.deleteSure")}</span>
							<button
								type="button"
								className={`${button} text-red-700 dark:text-red-400`}
								disabled={del.isPending}
								onClick={() =>
									del.mutate(undefined, {
										onSuccess: () => navigate({ to: "/collections" }),
									})
								}
							>
								{t("collections.deleteYes")}
							</button>
						</>
					) : (
						<button
							type="button"
							className={button}
							onClick={() => setDeleting(true)}
						>
							{t("smart.delete")}
						</button>
					))}
			</div>
		</form>
	);
}
