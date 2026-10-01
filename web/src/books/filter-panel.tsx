import { useState } from "react";
import {
	FACET_FIELDS,
	type Facet,
	type FacetField,
	type FilterPicks,
	pickCount,
} from "@/books/filters";
import { type MessageKey, t } from "@/i18n";
import { formatLanguage } from "@/lib/format";

/** How many values of a field show before "Show all". */
const SHOWN = 8;

const FIELD_LABELS: Record<FacetField, MessageKey> = {
	author: "filter.field.author",
	series: "filter.field.series",
	tag: "filter.field.tag",
	language: "filter.field.language",
	published: "filter.field.published",
	format: "filter.field.format",
};

/** A value as people read it; the server labels names, the rest is ours. */
function valueLabel(field: FacetField, value: string, label?: string): string {
	switch (field) {
		case "language":
			return formatLanguage(value);
		case "published":
			return t("filter.decade", { decade: value });
		case "format":
			return value.toUpperCase();
		default:
			return label ?? value;
	}
}

export function FilterPanel({
	facets,
	picks,
	onChange,
}: {
	facets: Facet[];
	picks: FilterPicks;
	onChange: (picks: FilterPicks) => void;
}) {
	const byField = new Map(facets.map((f) => [f.field, f]));
	const active = pickCount(picks);

	function toggle(field: FacetField, value: string) {
		const current = picks[field] ?? [];
		const next = current.includes(value)
			? current.filter((v) => v !== value)
			: [...current, value];
		onChange({ ...picks, [field]: next.length > 0 ? next : undefined });
	}

	return (
		<div className="flex flex-col gap-5 text-sm">
			{active > 0 && (
				<button
					type="button"
					onClick={() => onChange({})}
					className="self-start rounded-md border border-slate-300 px-3 py-1 hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
				>
					{t("filter.clear")}
				</button>
			)}
			{FACET_FIELDS.map((field) => (
				<FacetGroup
					key={field}
					field={field}
					facet={byField.get(field)}
					picked={picks[field] ?? []}
					onToggle={(value) => toggle(field, value)}
				/>
			))}
		</div>
	);
}

function FacetGroup({
	field,
	facet,
	picked,
	onToggle,
}: {
	field: FacetField;
	facet: Facet | undefined;
	picked: string[];
	onToggle: (value: string) => void;
}) {
	const [all, setAll] = useState(false);
	const values = facet?.values ?? [];
	// What was picked stays in view even when no book left has it, so that
	// it can be taken back.
	const missing = picked
		.filter((v) => !values.some((f) => f.value === v))
		.map((value) => ({ value, label: value, count: 0 }));
	const listed = [...values, ...missing];
	if (listed.length === 0) {
		return null;
	}
	const shown = all
		? listed
		: listed.filter((v, i) => i < SHOWN || picked.includes(v.value));

	return (
		<fieldset className="flex flex-col gap-1">
			<legend className="mb-1 font-medium">{t(FIELD_LABELS[field])}</legend>
			{shown.map((v) => (
				<label key={v.value} className="flex items-baseline gap-2">
					<input
						type="checkbox"
						checked={picked.includes(v.value)}
						onChange={() => onToggle(v.value)}
						aria-label={t("filter.value", {
							label: valueLabel(field, v.value, v.label),
							count: v.count,
						})}
						className="accent-sky-700"
					/>
					<span aria-hidden="true" className="min-w-0 flex-1 truncate">
						{valueLabel(field, v.value, v.label)}
					</span>
					<span aria-hidden="true" className="text-slate-500 tabular-nums">
						{v.count}
					</span>
				</label>
			))}
			{listed.length > SHOWN && (
				<button
					type="button"
					onClick={() => setAll(!all)}
					className="self-start text-sky-800 hover:underline dark:text-sky-300"
				>
					{all ? t("filter.fewer") : t("filter.all", { count: listed.length })}
				</button>
			)}
		</fieldset>
	);
}
