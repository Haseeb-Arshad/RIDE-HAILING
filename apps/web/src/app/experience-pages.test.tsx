import type { ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DriverPage from "@/app/driver/page";
import LoginPage from "@/app/login/page";
import RidePage from "@/app/rides/[id]/page";
import { ApiError, api, type DriverRequest, type Location, type Ride, type SessionResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth-provider";

const { router } = vi.hoisted(() => ({
  router: { push: vi.fn(), replace: vi.fn() },
}));

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  useParams: () => ({ id: "40000000-0000-4000-8000-000000000001" }),
}));

vi.mock("@/components/auth-guard", () => ({
  AuthGuard: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("@/lib/auth-provider", () => ({
  useAuth: vi.fn(),
  destinationFor: (role: "rider" | "driver") => role === "driver" ? "/driver" : "/rider",
}));

const pickup: Location = {
  id: "10000000-0000-4000-8000-000000000001",
  name: "Liberty Market",
  area: "Gulberg III",
  city: "Lahore",
};
const destination: Location = {
  id: "10000000-0000-4000-8000-000000000002",
  name: "MM Alam Road",
  area: "Gulberg III",
  city: "Lahore",
};
const riderSession: SessionResponse = {
  user: { id: "20000000-0000-4000-8000-000000000001", email: "rider@example.test", display_name: "Demo Rider", role: "rider" },
};
const driverSession: SessionResponse = {
  user: { id: "20000000-0000-4000-8000-000000000002", email: "driver.one@example.test", display_name: "Demo Driver One", role: "driver" },
};
const requestedRide: Ride = {
  id: "40000000-0000-4000-8000-000000000001",
  rider: { id: riderSession.user.id, display_name: riderSession.user.display_name },
  pickup,
  destination,
  category: "economy",
  estimated_fare_paisa: 23_676,
  estimated_distance_meters: 2_250,
  pickup_eta_minutes: null,
  currency: "PKR",
  approximate: true,
  status: "requested",
  assignment: null,
  created_at: "2026-11-01T10:00:00Z",
  updated_at: "2026-11-01T10:00:00Z",
};
const driverRequest: DriverRequest = {
  id: requestedRide.id,
  pickup,
  destination,
  category: "economy",
  estimated_fare_paisa: requestedRide.estimated_fare_paisa,
  estimated_distance_meters: requestedRide.estimated_distance_meters,
  currency: "PKR",
  status: "requested",
  created_at: requestedRide.created_at,
  pickup_distance_meters: 250,
};

function setSession(session: SessionResponse) {
  vi.mocked(useAuth).mockReturnValue({
    state: { status: "signed_in", session, message: null },
    signIn: vi.fn(),
    signOut: vi.fn(),
    refresh: vi.fn(),
  });
}

function mockDriverWorkspace(requests: DriverRequest[] = []) {
  vi.spyOn(api, "driverAvailability").mockResolvedValue({ status: "available" });
  vi.spyOn(api, "driverAssignment").mockResolvedValue({ assignment: null });
  vi.spyOn(api, "driverRequests").mockResolvedValue({ requests, next_cursor: null });
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useAuth).mockReturnValue({
    state: { status: "signed_out", session: null, message: null },
    signIn: vi.fn(),
    signOut: vi.fn(),
    refresh: vi.fn(),
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("account and driver pages", () => {
  it("shows a useful error when Supabase Auth rejects a demo login", async () => {
    const signIn = vi.fn().mockRejectedValue(new ApiError(401, "invalid_credentials", "Invalid credentials."));
    vi.mocked(useAuth).mockReturnValue({
      state: { status: "signed_out", session: null, message: null },
      signIn,
      signOut: vi.fn(),
      refresh: vi.fn(),
    });

    render(<LoginPage />);
    fireEvent.change(screen.getByLabelText("Demo password"), { target: { value: "wrong-password" } });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    expect((await screen.findByRole("alert")).textContent).toContain("don’t match an approved demo account");
    expect(signIn).toHaveBeenCalledWith("rider@example.test", "wrong-password");
  });

  it("routes a successful driver login to the driver workspace", async () => {
    const signIn = vi.fn().mockResolvedValue(driverSession);
    vi.mocked(useAuth).mockReturnValue({
      state: { status: "signed_out", session: null, message: null },
      signIn,
      signOut: vi.fn(),
      refresh: vi.fn(),
    });

    render(<LoginPage />);
    fireEvent.change(screen.getByLabelText("Demo account"), { target: { value: driverSession.user.email } });
    fireEvent.change(screen.getByLabelText("Demo password"), { target: { value: "seeded-password" } });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/driver"));
  });

  it("shows the available-driver empty state when there are no matching requests", async () => {
    setSession(driverSession);
    mockDriverWorkspace();

    render(<DriverPage />);

    expect(await screen.findByText("No matching requests right now")).toBeTruthy();
    expect(screen.getByText("Available")).toBeTruthy();
  });

  it("refreshes requests and explains an acceptance conflict", async () => {
    setSession(driverSession);
    vi.spyOn(api, "driverAvailability").mockResolvedValue({ status: "available" });
    vi.spyOn(api, "driverAssignment").mockResolvedValue({ assignment: null });
    vi.spyOn(api, "driverRequests")
      .mockResolvedValueOnce({ requests: [driverRequest], next_cursor: null })
      .mockResolvedValueOnce({ requests: [], next_cursor: null });
    vi.spyOn(api, "acceptRide").mockRejectedValue(new ApiError(409, "state_conflict", "The request was already accepted."));

    render(<DriverPage />);
    fireEvent.click(await screen.findByRole("button", { name: "Accept ride" }));

    expect(await screen.findByText(/request changed before it could be accepted/)).toBeTruthy();
    expect(await screen.findByText("No matching requests right now")).toBeTruthy();
    expect(api.acceptRide).toHaveBeenCalledWith(requestedRide.id);
  });
});

describe("rider ride page", () => {
  it("cancels an active ride after the rider confirms", async () => {
    setSession(riderSession);
    vi.spyOn(api, "ride").mockResolvedValue(requestedRide);
    vi.spyOn(api, "cancelRide").mockResolvedValue({ ...requestedRide, status: "cancelled" });
    vi.stubGlobal("confirm", vi.fn(() => true));

    render(<RidePage />);
    fireEvent.click(await screen.findByRole("button", { name: "Cancel ride" }));

    expect(await screen.findByText(/This ride is closed/)).toBeTruthy();
    expect(screen.getByText("Trip cancelled")).toBeTruthy();
    expect(api.cancelRide).toHaveBeenCalledWith(requestedRide.id);
  });

  it("shows completed rides as terminal and hides cancellation", async () => {
    setSession(riderSession);
    vi.spyOn(api, "ride").mockResolvedValue({ ...requestedRide, status: "completed" });

    render(<RidePage />);

    expect(await screen.findByText("Your trip is complete. Thanks for riding with us.")).toBeTruthy();
    expect(screen.getByText("Trip completed")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Cancel ride" })).toBeNull();
  });
});
