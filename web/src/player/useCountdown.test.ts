import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useCountdown } from "./useCountdown";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("useCountdown", () => {
  it("counts down and fires onExpire once at zero", () => {
    vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
    const deadline = new Date("2030-01-01T00:00:05Z"); // 5s out
    const onExpire = vi.fn();
    const { result } = renderHook(() => useCountdown(deadline, onExpire));
    expect(result.current).toBeGreaterThan(4000);

    act(() => { vi.advanceTimersByTime(5100); });
    expect(result.current).toBe(0);
    expect(onExpire).toHaveBeenCalledTimes(1);

    act(() => { vi.advanceTimersByTime(1000); });
    expect(onExpire).toHaveBeenCalledTimes(1); // fires exactly once
  });

  it("returns null for an untimed (null) deadline", () => {
    const { result } = renderHook(() => useCountdown(null));
    expect(result.current).toBeNull();
  });
});
