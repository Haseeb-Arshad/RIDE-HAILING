import { render, screen } from "@testing-library/react";
import { afterAll, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
  vi.stubEnv("NEXT_PUBLIC_GOOGLE_MAPS_API_KEY", "browser-test-key");
  return {
    createMap: vi.fn(),
    mapClickHandler: null as null | ((event: { latLng: { lat: () => number; lng: () => number } | null }) => void),
  };
});

vi.mock("@googlemaps/js-api-loader", () => ({
  setOptions: vi.fn(),
  importLibrary: vi.fn(async () => ({
    Map: class {
      constructor(element: HTMLElement) { mocks.createMap(element); }
      addListener(_eventName: string, handler: typeof mocks.mapClickHandler extends infer T ? Exclude<T, null> : never) {
        mocks.mapClickHandler = handler;
        return { remove() {} };
      }
      setOptions() {}
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
  expect(mocks.createMap).toHaveBeenCalledWith(screen.getByLabelText("Map displaying route locations across Lahore"));
});

it("lets a rider select a point anywhere inside the Lahore service bounds", async () => {
  const onMapPointSelected = vi.fn();
  vi.stubGlobal("google", { maps: { LatLngBounds: class {} } });
  render(<GoogleRouteMap
    locations={[]}
    pickupID=""
    destinationID=""
    selectionTarget="pickup"
    onMapPointSelected={onMapPointSelected}
  />);
  await screen.findByText("API LOADED");
  mocks.mapClickHandler?.({ latLng: { lat: () => 31.63, lng: () => 74.57 } });
  expect(onMapPointSelected).toHaveBeenCalledWith({ latitude: 31.63, longitude: 74.57 });
});
