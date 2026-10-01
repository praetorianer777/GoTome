import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type FormEvent, type ReactNode, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { type BookDetail, bookQuery } from "@/books/api";
import { Cover } from "@/books/cover";
import {
	type BookEdit,
	type FieldState,
	LOCKABLE,
	type Lockable,
	type NameKind,
	namesQuery,
	useDeleteCover,
	useEditBook,
	usePutCover,
} from "@/books/edit";
import { FormError, SubmitButton, fieldErrors } from "@/components/form";
import { type MessageKey, t } from "@/i18n";

type Role = "author" | "narrator" | "translator" | "editor" | "illustrator";
const ROLES: Role[] = [
	"author",
	"narrator",
	"translator",
	"editor",
	"illustrator",
];
const IDENTIFIER_TYPES = [
	"isbn",
	"asin",
	"doi",
	"openlibrary",
	"google",
	"hardcover",
	"other",
];

const inputClass =
	"w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 aria-invalid:border-red-600 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100 dark:aria-invalid:border-red-400";
const quietButton =
	"whitespace-nowrap rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** The form's values: what the person sees in the inputs. */
interface Draft {
	title: string;
	subtitle: string;
	description: string;
	language: string;
	published: string;
	publisher: string;
	seriesName: string;
	seriesIndex: string;
	pageCount: string;
	contributors: { name: string; role: Role }[];
	tags: string[];
	/** The book's own identifiers; those of its files are not edited here. */
	identifiers: { type: string; value: string }[];
}

function draftOf(book: BookDetail): Draft {
	return {
		title: book.title,
		subtitle: book.subtitle ?? "",
		description: book.description ?? "",
		language: book.language ?? "",
		published: book.published ?? "",
		publisher: book.publisher ?? "",
		seriesName: book.series ?? "",
		seriesIndex: book.seriesIndex != null ? String(book.seriesIndex) : "",
		pageCount: book.pageCount != null ? String(book.pageCount) : "",
		contributors: book.contributors.map((c) => ({
			name: c.name,
			role: c.role,
		})),
		tags: [...book.tags],
		identifiers: book.identifiers
			.filter((i) => !i.fromFile)
			.map((i) => ({ type: i.type, value: i.value })),
	};
}

const same = (a: unknown, b: unknown) =>
	JSON.stringify(a) === JSON.stringify(b);

/** What the person changed, as the server takes it. */
function changes(base: Draft, draft: Draft): BookEdit {
	const edit: BookEdit = {};
	for (const key of [
		"title",
		"subtitle",
		"language",
		"published",
		"publisher",
	] as const) {
		if (draft[key].trim() !== base[key].trim()) {
			edit[key] = draft[key].trim();
		}
	}
	if (draft.description.trim() !== base.description.trim()) {
		edit.description = draft.description.trim();
	}
	if (
		draft.seriesName.trim() !== base.seriesName.trim() ||
		draft.seriesIndex.trim() !== base.seriesIndex.trim()
	) {
		const index = draft.seriesIndex.trim();
		edit.series = {
			name: draft.seriesName.trim(),
			index: index === "" ? undefined : Number(index),
		};
	}
	if (draft.pageCount.trim() !== base.pageCount.trim()) {
		edit.pageCount =
			draft.pageCount.trim() === "" ? 0 : Number(draft.pageCount);
	}
	const people = draft.contributors.filter((c) => c.name.trim() !== "");
	if (!same(people, base.contributors)) {
		edit.contributors = people.map((c) => ({
			name: c.name.trim(),
			role: c.role,
		}));
	}
	if (!same(draft.tags, base.tags)) {
		edit.tags = draft.tags;
	}
	const identifiers = draft.identifiers.filter((i) => i.value.trim() !== "");
	if (!same(identifiers, base.identifiers)) {
		edit.identifiers = identifiers;
	}
	return edit;
}

function provenance(state: FieldState | undefined): string {
	switch (state?.source) {
		case "file":
			return t("edit.source.file", {
				format: (state.detail ?? "").toUpperCase(),
			});
		case "filename":
			return t("edit.source.filename");
		case "manual":
			return t("edit.source.manual");
		case "provider":
			return t("edit.source.provider", { provider: state.detail ?? "" });
	}
	return "";
}

