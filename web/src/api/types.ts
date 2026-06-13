export type HealthzResponse = {
  service: string;
  status: string;
  time: string;
};

export type InventorySummary = {
  planesTotal: number;
  planesReady: number;
  planesDegraded: number;
  planesOffline: number;
  nodesTotal: number;
  nodesReady: number;
  nodesUnavailable: number;
  cpuMilliCapacity: number;
  cpuMilliAllocated: number;
  cpuMilliFree: number;
  memoryMiCapacity: number;
  memoryMiAllocated: number;
  memoryMiFree: number;
};

export type InventoryPlane = {
  id: string;
  name: string;
  displayName: string;
  provider: string;
  region: string;
  status: string;
  statusMessage: string;
  nodesTotal: number;
  nodesReady: number;
  nodesUnavailable: number;
  cpuMilliCapacity: number;
  cpuMilliAllocated: number;
  memoryMiCapacity: number;
  memoryMiAllocated: number;
  lastSyncAt?: string;
};

export type InventoryView = {
  summary: InventorySummary;
  planes: InventoryPlane[];
};

export type PlaneResource = {
  id: string;
  name: string;
  displayName: string;
  provider: string;
  region: string;
  grpcEndpoint: string;
  status: {
    status: string;
    message: string;
    lastHeartbeatAt?: string;
    lastSyncAt?: string;
  };
};

export type PlaneListResponse = {
  items: PlaneResource[];
};

export type ServiceSpec = {
  planeID: string;
  instanceClass: string;
  exposure: string;
  image: string;
  command: string[];
  args: string[];
  env: Record<string, string>;
  defaultPort: number;
  readinessPath: string;
};

export type ServiceRunStatus = {
  phase: string;
  message?: string;
};

export type ServiceStatus = {
  phase: string;
  message?: string;
  observedGeneration: number;
  lastObservedAt?: string;
  run: ServiceRunStatus;
};

export type ServiceMetadata = {
  id: string;
  name: string;
  displayName: string;
  host: string;
  generation: number;
};

export type ServiceResource = {
  metadata: ServiceMetadata;
  spec: ServiceSpec;
  status: ServiceStatus;
};

export type ServiceListResponse = {
  items: ServiceResource[];
};
