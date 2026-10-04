import type { FacetField } from "@/books/filters";
import { STATUS_LABELS, STATUSES } from "@/books/reading";
import {
	RULE_FIELDS,
	RULE_OPS,
	type RuleGroup,
	type RuleItem,
	type RuleOp,
	newGroup,
	newRule,
	replaced,
} from "@/books/rules";
import { type MessageKey, t } from "@/i18n";

const control =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";
const button =
	"rounded-md border border-slate-300 px-2 py-1 hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800";

const OP_LABELS: Record<RuleOp, MessageKey> = {
	in: "rules.op.in",
	between: "rules.op.between",
	empty: "rules.op.empty",
};

/**
 * Edits a rule tree: groups that need every rule or any, nested as deep as
 * wanted, and rules on a field, each of which may be turned round.
 */
export function RuleBuilder({
	value,
	onChange,
}: {
	value: RuleGroup;
	onChange: (group: RuleGroup) => void;
}) {
	return <Group group={value} path={t("rules.top")} onChange={onChange} />;
}

function Group({
	group,
	path,
	onChange,
	onRemove,
}: {
	group: RuleGroup;
	/** How the group is named to a screen reader: "Rules", "Group 2 of Rules". */
	path: string;
	onChange: (group: RuleGroup) => void;
	onRemove?: () => void;
}) {
	const change = (
		item: RuleItem | RuleGroup,
		by: RuleItem | RuleGroup | null,
	) => onChange(replaced(group, item.key, by));
	return (
		<fieldset
			aria-label={path}
			className="flex flex-col gap-2 rounded-lg border border-slate-200 p-3 dark:border-slate-700"
		>
			<div className="flex flex-wrap items-center gap-2">
				<label className="flex items-center gap-2">
					<input
						type="checkbox"
						checked={group.negate}
						onChange={(e) => onChange({ ...group, negate: e.target.checked })}
					/>
					{t("rules.groupNot")}
				</label>
				<label className="flex items-center gap-2">
					<span>{t("rules.match")}</span>
					<select
						value={group.match}
						onChange={(e) =>
							onChange({
								...group,
								match: e.target.value as RuleGroup["match"],
							})
						}
						className={control}
					>
						<option value="all">{t("rules.match.all")}</option>
						<option value="any">{t("rules.match.any")}</option>
					</select>
				</label>
				<span className="grow" />
				{onRemove && (
					<button type="button" className={button} onClick={onRemove}>
						{t("rules.removeGroup")}
					</button>
				)}
			</div>
			<ul className="flex flex-col gap-2">
				{group.items.map((item, i) => {
					const name = t(
						item.kind === "group" ? "rules.groupName" : "rules.ruleName",
						{ n: i + 1, of: path },
					);
					return (
						<li key={item.key}>
							{item.kind === "group" ? (
								<Group
									group={item}
									path={name}
									onChange={(g) => change(item, g)}
									onRemove={() => change(item, null)}
								/>
							) : (
								<Rule
									rule={item}
									name={name}
									onChange={(r) => change(item, r)}
									onRemove={() => change(item, null)}
								/>
							)}
						</li>
					);
				})}
			</ul>
			<div className="flex gap-2">
				<button
					type="button"
					className={button}
					onClick={() =>
						onChange({ ...group, items: [...group.items, newRule()] })
					}
				>
					{t("rules.addRule")}
				</button>
				<button
					type="button"
					className={button}
					onClick={() =>
						onChange({
							...group,
							items: [
								...group.items,
								newGroup(group.match === "all" ? "any" : "all"),
							],
						})
					}
				>
					{t("rules.addGroup")}
				</button>
			</div>
		</fieldset>
	);
}

function Rule({
	rule,
	name,
	onChange,
	onRemove,
}: {
	rule: RuleItem;
	name: string;
	onChange: (rule: RuleItem) => void;
	onRemove: () => void;
}) {
	return (
		<fieldset aria-label={name} className="flex flex-wrap items-center gap-2">
			<label className="flex items-center gap-1">
				<input
					type="checkbox"
					checked={rule.negate}
					onChange={(e) => onChange({ ...rule, negate: e.target.checked })}
				/>
				{t("rules.not")}
			</label>
			<select
				aria-label={t("rules.field")}
				value={rule.field}
				onChange={(e) => {
					const field = e.target.value as FacetField;
					onChange({ ...newRule(field), key: rule.key, negate: rule.negate });
				}}
				className={control}
			>
				{RULE_FIELDS.map((f) => (
					<option key={f} value={f}>
						{t(`filter.field.${f}`)}
					</option>
				))}
			</select>
			<select
				aria-label={t("rules.op")}
				value={rule.op}
				onChange={(e) => onChange({ ...rule, op: e.target.value as RuleOp })}
				className={control}
			>
				{RULE_OPS[rule.field].map((op) => (
					<option key={op} value={op}>
						{t(OP_LABELS[op])}
					</option>
				))}
			</select>
			{rule.op === "in" && rule.field === "status" && (
				<span className="flex flex-wrap gap-x-3">
					{STATUSES.map((s) => (
						<label key={s} className="flex items-center gap-1">
							<input
								type="checkbox"
								checked={rule.picked.includes(s)}
								onChange={(e) =>
									onChange({
										...rule,
										picked: e.target.checked
											? [...rule.picked, s]
											: rule.picked.filter((p) => p !== s),
									})
								}
							/>
							{t(STATUS_LABELS[s])}
						</label>
					))}
				</span>
			)}
			{rule.op === "in" && rule.field !== "status" && (
				<input
					aria-label={t("rules.values")}
					placeholder={t(
						rule.field === "rating"
							? "rules.values.stars"
							: "rules.values.hint",
					)}
					value={rule.text}
					onChange={(e) => onChange({ ...rule, text: e.target.value })}
					className={`${control} min-w-48 flex-1`}
				/>
			)}
			{rule.op === "between" && (
				<>
					<input
						aria-label={t("rules.from")}
						inputMode="numeric"
						value={rule.from}
						onChange={(e) => onChange({ ...rule, from: e.target.value })}
						className={`${control} w-20`}
					/>
					<span>{t("rules.and")}</span>
					<input
						aria-label={t("rules.to")}
						inputMode="numeric"
						value={rule.to}
						onChange={(e) => onChange({ ...rule, to: e.target.value })}
						className={`${control} w-20`}
					/>
				</>
			)}
			<button
				type="button"
				className={button}
				aria-label={t("rules.remove")}
				onClick={onRemove}
			>
				✕
			</button>
		</fieldset>
	);
}
