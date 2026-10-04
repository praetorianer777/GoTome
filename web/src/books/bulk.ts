import type { MessageKey } from "@/i18n";
import { queryOptions, useMutation } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type BulkRequest = components["schemas"]["BulkRequest"];
export type BulkAction = BulkRequest["action"];
export type BulkChange = components["schemas"]["BulkChange"];
export type BulkStatus = components["schemas"]["BulkStatus"];
export type BulkResult = components["schemas"]["BulkResult"];
export type BulkOutcome = NonNullable<BulkResult["outcome"]>;

export const BULK_OUTCOMES: BulkOutcome[] = [
	"changed",
	"unchanged",
	"locked",
	"review",
	"notFound",
	"failed",
];

export const BULK_OUTCOME_LABELS: Record<BulkOutcome, MessageKey> = {
	changed: "bulk.outcome.changed",
	unchanged: "bulk.outcome.unchanged",
	locked: "bulk.outcome.locked",
	review: "bulk.outcome.review",
	notFound: "bulk.outcome.notFound",
	failed: "bulk.outcome.failed",
};

export function useStartBulk() {
	return useMutation({
		mutationFn: async (body: BulkRequest) =>
			(await api.POST("/books/bulk", { body })).data,
	});
}

const BULK_POLL_MS = 1500;

/** A bulk change, asked for again until it is finished. */
export function bulkQuery(id: string) {
	return queryOptions({
		queryKey: ["bulk", id],
		queryFn: async () =>
			(await api.GET("/bulk/{bulkId}", { params: { path: { bulkId: id } } }))
				.data,
		refetchInterval: (query) =>
			query.state.data && !query.state.data.finishedAt ? BULK_POLL_MS : false,
	});
}

/**
 * What a field of the bulk edit form does: nothing, set the value (an empty
 * one removes it), or, for lists, add or remove the names given.
 */
export type BulkMode = "" | "set" | "add" | "remove";

export const BULK_FIELDS = [
	"authors",
	"series",
	"publisher",
	"published",
	"language",
	"tags",
] as const;
export type BulkField = (typeof BULK_FIELDS)[number];

/** The fields that hold lists of names, which may be added to and taken from. */
export const BULK_LISTS: BulkField[] = ["authors", "tags"];

export type BulkForm = Record<BulkField, { mode: BulkMode; value: string }> & {
	includeLocked: boolean;
};

export function emptyBulkForm(): BulkForm {
	const form = { includeLocked: false } as BulkForm;
	for (const field of BULK_FIELDS) {
		form[field] = { mode: "", value: "" };
	}
	return form;
}

/** Names separated by commas, each once, without the empty ones. */
export function namesOf(text: string): string[] {
	return [
		...new Set(
			text
				.split(",")
				.map((n) => n.trim())
				.filter(Boolean),
		),
	];
}

/** The form as the change the server takes, or undefined when it changes nothing. */
export function bulkChangeOf(form: BulkForm): BulkChange | undefined {
	const change: BulkChange = {};
	let any = false;
	for (const field of BULK_FIELDS) {
		const { mode, value } = form[field];
		if (!mode) {
			continue;
		}
		any = true;
		if (field === "authors" || field === "tags") {
			const names = namesOf(value);
			const key =
				mode === "set"
					? field
					: (`${mode}${field === "authors" ? "Authors" : "Tags"}` as const);
			change[key] = names;
		} else {
			change[field] = value.trim();
		}
	}
	if (!any) {
		return undefined;
	}
	if (form.includeLocked) {
		change.includeLocked = true;
	}
	return change;
}
