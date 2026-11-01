import { render, screen } from "@testing-library/react";
import { afterAll, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
  vi.stubEnv("NEXT_PUBLIC_GOOGLE_MAPS_API_KEY", "browser-test-key");
  return { createMap: vi.fn() };
});

vi.mock("@googlemaps/js-api-loader", () => ({
  setOptions: vi.fn(),
  importLibrary: vi.fn(async () => ({
    Map: class {
      constructor(element: HTMLElement) { mocks.createMap(element); }
      setCenter() {}
      setZoom() {}
    },
  })),
}));

import { GoogleRouteMap } from "@/components/google-route-map";

afterAll(() => { vi.unstubAllEnvs(); vi.unstubAllGlobals(); });

it("mounts the map canvas before the provider initializes", async () => {
  vi.stubGlobal("google", { maps: { LatLngBounds: class {} } });
  render(<GoogleRouteMap locations={[]} pickupID="" destinationID="" />);
  await screen.findByText("API LOADED");
  expect(mocks.createMap).toHaveBeenCalledWith(screen.getByLabelText("Map showing selected Lahore pickup points"));
});
