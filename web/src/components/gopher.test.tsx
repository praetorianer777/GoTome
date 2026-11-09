import { act, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Celebration, celebrate } from "@/components/gopher";

afterEach(() => vi.useRealTimers());

it("shows the finished-book toast and takes it away by itself", () => {
	vi.useFakeTimers();
	render(<Celebration />);
	expect(screen.queryByRole("status")).toBeNull();
	act(() => celebrate());
	expect(screen.getByRole("status")).toHaveTextContent("Finished!");
	act(() => {
		vi.advanceTimersByTime(4100);
	});
	expect(screen.queryByRole("status")).toBeNull();
});
