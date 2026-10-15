import { queryOptions, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { t } from "@/i18n";

export type BuildInfo = components["schemas"]["BuildInfo"];

const RELEASES = "https://github.com/praetorianer777/GoTome/releases/tag/v";

/**
 * The server's build. Asked again every few minutes and when the tab comes
 * back into view, so an update made while it was open comes to light.
 */
export const versionQuery = queryOptions({
	queryKey: ["version"],
	queryFn: async () => (await api.GET("/version")).data ?? null,
	refetchInterval: 5 * 60_000,
	staleTime: 60_000,
});

/** A version of three numbers is a release; "dev" and anything else is not. */
export function isRelease(build: BuildInfo): boolean {
	return /^\d+\.\d+\.\d+$/.test(build.version);
}

/**
 * The build the page was loaded from, and whether the server now runs
 * another one. The first answer is taken for the loaded one: the app's own
 * files carry no version.
 */
export function useServerVersion(): {
	build: BuildInfo | null;
	updated: boolean;
} {
	const { data } = useQuery(versionQuery);
	const [loaded, setLoaded] = useState<BuildInfo | null>(null);
	useEffect(() => {
		if (data && !loaded) setLoaded(data);
	}, [data, loaded]);
	const updated =
		!!data &&
		!!loaded &&
		(data.version !== loaded.version ||
			(data.commit ?? "") !== (loaded.commit ?? ""));
	return { build: loaded, updated };
}

export function VersionFooter({
	build,
	updated,
}: {
	build: BuildInfo | null;
	updated: boolean;
}) {
	if (!build) return null;
	const commit = build.commit?.slice(0, 7);
	return (
		<footer className="border-t border-slate-200 px-4 py-3 text-xs text-slate-500 dark:border-slate-800 dark:text-slate-400">
			<div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-4 gap-y-1">
				{isRelease(build) ? (
					<a
						href={`${RELEASES}${build.version}`}
						target="_blank"
						rel="noreferrer"
						className="hover:underline"
					>
						{t("version.release", { version: build.version })}
					</a>
				) : (
					<span>
						{commit
							? t("version.buildWithCommit", { version: build.version, commit })
							: t("version.build", { version: build.version })}
					</span>
				)}
				{updated && (
					<p
						role="status"
						className="flex items-center gap-2 text-slate-700 dark:text-slate-200"
					>
						{t("version.updated")}
						<button
							type="button"
							onClick={() => window.location.reload()}
							className="rounded-md border border-slate-300 px-2 py-0.5 hover:bg-slate-100 dark:border-slate-600 dark:hover:bg-slate-800"
						>
							{t("version.reload")}
						</button>
					</p>
				)}
			</div>
		</footer>
	);
}
