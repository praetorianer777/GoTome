import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { ApiError } from "@/api/client";
import {
	type CollectionDetail,
	type Visibility,
	collectionQuery,
	moved,
	useDeleteCollection,
	useRemoveFromCollection,
	useReorderCollection,
	useUpdateCollection,
} from "@/books/collections";
import { Cover } from "@/books/cover";
import { FormError } from "@/components/form";
import { t } from "@/i18n";
import { VisibilityPicker, button, control } from "@/routes/collections";

/** One collection: its books in its order, which its owner arranges. */
export function CollectionPage({ collectionId }: { collectionId: string }) {
	const collection = useQuery(collectionQuery(collectionId));
	const [editing, setEditing] = useState(false);
	const c = collection.data;
	if (collection.error instanceof ApiError && collection.error.status === 404) {
		return <p className="text-slate-600 dark:text-slate-400">{t("collections.notFound")}</p>;
	}
	if (!c) {
		return (
			<>
				{collection.isPending && <p className="text-slate-500">{t("loading")}</p>}
				<FormError error={collection.error} />
			</>
		);
	}
	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-wrap items-baseline gap-x-4 gap-y-2">
				<h1 className="text-2xl font-semibold">{c.name}</h1>
				<span className="text-sm text-slate-600 dark:text-slate-400">
					{c.mine
						? t(`collections.visibility.${c.visibility}`)
						: t("collections.by", { name: c.ownerName })}
				</span>
				{c.mine && !editing && (
					<button
						type="button"
						className={button}
						onClick={() => setEditing(true)}
					>
						{t("collections.edit")}
					</button>
				)}
			</div>
			{c.description && !editing && (
				<p className="max-w-2xl">{c.description}</p>
			)}
			{editing && (
				<CollectionForm collection={c} onDone={() => setEditing(false)} />
			)}
			{c.bookList.length === 0 ? (
				<p className="text-slate-600 dark:text-slate-400">
					{c.mine ? t("collections.empty.mine") : t("collections.empty")}
				</p>
			) : (
				<CollectionBooks collection={c} />
			)}
		</div>
	);
}

function CollectionBooks({ collection }: { collection: CollectionDetail }) {
	const reorder = useReorderCollection(collection.id);
	const remove = useRemoveFromCollection();
	const ids = collection.bookList.map((b) => b.id);
	const busy = reorder.isPending || remove.isPending;
	return (
		<>
			<FormError error={reorder.error ?? remove.error} />
			<ol aria-label={t("collections.books")} className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
				{collection.bookList.map((book, i) => (
					<li key={book.id} className="flex items-center gap-3 py-2">
						<Link
							to="/books/$bookId"
							params={{ bookId: book.id }}
							className="flex min-w-0 flex-1 items-center gap-3"
						>
							<span className="w-10 shrink-0">
								<Cover book={book} size="small" />
							</span>
							<span className="flex min-w-0 flex-col">
								<span className="truncate font-medium">{book.title}</span>
								{book.authors.length > 0 && (
									<span className="truncate text-sm text-slate-600 dark:text-slate-400">
										{book.authors.join(", ")}
									</span>
								)}
							</span>
						</Link>
						{collection.mine && (
							<span className="flex shrink-0 gap-1">
								<button
									type="button"
									className={button}
									disabled={busy || i === 0}
									aria-label={t("collections.up", { title: book.title })}
									onClick={() => reorder.mutate(moved(ids, i, -1))}
								>
									↑
								</button>
								<button
									type="button"
									className={button}
									disabled={busy || i === ids.length - 1}
									aria-label={t("collections.down", { title: book.title })}
									onClick={() => reorder.mutate(moved(ids, i, 1))}
								>
									↓
								</button>
								<button
									type="button"
									className={button}
									disabled={busy}
									aria-label={t("collections.remove", { title: book.title })}
									onClick={() =>
										remove.mutate({ id: collection.id, book: book.id })
									}
								>
									✕
								</button>
							</span>
						)}
					</li>
				))}
			</ol>
		</>
	);
}

function CollectionForm({
	collection,
	onDone,
}: {
	collection: CollectionDetail;
	onDone: () => void;
}) {
	const [name, setName] = useState(collection.name);
	const [description, setDescription] = useState(collection.description ?? "");
	const [visibility, setVisibility] = useState<Visibility>(
		collection.visibility,
	);
	const [deleting, setDeleting] = useState(false);
	const update = useUpdateCollection(collection.id);
	const del = useDeleteCollection(collection.id);
	const navigate = useNavigate();
	return (
		<form
			aria-label={t("collections.edit")}
			className="flex max-w-2xl flex-col gap-3 rounded-lg border border-slate-200 p-4 text-sm dark:border-slate-800"
			onSubmit={(e) => {
				e.preventDefault();
				update.mutate({ name, description, visibility }, { onSuccess: onDone });
			}}
		>
			<label className="flex flex-col gap-1">
				<span>{t("collections.name")}</span>
				<input
					required
					value={name}
					onChange={(e) => setName(e.target.value)}
					className={control}
				/>
			</label>
			<label className="flex flex-col gap-1">
				<span>{t("collections.description")}</span>
				<textarea
					rows={3}
					value={description}
					onChange={(e) => setDescription(e.target.value)}
					className={control}
				/>
			</label>
			<VisibilityPicker value={visibility} onChange={setVisibility} />
			<p className="text-slate-600 dark:text-slate-400">
				{t("collections.sharedNote")}
			</p>
			<FormError error={update.error ?? del.error} />
			<div className="flex flex-wrap gap-2">
				<button type="submit" className={button} disabled={update.isPending}>
					{t("collections.save")}
				</button>
				<button type="button" className={button} onClick={onDone}>
					{t("collections.cancel")}
				</button>
				<span className="grow" />
				{deleting ? (
					<>
						<span className="self-center">{t("collections.deleteSure")}</span>
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
						{t("collections.delete")}
					</button>
				)}
			</div>
		</form>
	);
}
