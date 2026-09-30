import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { Field, FormError, fieldErrors, SubmitButton } from "@/components/form";
import { t } from "@/i18n";
import {
	type Library,
	librariesQuery,
	useCreateLibrary,
	useDeleteLibrary,
	useUpdateLibrary,
} from "@/libraries/api";
import { ScanStatus } from "@/libraries/scan";

const selectClass =
	"rounded-md border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";
const quietButton =
	"rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-60 dark:border-slate-600 dark:hover:bg-slate-800";

export function AdminLibraries() {
	const libraries = useQuery(librariesQuery);

	return (
		<div className="flex flex-col gap-8">
			<h1 className="text-2xl font-semibold">{t("libraries.title")}</h1>

			<section
				aria-labelledby="existing-libraries"
				className="flex flex-col gap-3"
			>
				<h2 id="existing-libraries" className="text-lg font-medium">
					{t("libraries.existing")}
				</h2>
				{libraries.isPending && (
					<p className="text-slate-500">{t("loading")}</p>
				)}
				<FormError error={libraries.error} />
				{libraries.data?.length === 0 && (
					<p className="text-slate-600 dark:text-slate-400">
						{t("libraries.none")}
					</p>
				)}
				<ul className="flex flex-col gap-3">
					{libraries.data?.map((library) => (
						<LibraryRow key={library.id} library={library} />
					))}
				</ul>
			</section>

			<CreateLibrary />
		</div>
	);
}

function LibraryRow({ library }: { library: Library }) {
	const update = useUpdateLibrary();
	const remove = useDeleteLibrary();
	const [name, setName] = useState(library.name);
	const [confirming, setConfirming] = useState(false);
	const errors = fieldErrors(update.error);
	const external = library.mode === "external";

	function rename(event: FormEvent) {
		event.preventDefault();
		if (name.trim() !== library.name) {
			update.mutate({ id: library.id, changes: { name } });
		}
	}

	return (
		<li
			aria-label={library.name}
			className="flex flex-col gap-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-950"
		>
			<form onSubmit={rename} className="flex flex-wrap items-end gap-3">
				<div className="min-w-48 flex-1">
					<Field
						label={t("libraries.name")}
						value={name}
						onChange={(e) => setName(e.target.value)}
						error={errors.name}
					/>
				</div>
				<button
					type="submit"
					disabled={update.isPending || name.trim() === library.name}
					className={quietButton}
				>
					{t("libraries.rename")}
				</button>
			</form>

			<dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
				<dt className="text-slate-500 dark:text-slate-400">
					{t("libraries.folder")}
				</dt>
				<dd className="break-all font-mono">{library.rootPath}</dd>
				<dt className="text-slate-500 dark:text-slate-400">
					{t("libraries.mode")}
				</dt>
				<dd>
					{t(external ? "libraries.mode.external" : "libraries.mode.managed")}
				</dd>
			</dl>

			<ScanStatus library={library} canScan />

			<div className="flex flex-wrap items-center gap-x-6 gap-y-3 text-sm">
				<label className="flex items-center gap-2">
					<span>{t("libraries.visibility")}</span>
					<select
						value={library.visibility}
						disabled={update.isPending}
						onChange={(e) =>
							update.mutate({
								id: library.id,
								changes: { visibility: e.target.value },
							})
						}
						className={`${selectClass} py-1 text-sm`}
					>
						<option value="shared">{t("libraries.visibility.shared")}</option>
						<option value="private">{t("libraries.visibility.private")}</option>
					</select>
				</label>
				{external && (
					<label className="flex items-center gap-2">
						<input
							type="checkbox"
							checked={library.writable}
							disabled={update.isPending}
							onChange={(e) =>
								update.mutate({
									id: library.id,
									changes: { writable: e.target.checked },
								})
							}
						/>
						<span>{t("libraries.writable")}</span>
					</label>
				)}
				<div className="ml-auto flex items-center gap-2">
					{confirming ? (
						<>
							<span>{t("libraries.delete.confirm")}</span>
							<button
								type="button"
								disabled={remove.isPending}
								onClick={() => remove.mutate(library.id)}
								className="rounded-md bg-red-700 px-3 py-1 text-sm font-medium text-white hover:bg-red-800 disabled:opacity-60"
							>
								{t("libraries.delete.yes")}
							</button>
							<button
								type="button"
								onClick={() => setConfirming(false)}
								className={quietButton}
							>
								{t("libraries.delete.no")}
							</button>
						</>
					) : (
						<button
							type="button"
							onClick={() => setConfirming(true)}
							className={quietButton}
						>
							{t("libraries.delete")}
						</button>
					)}
				</div>
			</div>
			{Object.keys(errors).length === 0 && (
				<FormError error={update.error ?? remove.error} />
			)}
		</li>
	);
}

