export type Role = "rider" | "driver";
export type Category = "economy" | "comfort";
export type RideStatus = "requested" | "accepted" | "completed" | "cancelled";
export type DriverStatus = "offline" | "available" | "busy";

export type User = {
  id: string;
  email: string;
  display_name: string;
  role: Role;
};

export type SessionResponse = {
  user: User;
  csrf_token: string;
};

export type Location = {
  id: string;
  name: string;
  area: string;
  city: "Lahore";
};

export type QuoteInput = {
  pickup_location_id: string;
  destination_location_id: string;
  category: Category;
};

export type Quote = {
  estimated_fare_paisa: number;
  estimated_distance_meters: number;
  pickup_eta_minutes: number | null;
  currency: "PKR";
  approximate: true;
};

export type Assignment = {
  id: string;
  driver_id: string;
  driver_display_name: string;
  status: "active" | "completed" | "cancelled";
  assigned_at: string;
};

export type Ride = {
  id: string;
  rider: { id: string; display_name: string };
  pickup: Location;
  destination: Location;
  category: Category;
  estimated_fare_paisa: number;
  estimated_distance_meters: number;
  pickup_eta_minutes: number | null;
  currency: "PKR";
  approximate: true;
  status: RideStatus;
  assignment: Assignment | null;
  created_at: string;
  updated_at: string;
};

export type DriverRequest = {
  id: string;
  pickup: Location;
  destination: Location;
  category: Category;
  estimated_fare_paisa: number;
  estimated_distance_meters: number;
  currency: "PKR";
  status: "requested";
  created_at: string;
  pickup_distance_meters: number;
};

export type DriverRequestPage = {
  requests: DriverRequest[];
  next_cursor: string | null;
};

export type ApiErrorBody = {
  error?: {
    code?: string;
    message?: string;
    request_id?: string;
  };
};

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly requestId?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

type RequestOptions = {
  method?: "GET" | "POST" | "PATCH";
  body?: unknown;
  csrfToken?: string;
  idempotencyKey?: string;
  signal?: AbortSignal;
  timeoutMs?: number;
  public?: boolean;
};

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers({ Accept: "application/json" });
  if (options.body !== undefined) headers.set("Content-Type", "application/json");
  if (options.csrfToken) headers.set("X-CSRF-Token", options.csrfToken);
  if (options.idempotencyKey) headers.set("Idempotency-Key", options.idempotencyKey);

  const timeoutController = new AbortController();
  const abortFromCaller = () => timeoutController.abort(options.signal?.reason);
  if (options.signal?.aborted) abortFromCaller();
  else options.signal?.addEventListener("abort", abortFromCaller, { once: true });
  const timeout = window.setTimeout(() => {
    timeoutController.abort(new DOMException("Request timed out", "TimeoutError"));
  }, options.timeoutMs ?? 10_000);
  let response: Response;
  let payload: unknown;
  try {
    response = await fetch(path, {
      method: options.method ?? "GET",
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      credentials: "include",
      cache: "no-store",
      signal: timeoutController.signal,
    });
    if (response.status !== 204) {
      try {
        payload = await response.json();
      } catch {
        payload = undefined;
      }
    }
  } catch (error) {
    if (options.signal?.aborted) throw error;
    if (timeoutController.signal.reason instanceof DOMException && timeoutController.signal.reason.name === "TimeoutError") {
      throw new ApiError(0, "request_timeout", "The request took too long. Please retry.");
    }
    throw new ApiError(0, "network_error", "The service could not be reached. Check your connection and retry.");
  } finally {
    window.clearTimeout(timeout);
    options.signal?.removeEventListener("abort", abortFromCaller);
  }

  if (response.status === 204) return undefined as T;

  if (!response.ok) {
    const error = payload as ApiErrorBody | undefined;
    const code = error?.error?.code ?? "request_failed";
    const message = error?.error?.message ?? `The request failed (${response.status}).`;
    if (response.status === 401 && !options.public && typeof window !== "undefined") {
      window.dispatchEvent(new Event("ride:unauthorized"));
    }
    throw new ApiError(response.status, code, message, error?.error?.request_id);
  }

  return payload as T;
}

export const api = {
  me: (signal?: AbortSignal) => request<SessionResponse>("/api/v1/auth/me", { signal }),
  login: (email: string, password: string) =>
    request<SessionResponse>("/api/v1/auth/login", {
      method: "POST",
      body: { email, password },
      public: true,
    }),
  logout: (csrfToken: string) =>
    request<void>("/api/v1/auth/logout", { method: "POST", csrfToken }),
  locations: (signal?: AbortSignal) =>
    request<{ locations: Location[] }>("/api/v1/locations", { signal }),
  quote: (input: QuoteInput, csrfToken: string, signal?: AbortSignal) =>
    request<Quote>("/api/v1/quotes", { method: "POST", body: input, csrfToken, signal }),
  createRide: (input: QuoteInput, csrfToken: string, idempotencyKey: string) =>
    request<Ride>("/api/v1/rides", {
      method: "POST",
      body: input,
      csrfToken,
      idempotencyKey,
    }),
  currentRide: (signal?: AbortSignal) =>
    request<{ ride: Ride | null }>("/api/v1/rides/current", { signal }),
  ride: (id: string, signal?: AbortSignal) =>
    request<Ride>(`/api/v1/rides/${encodeURIComponent(id)}`, { signal }),
  cancelRide: (id: string, csrfToken: string) =>
    request<Ride>(`/api/v1/rides/${encodeURIComponent(id)}/cancel`, {
      method: "POST",
      csrfToken,
    }),
  driverRequests: (cursor?: string, signal?: AbortSignal) => {
    const query = new URLSearchParams({ limit: "25" });
    if (cursor) query.set("cursor", cursor);
    return request<DriverRequestPage>(`/api/v1/driver/requests?${query.toString()}`, { signal });
  },
  driverAssignment: (signal?: AbortSignal) =>
    request<{ assignment: { ride: Ride } | null }>("/api/v1/driver/assignment", { signal }),
  driverAvailability: (signal?: AbortSignal) =>
    request<{ status: DriverStatus }>("/api/v1/drivers/me/availability", { signal }),
  setDriverAvailability: (status: "offline" | "available", csrfToken: string) =>
    request<{ status: DriverStatus }>("/api/v1/drivers/me/availability", {
      method: "PATCH",
      body: { status },
      csrfToken,
    }),
  acceptRide: (id: string, csrfToken: string) =>
    request<Ride>(`/api/v1/rides/${encodeURIComponent(id)}/accept`, {
      method: "POST",
      csrfToken,
    }),
  completeRide: (id: string, csrfToken: string) =>
    request<Ride>(`/api/v1/rides/${encodeURIComponent(id)}/complete`, {
      method: "POST",
      csrfToken,
    }),
};