export function BookEditPage({ id }: { id: string }) {
	const book = useQuery(bookQuery(id));
	if (book.error instanceof ApiError && book.error.status === 404) {
		return (
			<p className="text-slate-600 dark:text-slate-400">{t("book.notFound")}</p>
		);
	}
	if (!book.data) {
		return (
			<div className="flex flex-col gap-4">
				{book.isPending && <p className="text-slate-500">{t("loading")}</p>}
				<FormError error={book.error} />
			</div>
		);
	}
	return <EditForm book={book.data} />;
}

function EditForm({ book }: { book: BookDetail }) {
	const navigate = useNavigate();
	// What the form started from: the book may be read again while the
	// person types, and only what they changed is theirs to send.
	const [base] = useState(() => ({
		draft: draftOf(book),
		locks: Object.fromEntries(
			LOCKABLE.map((f) => [f, book.fields[f]?.locked ?? false]),
		) as Record<Lockable, boolean>,
	}));
	const [draft, setDraft] = useState(base.draft);
	const [locks, setLocks] = useState(base.locks);
	const edit = useEditBook(book.id);
	const errors = fieldErrors(edit.error);

	function change<K extends keyof Draft>(
		key: K,
		value: Draft[K],
		field: Lockable,
	) {
		setDraft((d) => ({ ...d, [key]: value }));
		setLocks((l) => ({ ...l, [field]: true }));
	}
	const lock = (field: Lockable) => (on: boolean) =>
		setLocks((l) => ({ ...l, [field]: on }));
	const toBook = () =>
		navigate({ to: "/books/$bookId", params: { bookId: book.id } });

	function submit(event: FormEvent) {
		event.preventDefault();
		const body = changes(base.draft, draft);
		// The server locks what it is sent; the rest of the locks go along
		// where they differ from that.
		const lockChanges: Record<string, boolean> = {};
		for (const field of LOCKABLE) {
			const server = field in body ? true : base.locks[field];
			if (locks[field] !== server) {
				lockChanges[field] = locks[field];
			}
		}
		if (Object.keys(lockChanges).length > 0) {
			body.locks = lockChanges;
		}
		if (Object.keys(body).length === 0) {
			toBook();
			return;
		}
		edit.mutate(body, { onSuccess: toBook });
	}

	const row = (
		field: Lockable,
		label: MessageKey,
		input: (id: string, about: string) => ReactNode,
		extra?: { hint?: MessageKey; group?: boolean },
	) => (
		<Row
			label={t(label)}
			state={book.fields[field]}
			locked={locks[field]}
			onLock={lock(field)}
			error={errors[field]}
			hint={extra?.hint && t(extra.hint)}
			group={extra?.group}
		>
			{input}
		</Row>
	);

	return (
		<div className="flex flex-col gap-6">
			<Link
				to="/books/$bookId"
				params={{ bookId: book.id }}
				className="self-start text-sm text-brand-strong underline underline-offset-4 dark:text-brand"
			>
				{t("edit.back")}
			</Link>
			<div>
				<h1 className="text-2xl font-semibold">
					{t("edit.title", { title: book.title })}
				</h1>
				<p className="mt-1 max-w-2xl text-slate-600 dark:text-slate-400">
					{t("edit.intro")}
				</p>
			</div>
			<form
				onSubmit={submit}
				noValidate
				className="flex max-w-2xl flex-col gap-5"
			>
				<CoverRow book={book} locked={locks.cover} onLock={lock("cover")} />
				{row("title", "edit.field.title", (id, about) => (
					<input
						id={id}
						aria-describedby={about}
						aria-invalid={errors.title ? true : undefined}
						required
						value={draft.title}
						onChange={(e) => change("title", e.target.value, "title")}
						className={inputClass}
					/>
				))}
				{row("subtitle", "edit.field.subtitle", (id, about) => (
					<input
						id={id}
						aria-describedby={about}
						value={draft.subtitle}
						onChange={(e) => change("subtitle", e.target.value, "subtitle")}
						className={inputClass}
					/>
				))}
				{row(
					"contributors",
					"edit.field.contributors",
					() => (
						<People
							value={draft.contributors}
							onChange={(v) => change("contributors", v, "contributors")}
						/>
					),
					{ group: true },
				)}
				{row("series", "edit.field.series", (id, about) => (
					<div className="flex gap-2">
						<NameInput
							id={id}
							kind="series"
							aria-describedby={about}
							value={draft.seriesName}
							onChange={(v) => change("seriesName", v, "series")}
						/>
						<input
							aria-label={t("edit.field.seriesIndex")}
								placeholder={t("edit.seriesIndexShort")}
							inputMode="decimal"
							value={draft.seriesIndex}
							onChange={(e) => change("seriesIndex", e.target.value, "series")}
							className={`${inputClass} max-w-24`}
						/>
					</div>
				))}
				{row("publisher", "edit.field.publisher", (id, about) => (
					<NameInput
						id={id}
						kind="publisher"
						aria-describedby={about}
						value={draft.publisher}
						onChange={(v) => change("publisher", v, "publisher")}
					/>
				))}
				{row(
					"published",
					"edit.field.published",
					(id, about) => (
						<input
							id={id}
							aria-describedby={about}
							aria-invalid={errors.published ? true : undefined}
							value={draft.published}
							onChange={(e) => change("published", e.target.value, "published")}
							className={inputClass}
						/>
					),
					{ hint: "edit.hint.published" },
				)}
				{row(
					"language",
					"edit.field.language",
					(id, about) => (
						<input
							id={id}
							aria-describedby={about}
							aria-invalid={errors.language ? true : undefined}
							value={draft.language}
							onChange={(e) => change("language", e.target.value, "language")}
							className={inputClass}
						/>
					),
					{ hint: "edit.hint.language" },
				)}
				{row("pageCount", "edit.field.pageCount", (id, about) => (
					<input
						id={id}
						aria-describedby={about}
						inputMode="numeric"
						value={draft.pageCount}
						onChange={(e) => change("pageCount", e.target.value, "pageCount")}
						className={`${inputClass} max-w-32`}
					/>
				))}
				{row(
					"tags",
					"edit.field.tags",
					() => (
						<Tags
							value={draft.tags}
							onChange={(v) => change("tags", v, "tags")}
						/>
					),
					{ group: true },
				)}
				{row(
					"identifiers",
					"edit.field.identifiers",
					() => (
						<Identifiers
							book={book}
							value={draft.identifiers}
							onChange={(v) => change("identifiers", v, "identifiers")}
						/>
					),
					{ group: true },
				)}
				{row("description", "edit.field.description", (id, about) => (
					<textarea
						id={id}
						aria-describedby={about}
						rows={8}
						value={draft.description}
						onChange={(e) =>
							change("description", e.target.value, "description")
						}
						className={inputClass}
					/>
				))}
				<FormError error={edit.error} />
				<div className="flex items-center gap-3">
					<SubmitButton pending={edit.isPending}>{t("edit.save")}</SubmitButton>
					<button type="button" onClick={toBook} className={quietButton}>
						{t("edit.cancel")}
					</button>
				</div>
			</form>
		</div>
	);
}

