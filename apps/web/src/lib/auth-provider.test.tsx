import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider, useAuth } from "@/lib/auth-provider";
import { api, setApiAccessToken, type SessionResponse } from "@/lib/api";

const { supabase } = vi.hoisted(() => ({
  supabase: {
    auth: {
      getSession: vi.fn(),
      onAuthStateChange: vi.fn(),
      signInWithPassword: vi.fn(),
      signOut: vi.fn(),
    },
  },
}));

vi.mock("@/lib/supabase/client", () => ({ getSupabaseBrowserClient: () => supabase }));

const profile: SessionResponse = {
  user: { id: "20000000-0000-4000-8000-000000000001", email: "rider@example.test", display_name: "Demo Rider", role: "rider" },
};
const supabaseSession = {
  access_token: "signed-supabase-access-token",
  refresh_token: "refresh-token-never-sent-to-the-ride-api",
  user: { id: "30000000-0000-4000-8000-000000000001", user_metadata: { role: "driver" } },
};

function jsonResponse(payload: unknown, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => payload } as Response;
}

function SessionStatus() {
  const { state, signIn } = useAuth();
  return (
    <div>
      <span>{state.status}</span>
      <span>{state.status === "signed_in" ? state.session.user.role : ""}</span>
      <button type="button" onClick={() => { void api.locations().catch(() => undefined); }}>Call protected API</button>
      <button type="button" onClick={() => { void signIn("rider@example.test", "demo-password"); }}>Sign in</button>
    </div>
  );
}

beforeEach(() => {
  supabase.auth.getSession.mockReset().mockResolvedValue({ data: { session: supabaseSession }, error: null });
  supabase.auth.onAuthStateChange.mockReset().mockReturnValue({ data: { subscription: { unsubscribe: vi.fn() } } });
  supabase.auth.signInWithPassword.mockReset().mockResolvedValue({ data: { session: supabaseSession }, error: null });
  supabase.auth.signOut.mockReset().mockResolvedValue({ error: null });
  setApiAccessToken(null);
});

afterEach(() => {
  vi.unstubAllGlobals();
  setApiAccessToken(null);
});

describe("Supabase Auth integration", () => {
  it("loads the Go profile from a Supabase session and sends only its access token", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(profile));
    vi.stubGlobal("fetch", fetchMock);

    render(<AuthProvider><SessionStatus /></AuthProvider>);
    expect(await screen.findByText("signed_in")).toBeTruthy();
    expect(screen.getByText("rider")).toBeTruthy();

    const [url, options] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/auth/me");
    expect(new Headers(options.headers).get("Authorization")).toBe(`Bearer ${supabaseSession.access_token}`);
    expect(options.credentials).toBe("omit");
  });

  it("uses Supabase password sign-in and resolves authorization from the Go profile", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(profile));
    vi.stubGlobal("fetch", fetchMock);
    supabase.auth.getSession.mockResolvedValueOnce({ data: { session: null }, error: null });

    render(<AuthProvider><SessionStatus /></AuthProvider>);
    expect(await screen.findByText("signed_out")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByText("signed_in")).toBeTruthy();
    expect(supabase.auth.signInWithPassword).toHaveBeenCalledWith({ email: "rider@example.test", password: "demo-password" });
    expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get("Authorization")).toBe(`Bearer ${supabaseSession.access_token}`);
  });

  it("clears the browser session when the Go API rejects an expired access token", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(profile))
      .mockResolvedValueOnce(jsonResponse({ error: { code: "unauthenticated", message: "The Supabase access token is invalid or expired." } }, 401));
    vi.stubGlobal("fetch", fetchMock);

    render(<AuthProvider><SessionStatus /></AuthProvider>);
    expect(await screen.findByText("signed_in")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Call protected API" }));

    expect(await screen.findByText("signed_out")).toBeTruthy();
    expect(supabase.auth.signOut).toHaveBeenCalled();
  });

  it("does not retain an API bearer token for an unlinked profile", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: { code: "profile_not_found", message: "This account is not enabled." } }, 403))
      .mockResolvedValueOnce(jsonResponse({ locations: [] }));
    vi.stubGlobal("fetch", fetchMock);

    render(<AuthProvider><SessionStatus /></AuthProvider>);
    expect(await screen.findByText("unlinked")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Call protected API" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    const [, options] = fetchMock.mock.calls[1] as [string, RequestInit];
    expect(new Headers(options.headers).has("Authorization")).toBe(false);
  });
});