function CreateLibrary() {
	const create = useCreateLibrary();
	const modeHint = useId();
	const [name, setName] = useState("");
	const [mode, setMode] = useState<"managed" | "external">("managed");
	const [rootPath, setRootPath] = useState("");
	const [visibility, setVisibility] = useState<"shared" | "private">("shared");
	const errors = fieldErrors(create.error);

	function submit(event: FormEvent) {
		event.preventDefault();
		create.mutate(
			{
				name,
				mode,
				visibility,
				rootPath: mode === "external" ? rootPath : undefined,
			},
			{
				onSuccess: () => {
					setName("");
					setRootPath("");
				},
			},
		);
	}

	return (
		<section aria-labelledby="new-library" className="flex flex-col gap-3">
			<h2 id="new-library" className="text-lg font-medium">
				{t("libraries.new")}
			</h2>
			<form
				onSubmit={submit}
				className="flex max-w-xl flex-col gap-4"
				noValidate
			>
				<Field
					label={t("libraries.name")}
					value={name}
					onChange={(e) => setName(e.target.value)}
					error={errors.name}
					required
				/>
				{/* The hint describes the field rather than being part of its
				    name, or a screen reader would read it out as the label. */}
				<div className="flex flex-col gap-1">
					<label className="flex flex-col gap-1">
						<span className="text-sm font-medium">{t("libraries.mode")}</span>
						<select
							value={mode}
							onChange={(e) =>
								setMode(e.target.value as "managed" | "external")
							}
							aria-describedby={modeHint}
							className={selectClass}
						>
							<option value="managed">{t("libraries.mode.managed")}</option>
							<option value="external">{t("libraries.mode.external")}</option>
						</select>
					</label>
					<p
						id={modeHint}
						className="text-sm text-slate-500 dark:text-slate-400"
					>
						{t(
							mode === "managed"
								? "libraries.mode.managed.hint"
								: "libraries.mode.external.hint",
						)}
					</p>
				</div>
				{mode === "external" && (
					<Field
						label={t("libraries.folder")}
						value={rootPath}
						onChange={(e) => setRootPath(e.target.value)}
						error={errors.rootPath}
						hint={t("libraries.folder.hint")}
						spellCheck={false}
						autoCapitalize="none"
						required
					/>
				)}
				<label className="flex flex-col gap-1">
					<span className="text-sm font-medium">
						{t("libraries.visibility")}
					</span>
					<select
						value={visibility}
						onChange={(e) =>
							setVisibility(e.target.value as "shared" | "private")
						}
						className={selectClass}
					>
						<option value="shared">{t("libraries.visibility.shared")}</option>
						<option value="private">{t("libraries.visibility.private")}</option>
					</select>
				</label>
				{Object.keys(errors).length === 0 && <FormError error={create.error} />}
				<div>
					<SubmitButton pending={create.isPending}>
						{t("libraries.create")}
					</SubmitButton>
				</div>
			</form>
		</section>
	);
}