/**
 * One field of the form: its label, its input, where its value came from and
 * its lock. A field of several inputs is a group under its label.
 */
function Row({
	label,
	state,
	locked,
	onLock,
	error,
	hint,
	group,
	children,
}: {
	label: string;
	state: FieldState | undefined;
	locked: boolean;
	onLock: (on: boolean) => void;
	error?: string;
	hint?: string;
	group?: boolean;
	children: (id: string, about: string) => ReactNode;
}) {
	const id = useId();
	const about = `${id}-about`;
	const notes = [hint, provenance(state)].filter(Boolean).join(" · ");
	const Wrapper = group ? "fieldset" : "div";
	return (
		<Wrapper className="flex flex-col gap-1">
			<div className="flex flex-wrap items-baseline justify-between gap-x-4">
				{group ? (
					<legend className="float-left text-sm font-medium">{label}</legend>
				) : (
					<label htmlFor={id} className="text-sm font-medium">
						{label}
					</label>
				)}
				<label className="flex items-center gap-1.5 text-sm text-slate-600 dark:text-slate-400">
					<input
						type="checkbox"
						checked={locked}
						onChange={(e) => onLock(e.target.checked)}
						aria-label={t("edit.lockNamed", { field: label })}
					/>
					{t("edit.locked")}
				</label>
			</div>
			{children(id, about)}
			{notes && (
				<p id={about} className="text-sm text-slate-500 dark:text-slate-400">
					{notes}
				</p>
			)}
			{error && (
				<p className="text-sm text-red-700 dark:text-red-300">{error}</p>
			)}
		</Wrapper>
	);
}

