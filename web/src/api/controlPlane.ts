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
  service: (serviceID: string) => ["service", serviceID] as const,
};

export function getHealthz(): Promise<HealthzResponse> {
  return fetchJSON<HealthzResponse>("/api/healthz");
}

export function getInventory(token: string): Promise<InventoryView> {
  return fetchJSON<InventoryView>(
    "/api/v1/control/inventory",
    undefined,
    token,
  );
}

export function listPlanes(token: string): Promise<PlaneListResponse> {
  return fetchJSON<PlaneListResponse>(
    "/api/v1/control/planes",
    undefined,
    token,
  );
}

export function listServices(token: string): Promise<ServiceListResponse> {
  return fetchJSON<ServiceListResponse>("/api/v1/services", undefined, token);
}

export function getService(
  token: string,
  serviceID: string,
): Promise<ServiceResource> {
  return fetchJSON<ServiceResource>(
    `/api/v1/services/${serviceID}`,
    undefined,
    token,
  );
}

export function createService(
  token: string,
  payload: unknown,
): Promise<ServiceResource> {
  return fetchJSON<ServiceResource>(
    "/api/v1/services",
    {
      method: "POST",
      body: JSON.stringify(payload),
    },
    token,
  );
}

export function updateService(
  token: string,
  serviceID: string,
  payload: unknown,
): Promise<ServiceResource> {
  return fetchJSON<ServiceResource>(
    `/api/v1/services/${serviceID}`,
    {
      method: "PUT",
      body: JSON.stringify(payload),
    },
    token,
  );
}

export function deleteService(
  token: string,
  serviceID: string,
): Promise<ServiceResource> {
  return fetchJSON<ServiceResource>(
    `/api/v1/services/${serviceID}`,
    { method: "DELETE" },
    token,
  );
}
