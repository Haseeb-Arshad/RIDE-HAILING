import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BookingForm } from "@/components/booking-form";
import { RideProgress, StatusPill } from "@/components/ride-status";
import type { Location, Quote, Ride } from "@/lib/api";

const pickup: Location = { id: "10000000-0000-4000-8000-000000000001", name: "Liberty Market", area: "Gulberg III", city: "Lahore" };
const destination: Location = { id: "10000000-0000-4000-8000-000000000002", name: "MM Alam Road", area: "Gulberg III", city: "Lahore" };
const quote: Quote = { estimated_fare_paisa: 23_676, estimated_distance_meters: 2250, pickup_eta_minutes: null, currency: "PKR", approximate: true };
const ride: Ride = {
  id: "40000000-0000-4000-8000-000000000001",
  rider: { id: "20000000-0000-4000-8000-000000000001", display_name: "Demo Rider" },
  pickup,
  destination,
  category: "economy",
  estimated_fare_paisa: quote.estimated_fare_paisa,
  estimated_distance_meters: quote.estimated_distance_meters,
  pickup_eta_minutes: null,
  currency: "PKR",
  approximate: true,
  status: "requested",
  assignment: null,
  created_at: "2026-11-01T10:00:00Z",
  updated_at: "2026-11-01T10:00:00Z",
};

let fetchMock: ReturnType<typeof vi.fn>;

function jsonResponse(payload: unknown, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => payload } as Response;
}

function chooseRoute() {
  fireEvent.change(screen.getByLabelText("Pickup"), { target: { value: pickup.id } });
  fireEvent.change(screen.getByLabelText("Destination"), { target: { value: destination.id } });
}

function renderForm(onRideCreated = vi.fn()) {
  render(<BookingForm locations={[pickup, destination]} ownerID="20000000-0000-4000-8000-000000000001" csrfToken="test-csrf" onRideCreated={onRideCreated} />);
  return onRideCreated;
}

