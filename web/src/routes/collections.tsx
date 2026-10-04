import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useRouteContext } from "@tanstack/react-router";
import { can } from "@/auth/session";
import { useState } from "react";
import {
	type Collection,
	type Visibility,
	collectionsQuery,
	useCreateCollection,
} from "@/books/collections";
import { smartShelvesQuery } from "@/books/smart-shelves";
import { FormError } from "@/components/form";
import { t } from "@/i18n";

export const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";
export const button =
	"rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

/** The person's collections and those others share. */
export function Collections() {
	const collections = useQuery(collectionsQuery());
	const mine = collections.data?.filter((c) => c.mine) ?? [];
	const shared = collections.data?.filter((c) => !c.mine) ?? [];
	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-semibold">{t("collections.title")}</h1>
			<NewCollection />
			<FormError error={collections.error} />
			<section
				aria-labelledby="collections-mine"
				className="flex flex-col gap-2"
			>
				<h2 id="collections-mine" className="text-lg font-medium">
					{t("collections.mine")}
				</h2>
				{collections.isSuccess && mine.length === 0 && (
					<p className="text-slate-600 dark:text-slate-400">
						{t("collections.none")}
					</p>
				)}
				<CollectionList collections={mine} />
			</section>
			<SmartShelves />
			{shared.length > 0 && (
				<section
					aria-labelledby="collections-shared"
					className="flex flex-col gap-2"
				>
					<h2 id="collections-shared" className="text-lg font-medium">
						{t("collections.shared")}
					</h2>
					<CollectionList collections={shared} />
				</section>
			)}
		</div>
	);
}

function CollectionList({ collections }: { collections: Collection[] }) {
	return (
		<ul className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
			{collections.map((c) => (
				<li key={c.id} className="py-2">
					<Link
						to="/collections/$collectionId"
						params={{ collectionId: c.id }}
						className="flex flex-wrap items-baseline gap-x-3 hover:underline"
					>
						<span className="font-medium">{c.name}</span>
						<span className="text-sm text-slate-600 dark:text-slate-400">
							{[
								t("collections.count", { count: c.books }),
								c.mine
									? t(`collections.visibility.${c.visibility}`)
									: t("collections.by", { name: c.ownerName }),
							].join(" · ")}
						</span>
					</Link>
					{c.description && (
						<p className="text-sm text-slate-600 dark:text-slate-400">
							{c.description}
						</p>
					)}
				</li>
			))}
		</ul>
	);
}

/** The smart shelves the person sees, theirs and shared ones, with what each holds for them. */
function SmartShelves() {
	const { user } = useRouteContext({ from: "/app" });
	const shelves = useQuery(smartShelvesQuery);
	return (
		<section aria-labelledby="smart-shelves" className="flex flex-col gap-2">
			<div className="flex flex-wrap items-baseline gap-3">
				<h2 id="smart-shelves" className="text-lg font-medium">
					{t("smart.title")}
				</h2>
				{can(user, "personal:manage") && (
					<Link to="/smart-shelves/new" className={button}>
						{t("smart.new")}
					</Link>
				)}
			</div>
			<FormError error={shelves.error} />
			{shelves.isSuccess && shelves.data.length === 0 && (
				<p className="text-slate-600 dark:text-slate-400">{t("smart.none")}</p>
			)}
			<ul className="flex flex-col divide-y divide-slate-200 dark:divide-slate-800">
				{shelves.data?.map((s) => (
					<li key={s.id} className="py-2">
						<Link
							to="/smart-shelves/$shelfId"
							params={{ shelfId: s.id }}
							className="flex flex-wrap items-baseline gap-x-3 hover:underline"
						>
							<span className="font-medium">{s.name}</span>
							<span className="text-sm text-slate-600 dark:text-slate-400">
								{[
									t("collections.count", { count: s.books }),
									s.mine ? t(`collections.visibility.${s.visibility}`) : t("collections.by", { name: s.ownerName }),
								].join(" · ")}
							</span>
						</Link>
					</li>
				))}
			</ul>
		</section>
	);
}

function NewCollection() {
	const [name, setName] = useState("");
	const [visibility, setVisibility] = useState<Visibility>("private");
	const create = useCreateCollection();
	const navigate = useNavigate();
	return (
		<form
			aria-labelledby="collection-new"
			className="flex flex-wrap items-end gap-3 rounded-lg border border-slate-200 p-4 text-sm dark:border-slate-800"
			onSubmit={(e) => {
				e.preventDefault();
				create.mutate(
					{ name, visibility },
					{
						onSuccess: (c) =>
							c &&
							navigate({
								to: "/collections/$collectionId",
								params: { collectionId: c.id },
							}),
					},
				);
			}}
		>
			<h2 id="collection-new" className="w-full text-lg font-medium">
				{t("collections.new")}
			</h2>
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
			<button type="submit" className={button} disabled={create.isPending}>
				{t("collections.create")}
			</button>
			<div className="w-full">
				<FormError error={create.error} />
			</div>
		</form>
	);
}

export function VisibilityPicker({
	value,
	onChange,
}: {
	value: Visibility;
	onChange: (v: Visibility) => void;
}) {
	return (
		<label className="flex flex-col gap-1">
			<span>{t("collections.visibility")}</span>
			<select
				value={value}
				onChange={(e) => onChange(e.target.value as Visibility)}
				className={control}
			>
				<option value="private">{t("collections.visibility.private")}</option>
				<option value="shared">{t("collections.visibility.shared")}</option>
			</select>
		</label>
	);
}
