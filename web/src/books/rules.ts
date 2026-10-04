import { FACET_FIELDS, type FacetField } from "@/books/filters";

export type RuleOp = "in" | "between" | "empty";

/** The comparisons each field takes, as the server's filter registry has them. */
export const RULE_OPS: Record<FacetField, RuleOp[]> = {
	author: ["in", "empty"],
	series: ["in", "empty"],
	tag: ["in", "empty"],
	language: ["in", "empty"],
	published: ["between", "empty"],
	format: ["in"],
	status: ["in"],
	rating: ["in", "between", "empty"],
};

export const RULE_FIELDS = FACET_FIELDS;

/**
 * One rule as the builder edits it. Values of in are typed as one text,
 * split at commas when the tree is made, except for status, which is
 * picked; between keeps its lower and upper bound.
 */
export interface RuleItem {
	kind: "rule";
	key: number;
	negate: boolean;
	field: FacetField;
	op: RuleOp;
	text: string;
	picked: string[];
	from: string;
	to: string;
}

export interface RuleGroup {
	kind: "group";
	key: number;
	negate: boolean;
	match: "all" | "any";
	items: (RuleItem | RuleGroup)[];
}

/** The tree the server takes: all, any, not, or a rule. */
interface Node {
	all?: Node[];
	any?: Node[];
	not?: Node;
	field?: string;
	op?: string;
	values?: string[];
}

let nextKey = 1;
const key = () => nextKey++;

export function newRule(field: FacetField = "author"): RuleItem {
	const op = RULE_OPS[field][0] ?? "in";
	return {
		kind: "rule",
		key: key(),
		negate: false,
		field,
		op,
		text: "",
		picked: [],
		from: "",
		to: "",
	};
}

export function newGroup(
	match: RuleGroup["match"] = "all",
	items: RuleGroup["items"] = [],
): RuleGroup {
	return { kind: "group", key: key(), negate: false, match, items };
}

function valuesOf(rule: RuleItem): string[] {
	switch (rule.op) {
		case "empty":
			return [];
		case "between":
			return [rule.from.trim(), rule.to.trim()];
		default:
			return rule.field === "status"
				? rule.picked
				: rule.text
						.split(",")
						.map((v) => v.trim())
						.filter(Boolean);
	}
}

function toNode(item: RuleItem | RuleGroup): Node {
	const node: Node =
		item.kind === "group"
			? { [item.match]: item.items.map(toNode) }
			: {
					field: item.field,
					op: item.op,
					...(item.op === "empty" ? {} : { values: valuesOf(item) }),
				};
	return item.negate ? { not: node } : node;
}

/** The group as the rule tree's JSON. */
export function treeOf(group: RuleGroup): string {
	return JSON.stringify(toNode(group));
}

function fromNode(node: Node, negate: boolean): RuleItem | RuleGroup {
	if (node.not) {
		return fromNode(node.not, !negate);
	}
	if (node.field) {
		const field = (RULE_FIELDS as string[]).includes(node.field)
			? (node.field as FacetField)
			: "author";
		const op = (["in", "between", "empty"] as string[]).includes(node.op ?? "")
			? (node.op as RuleOp)
			: "in";
		const values = node.values ?? [];
		return {
			...newRule(field),
			negate,
			op,
			text: op === "in" && field !== "status" ? values.join(", ") : "",
			picked: field === "status" ? values : [],
			from: op === "between" ? (values[0] ?? "") : "",
			to: op === "between" ? (values[1] ?? "") : "",
		};
	}
	const match = node.any ? "any" : "all";
	return {
		...newGroup(
			match,
			(node[match] ?? []).map((n) => fromNode(n, false)),
		),
		negate,
	};
}

/** The builder's group for a tree's JSON; a tree that is one rule becomes a group of it. */
export function groupOf(tree: string): RuleGroup {
	let node: Node;
	try {
		node = JSON.parse(tree) as Node;
	} catch {
		return newGroup();
	}
	const item = fromNode(node, false);
	return item.kind === "group" ? item : newGroup("all", [item]);
}

/** The tree with the item of that key replaced, or taken out when replaced by nothing. */
export function replaced(
	group: RuleGroup,
	itemKey: number,
	by: RuleItem | RuleGroup | null,
): RuleGroup {
	return {
		...group,
		items: group.items.flatMap((item) => {
			if (item.key === itemKey) {
				return by ? [by] : [];
			}
			return item.kind === "group" ? [replaced(item, itemKey, by)] : [item];
		}),
	};
}
