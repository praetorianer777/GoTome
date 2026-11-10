import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api } from "@/api/client";
import { kindleQuery } from "@/books/kindle";
import { Field, FormError, SubmitButton, fieldErrors } from "@/components/form";
import { t } from "@/i18n";
import { formatSize } from "@/lib/format";

function useSetKindleAddress() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (address: string) => {
			return (await api.PUT("/me/kindle", { body: { address } })).data;
		},
		onSuccess: (data) => queryClient.setQueryData(kindleQuery.queryKey, data),
	});
}

/** The person's Kindle address, and what Amazon needs for books to arrive there. */
export function KindleAddress() {
	const status = useQuery(kindleQuery);
	const save = useSetKindleAddress();
	const [address, setAddress] = useState("");
	const saved = status.data?.address;
	useEffect(() => {
		if (saved !== undefined) setAddress(saved);
	}, [saved]);
	if (!status.data) return null;
	const errors = fieldErrors(save.error);
	return (
		<section aria-labelledby="kindle" className="flex max-w-md flex-col gap-4">
			<h2 id="kindle" className="text-lg font-medium">
				{t("kindle.profile")}
			</h2>
			<p className="text-slate-600 dark:text-slate-400">
				{status.data.available
					? t("kindle.profile.intro", {
							format: status.data.format.toUpperCase(),
							size: formatSize(status.data.maxBytes),
						})
					: t("kindle.profile.off")}
			</p>
			{status.data.available && (
				<p className="text-slate-600 dark:text-slate-400">
					{t("kindle.approve", { sender: status.data.sender ?? "" })}
				</p>
			)}
			<form
				onSubmit={(e) => {
					e.preventDefault();
					save.mutate(address.trim());
				}}
				className="flex flex-col gap-4"
				noValidate
			>
				<Field
					label={t("kindle.address")}
					type="email"
					inputMode="email"
					placeholder="name@kindle.com"
					hint={t("kindle.address.hint")}
					value={address}
					onChange={(e) => setAddress(e.target.value)}
					error={errors.address}
					optional
				/>
				{Object.keys(errors).length === 0 && <FormError error={save.error} />}
				<div>
					<SubmitButton pending={save.isPending} kind="secondary">
						{t("kindle.save")}
					</SubmitButton>
				</div>
			</form>
		</section>
	);
}
