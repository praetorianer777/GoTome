import { describe, expect, it } from "vitest";
import { groupOf, newGroup, newRule, replaced, treeOf } from "@/books/rules";

describe("the rule builder's tree", () => {
	it("makes the tree the server takes, with names split at commas", () => {
		const author = {
			...newRule("author"),
			text: "Brandon Sanderson, , Robin Hobb ",
		};
		const rating = {
			...newRule("rating"),
			op: "between" as const,
			from: "4",
			to: "",
		};
		const status = { ...newRule("status"), picked: ["unread"] };
		const series = { ...newRule("series"), op: "empty" as const, negate: true };
		const either = newGroup("any", [status, series]);
		expect(
			JSON.parse(treeOf(newGroup("all", [author, rating, either]))),
		).toEqual({
			all: [
				{
					field: "author",
					op: "in",
					values: ["Brandon Sanderson", "Robin Hobb"],
				},
				{ field: "rating", op: "between", values: ["4", ""] },
				{
					any: [
						{ field: "status", op: "in", values: ["unread"] },
						{ not: { field: "series", op: "empty" } },
					],
				},
			],
		});
	});

	it("reads a tree back as it was written", () => {
		const tree = JSON.stringify({
			all: [
				{ field: "author", op: "in", values: ["Brandon Sanderson"] },
				{
					not: {
						any: [
							{ field: "status", op: "in", values: ["completed", "abandoned"] },
						],
					},
				},
				{ not: { not: { field: "tag", op: "empty" } } },
			],
		});
		expect(treeOf(groupOf(tree))).toBe(
			JSON.stringify({
				all: [
					{ field: "author", op: "in", values: ["Brandon Sanderson"] },
					{
						not: {
							any: [
								{
									field: "status",
									op: "in",
									values: ["completed", "abandoned"],
								},
							],
						},
					},
					{ field: "tag", op: "empty" },
				],
			}),
		);
		expect(
			treeOf(
				groupOf(
					JSON.stringify({ field: "format", op: "in", values: ["epub"] }),
				),
			),
		).toBe(
			JSON.stringify({
				all: [{ field: "format", op: "in", values: ["epub"] }],
			}),
		);
		expect(treeOf(groupOf("not json"))).toBe(JSON.stringify({ all: [] }));
	});

	it("replaces and removes items however deep they are", () => {
		const inner = newRule("tag");
		const group = newGroup("all", [newRule(), newGroup("any", [inner])]);
		const changed = replaced(group, inner.key, { ...inner, text: "Fantasy" });
		expect(JSON.parse(treeOf(changed)).all[1]).toEqual({
			any: [{ field: "tag", op: "in", values: ["Fantasy"] }],
		});
		expect(JSON.parse(treeOf(replaced(group, inner.key, null))).all[1]).toEqual(
			{ any: [] },
		);
	});
});
