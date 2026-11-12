import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { flushQueue } from "./queue";

/** Sends the positions written offline when the app opens and whenever the
 * browser says it is back online. */
export function useOfflineSync() {
	const queryClient = useQueryClient();
	useEffect(() => {
		const send = () => void flushQueue(queryClient);
		send();
		window.addEventListener("online", send);
		return () => window.removeEventListener("online", send);
	}, [queryClient]);
}