/** A name input that offers the names already in use as the person types. */
function NameInput({
	kind,
	value,
	onChange,
	id,
	...rest
}: {
	kind: NameKind;
	value: string;
	onChange: (value: string) => void;
	id?: string;
	"aria-label"?: string;
	"aria-describedby"?: string;
	onKeyDown?: (event: React.KeyboardEvent<HTMLInputElement>) => void;
}) {
	const list = useId();
	const names = useQuery(namesQuery(kind, value));
	return (
		<>
			<input
				id={id}
				list={list}
				autoComplete="off"
				value={value}
				onChange={(e) => onChange(e.target.value)}
				className={inputClass}
				{...rest}
			/>
			<datalist id={list}>
				{(names.data ?? [])
					.filter((n) => n !== value)
					.map((n) => (
						<option key={n} value={n} />
					))}
			</datalist>
		</>
	);
}

function People({
	value,
	onChange,
}: {
	value: Draft["contributors"];
	onChange: (v: Draft["contributors"]) => void;
}) {
	const set = (i: number, person: Draft["contributors"][number]) =>
		onChange(value.map((p, j) => (j === i ? person : p)));
	return (
		<div className="flex flex-col gap-2">
			{value.map((person, i) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: a row is its place in the list; names change as they are typed
				<div key={i} className="flex flex-wrap gap-2 sm:flex-nowrap">
					<NameInput
						kind="author"
						aria-label={t("edit.person.name", { number: i + 1 })}
						value={person.name}
						onChange={(name) => set(i, { ...person, name })}
					/>
					<select
						aria-label={t("edit.person.role", { number: i + 1 })}
						value={person.role}
						onChange={(e) =>
							set(i, { ...person, role: e.target.value as Role })
						}
						className={`${inputClass} sm:max-w-40`}
					>
						{ROLES.map((role) => (
							<option key={role} value={role}>
								{t(`edit.role.${role}`)}
							</option>
						))}
					</select>
					<button
						type="button"
						onClick={() => onChange(value.filter((_, j) => j !== i))}
						aria-label={t("edit.person.remove", {
							name: person.name || String(i + 1),
						})}
						className={quietButton}
					>
						×
					</button>
				</div>
			))}
			<button
				type="button"
				onClick={() => onChange([...value, { name: "", role: "author" }])}
				className={`${quietButton} self-start`}
			>
				{t("edit.person.add")}
			</button>
		</div>
	);
}

function Tags({
	value,
	onChange,
}: {
	value: string[];
	onChange: (v: string[]) => void;
}) {
	const [typed, setTyped] = useState("");
	function add() {
		const tag = typed.trim();
		if (tag && !value.some((v) => v.toLowerCase() === tag.toLowerCase())) {
			onChange([...value, tag]);
		}
		setTyped("");
	}
	return (
		<div className="flex flex-col gap-2">
			{value.length > 0 && (
				<ul className="flex flex-wrap gap-2">
					{value.map((tag) => (
						<li
							key={tag}
							className="flex items-center gap-1 rounded-full bg-slate-100 py-0.5 pr-1 pl-3 text-sm dark:bg-slate-800"
						>
							{tag}
							<button
								type="button"
								onClick={() => onChange(value.filter((v) => v !== tag))}
								aria-label={t("edit.tag.remove", { tag })}
								className="rounded-full px-1.5 hover:bg-slate-200 dark:hover:bg-slate-700"
							>
								×
							</button>
						</li>
					))}
				</ul>
			)}
			<div className="flex gap-2">
				<NameInput
					kind="tag"
					aria-label={t("edit.tag.new")}
					value={typed}
					onChange={setTyped}
					onKeyDown={(e) => {
						if (e.key === "Enter") {
							e.preventDefault();
							add();
						}
					}}
				/>
				<button type="button" onClick={add} className={quietButton}>
					{t("edit.tag.add")}
				</button>
			</div>
		</div>
	);
}

