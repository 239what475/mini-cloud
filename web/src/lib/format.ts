import type {
  PlaneResource,
  ServiceResource,
  ServiceStatus,
} from "../api/types";

export function formatTime(value?: string | null): string {
  if (!value) {
    return "-";
  }
  return new Date(value).toLocaleString();
}

export function percent(used: number, total: number): string {
  if (total <= 0) {
    return "-";
  }
  return `${Math.round((used / total) * 100)}%`;
}

export function phaseText(status?: ServiceStatus | null): string {
  if (!status) {
    return "-";
  }
  return `${status.phase} / ${status.run.phase}`;
}

export function generationText(service?: ServiceResource | null): string {
  if (!service) {
    return "-";
  }
  return `gen ${service.status.observedGeneration}/${service.metadata.generation}`;
}

export function planeLabel(planes: PlaneResource[], planeID: string): string {
  const plane = planes.find((item) => item.id === planeID);
  if (!plane) {
    return planeID;
  }
  return `${plane.displayName} (${plane.provider}/${plane.region})`;
}