beforeEach(() => {
  sessionStorage.clear();
  fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("crypto", { randomUUID: () => "00000000-0000-4000-8000-000000000001" });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("booking form", () => {
  it("blocks a missing or identical route and validates before requesting a quote", () => {
    renderForm();
    const review = screen.getByRole("button", { name: "Review fare estimate" }) as HTMLButtonElement;
    expect(review.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Pickup"), { target: { value: pickup.id } });
    fireEvent.change(screen.getByLabelText("Destination"), { target: { value: pickup.id } });
    expect(screen.getByRole("alert").textContent).toContain("must be different");
    expect(review.disabled).toBe(true);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows a pending state and disables quote review until the service responds", async () => {
    let resolveFetch: ((response: Response) => void) | undefined;
    fetchMock.mockImplementationOnce(() => new Promise<Response>((resolve) => { resolveFetch = resolve; }));
    renderForm();
    chooseRoute();
    fireEvent.click(screen.getByRole("button", { name: "Review fare estimate" }));

    expect(screen.getByRole("status").textContent).toContain("estimate");
    expect((screen.getByRole("button", { name: "Calculating estimate…" }) as HTMLButtonElement).disabled).toBe(true);
    resolveFetch?.(jsonResponse(quote));
    await screen.findByRole("button", { name: /request this ride/i });
    expect(screen.getByText("Pickup ETA unavailable")).toBeTruthy();
  });

  it("renders an API error banner when quote calculation fails", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: { code: "database_unavailable", message: "Ride data is temporarily unavailable." } }, 503));
    renderForm();
    chooseRoute();
    fireEvent.click(screen.getByRole("button", { name: "Review fare estimate" }));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "Ride data is temporarily unavailable.");
  });

  it("keeps a stable idempotency key across a lost response and disables duplicate submits", async () => {
    let rejectCreate: ((reason?: unknown) => void) | undefined;
    fetchMock
      .mockResolvedValueOnce(jsonResponse(quote))
      .mockImplementationOnce(() => new Promise<Response>((_resolve, reject) => { rejectCreate = reject; }))
      .mockResolvedValueOnce(jsonResponse({ ride: null }))
      .mockResolvedValueOnce(jsonResponse(ride, 201));
    const onRideCreated = renderForm();
    chooseRoute();
    fireEvent.click(screen.getByRole("button", { name: "Review fare estimate" }));
    await screen.findByRole("button", { name: /request this ride/i });

    fireEvent.click(screen.getByRole("button", { name: /request this ride/i }));
    const pendingButton = await screen.findByRole("button", { name: "Requesting ride…" }) as HTMLButtonElement;
    expect(pendingButton.disabled).toBe(true);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    fireEvent.click(pendingButton);
    expect(fetchMock).toHaveBeenCalledTimes(2);

    rejectCreate?.(new TypeError("offline"));
    await screen.findByRole("alert");
    expect(screen.getByRole("alert").textContent).toContain("same request key");
    fireEvent.click(screen.getByRole("button", { name: /request this ride/i }));
    await waitFor(() => expect(onRideCreated).toHaveBeenCalledWith(ride));

    const firstCreateHeaders = new Headers(fetchMock.mock.calls[1][1]?.headers);
    const retryCreateHeaders = new Headers(fetchMock.mock.calls[3][1]?.headers);
    expect(firstCreateHeaders.get("Idempotency-Key")).toBe("booking_00000000-0000-4000-8000-000000000001");
    expect(retryCreateHeaders.get("Idempotency-Key")).toBe(firstCreateHeaders.get("Idempotency-Key"));
    expect(firstCreateHeaders.get("X-CSRF-Token")).toBe("test-csrf");
  });

  it("reconciles an active-ride conflict by opening the server's current ride", async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse(quote))
      .mockResolvedValueOnce(jsonResponse({ error: { code: "open_ride_exists", message: "Finish or cancel your current ride before requesting another." } }, 409))
      .mockResolvedValueOnce(jsonResponse({ ride }));
    const onRideCreated = renderForm();
    chooseRoute();
    fireEvent.click(screen.getByRole("button", { name: "Review fare estimate" }));
    await screen.findByRole("button", { name: /request this ride/i });
    fireEvent.click(screen.getByRole("button", { name: /request this ride/i }));
    await waitFor(() => expect(onRideCreated).toHaveBeenCalledWith(ride));
    expect(fetchMock.mock.calls[2][0]).toBe("/api/v1/rides/current");
  });

  it("restores a pending attempt after refresh and reuses its key", async () => {
    const savedKey = "booking_saved-attempt-123";
    sessionStorage.setItem("ride-lahore.booking-attempt", JSON.stringify({
      key: savedKey,
      owner_id: "20000000-0000-4000-8000-000000000001",
      input: { pickup_location_id: pickup.id, destination_location_id: destination.id, category: "comfort" },
    }));
    fetchMock
      .mockResolvedValueOnce(jsonResponse(quote))
      .mockResolvedValueOnce(jsonResponse({ ...ride, category: "comfort" }, 201));
    const onRideCreated = renderForm();
    await waitFor(() => expect((screen.getByLabelText("Pickup") as HTMLSelectElement).value).toBe(pickup.id));
    expect((screen.getByLabelText("Destination") as HTMLSelectElement).value).toBe(destination.id);
    expect((screen.getByRole("radio", { name: /comfort/i }) as HTMLInputElement).checked).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Review fare estimate" }));
    await screen.findByRole("button", { name: /request this ride/i });
    fireEvent.click(screen.getByRole("button", { name: /request this ride/i }));
    await waitFor(() => expect(onRideCreated).toHaveBeenCalled());
    expect(new Headers(fetchMock.mock.calls[1][1]?.headers).get("Idempotency-Key")).toBe(savedKey);
  });
});

describe("ride status", () => {
  it.each([
    ["accepted", "Driver assigned"],
    ["completed", "Trip completed"],
  ] as const)("renders %s with its persisted label", (status, label) => {
    render(<><StatusPill status={status} /><RideProgress status={status} /></>);
    expect(screen.getAllByText(label).length).toBeGreaterThan(0);
    expect(screen.getByRole("list", { name: `Ride progress: ${label}` })).toBeTruthy();
  });
});
