import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, setApiAccessToken } from "@/lib/api";

function jsonResponse(payload: unknown, status: number) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => payload,
  } as Response;
}

beforeEach(() => setApiAccessToken(null));

afterEach(() => {
  vi.unstubAllGlobals();
  setApiAccessToken(null);
});

describe("API error responses", () => {
  it("turns object-valued error fields into a readable login-safe message", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({
      error: {
        code: { value: "gateway_error" },
        message: { detail: "upstream response object" },
        request_id: { value: "request-id" },
      },
    }, 502)));

    await expect(api.me()).rejects.toMatchObject({
      status: 502,
      code: "request_failed",
      message: "The request failed (502).",
      requestId: undefined,
    });
  });

  it("falls back when an unreadable message is passed to the API error", () => {
    const error = new ApiError(503, "upstream_error", { detail: "object" });

    expect(error.message).toBe("The service returned an unreadable error. Please try again.");
  });
});
