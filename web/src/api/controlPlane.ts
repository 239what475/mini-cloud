import { fetchJSON } from "./client";
import type {
  HealthzResponse,
  InventoryView,
  PlaneListResponse,
  ServiceListResponse,
  ServiceResource,
} from "./types";

export const queryKeys = {
  healthz: ["healthz"] as const,
  inventory: ["control-inventory"] as const,
  planes: ["control-planes"] as const,
  services: ["services"] as const,
  service: (serviceID: string, planeID?: string) =>
    planeID
      ? (["service", serviceID, planeID] as const)
      : (["service", serviceID] as const),
};

export function getHealthz(): Promise<HealthzResponse> {
  return fetchJSON<HealthzResponse>("/api/healthz");
}

export function login(token: string): Promise<{ status: string }> {
  return fetchJSON<{ status: string }>("/api/v1/login", {
    method: "POST",
    body: JSON.stringify({ token }),
  });
}

export function logout(): Promise<{ status: string }> {
  return fetchJSON<{ status: string }>("/api/v1/logout", { method: "POST" });
}

export function getInventory(): Promise<InventoryView> {
  return fetchJSON<InventoryView>("/api/v1/control/inventory");
}

export function listPlanes(): Promise<PlaneListResponse> {
  return fetchJSON<PlaneListResponse>("/api/v1/control/planes");
}

export function listServices(): Promise<ServiceListResponse> {
  return fetchJSON<ServiceListResponse>("/api/v1/services");
}

export function getService(
  serviceID: string,
  planeID: string,
): Promise<ServiceResource> {
  return fetchJSON<ServiceResource>(
    `/api/v1/services/${serviceID}?planeID=${encodeURIComponent(planeID)}`,
  );
}

export function createService(payload: unknown): Promise<ServiceResource> {
  return fetchJSON<ServiceResource>("/api/v1/services", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function updateService(
  serviceID: string,
  planeID: string,
  payload: unknown,
): Promise<ServiceResource> {
  return fetchJSON<ServiceResource>(
    `/api/v1/services/${serviceID}?planeID=${encodeURIComponent(planeID)}`,
    {
      method: "PUT",
      body: JSON.stringify(payload),
    },
  );
}

export function deleteService(
  serviceID: string,
  planeID: string,
): Promise<ServiceResource> {
  return fetchJSON<ServiceResource>(
    `/api/v1/services/${serviceID}?planeID=${encodeURIComponent(planeID)}`,
    { method: "DELETE" },
  );
}
