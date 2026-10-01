import type { components } from "@/api/schema";

export type Facet = components["schemas"]["Facet"];
export type FacetField = Facet["field"];
export type FacetValue = components["schemas"]["FacetValue"];

/**
 * What the filter panel has picked, per field, as the address carries it. A
 * book matches when it has one of the picked values of every field; the
 * published field picks decades by their first year.
 */
export type FilterPicks = Partial<Record<FacetField, string[]>>;

export const FACET_FIELDS: FacetField[] = [
	"author",
	"series",
	"tag",
	"language",
	"published",
	"status",
	"rating",
	"format",
];

interface Rule {
	field?: string;
	op?: string;
	values?: string[];
	all?: Rule[];
	any?: Rule[];
}

/** The picks as the rule tree the server filters by, or undefined for none. */
export function filterTree(picks: FilterPicks): string | undefined {
	const all: Rule[] = [];
	for (const field of FACET_FIELDS) {
		const values = picks[field];
		if (!values?.length) {
			continue;
		}
		if (field === "published") {
			all.push({
				any: values.map((decade) => ({
					field,
					op: "between",
					values: [decade, String(Number(decade) + 9)],
				})),
			});
		} else {
			all.push({ field, op: "in", values });
		}
	}
	return all.length > 0 ? JSON.stringify({ all }) : undefined;
}

/** How many values are picked, over all fields. */
export function pickCount(picks: FilterPicks): number {
	return FACET_FIELDS.reduce((n, field) => n + (picks[field]?.length ?? 0), 0);
}

/** Reads the picks out of an address's search parameters, keeping only strings. */
export function picksFrom(search: Record<string, unknown>): FilterPicks {
	const picks: FilterPicks = {};
	for (const field of FACET_FIELDS) {
		const raw = search[field];
		const values = (Array.isArray(raw) ? raw : [raw]).filter(
			(v): v is string => typeof v === "string" && v !== "",
		);
		if (values.length > 0) {
			picks[field] = values;
		}
	}
	return picks;
}