function Identifiers({
	book,
	value,
	onChange,
}: {
	book: BookDetail;
	value: Draft["identifiers"];
	onChange: (v: Draft["identifiers"]) => void;
}) {
	const set = (i: number, ident: Draft["identifiers"][number]) =>
		onChange(value.map((v, j) => (j === i ? ident : v)));
	return (
		<div className="flex flex-col gap-2">
			{book.identifiers
				.filter((i) => i.fromFile)
				.map((i) => (
					<p
						key={`${i.type}${i.value}`}
						className="text-sm text-slate-600 dark:text-slate-400"
					>
						{t("edit.identifier.fromFile", {
							type: i.type.toUpperCase(),
							value: i.value,
						})}
					</p>
				))}
			{value.map((ident, i) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: a row is its place in the list; values change as they are typed
				<div key={i} className="flex gap-2">
					<select
						aria-label={t("edit.identifier.type", { number: i + 1 })}
						value={ident.type}
						onChange={(e) => set(i, { ...ident, type: e.target.value })}
						className={`${inputClass} max-w-36`}
					>
						{IDENTIFIER_TYPES.map((type) => (
							<option key={type} value={type}>
								{type.toUpperCase()}
							</option>
						))}
					</select>
					<input
						aria-label={t("edit.identifier.value", { number: i + 1 })}
						value={ident.value}
						onChange={(e) => set(i, { ...ident, value: e.target.value })}
						className={inputClass}
					/>
					<button
						type="button"
						onClick={() => onChange(value.filter((_, j) => j !== i))}
						aria-label={t("edit.identifier.remove", { number: i + 1 })}
						className={quietButton}
					>
						×
					</button>
				</div>
			))}
			<button
				type="button"
				onClick={() => onChange([...value, { type: "isbn", value: "" }])}
				className={`${quietButton} self-start`}
			>
				{t("edit.identifier.add")}
			</button>
		</div>
	);
}

/** The cover is changed at once, not with the form: an image is no draft. */
function CoverRow({
	book,
	locked,
	onLock,
}: {
	book: BookDetail;
	locked: boolean;
	onLock: (on: boolean) => void;
}) {
	const put = usePutCover(book.id);
	const remove = useDeleteCover(book.id);
	const busy = put.isPending || remove.isPending;
	return (
		<Row
			label={t("edit.field.cover")}
			state={book.fields.cover}
			locked={locked}
			onLock={onLock}
			error={fieldErrors(put.error).file}
			group
		>
			{() => (
				<div className="flex items-end gap-4">
					<div className="w-24">
						<Cover book={book} size="small" />
					</div>
					<div className="flex flex-col items-start gap-2">
						<label
							className={`${quietButton} cursor-pointer has-[:disabled]:opacity-60`}
						>
							{t("edit.cover.choose")}
							<input
								type="file"
								accept="image/jpeg,image/png,image/gif,image/webp"
								disabled={busy}
								className="sr-only"
								onChange={(e) => {
									const file = e.target.files?.[0];
									if (file) {
										put.mutate(file, { onSuccess: () => onLock(true) });
									}
									e.target.value = "";
								}}
							/>
						</label>
						{book.coverKey && (
							<button
								type="button"
								disabled={busy}
								onClick={() => remove.mutate(undefined, { onSuccess: () => onLock(true) })}
								className={quietButton}
							>
								{t("edit.cover.remove")}
							</button>
						)}
						<FormError
							error={
								fieldErrors(put.error).file
									? undefined
									: (put.error ?? remove.error)
							}
						/>
					</div>
				</div>
			)}
		</Row>
	);
}
