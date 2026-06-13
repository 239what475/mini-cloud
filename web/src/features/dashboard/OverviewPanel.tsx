import type { UseQueryResult } from "@tanstack/react-query";
import type { HealthzResponse, InventoryView } from "../../api/types";
import { percent } from "../../lib/format";

type Props = {
  hasAdminToken: boolean;
  healthQuery: UseQueryResult<HealthzResponse>;
  inventory?: InventoryView;
  inventoryError?: unknown;
};

export function OverviewPanel({
  hasAdminToken,
  healthQuery,
  inventory,
  inventoryError,
}: Props) {
  return (
    <section id="overview" className="panel">
      <div className="panel__header">
        <div>
          <p className="eyebrow">live status</p>
          <h2>平台概览</h2>
        </div>
      </div>

      <div className="status-grid">
        <div className="status-card">
          <span className="status-card__label">Healthz</span>
          <strong>{healthQuery.data?.status ?? "loading"}</strong>
          <p>{healthQuery.data?.service ?? "-"}</p>
        </div>
        <div className="status-card">
          <span className="status-card__label">Planes</span>
          <strong>{inventory?.summary.planesTotal ?? "-"}</strong>
          <p>
            ready {inventory?.summary.planesReady ?? "-"} / offline{" "}
            {inventory?.summary.planesOffline ?? "-"}
          </p>
        </div>
        <div className="status-card">
          <span className="status-card__label">Nodes</span>
          <strong>{inventory?.summary.nodesTotal ?? "-"}</strong>
          <p>
            ready {inventory?.summary.nodesReady ?? "-"} / unavailable{" "}
            {inventory?.summary.nodesUnavailable ?? "-"}
          </p>
        </div>
        <div className="status-card">
          <span className="status-card__label">Capacity</span>
          <strong>
            CPU{" "}
            {percent(
              inventory?.summary.cpuMilliAllocated ?? 0,
              inventory?.summary.cpuMilliCapacity ?? 0,
            )}
          </strong>
          <p>
            Memory{" "}
            {percent(
              inventory?.summary.memoryMiAllocated ?? 0,
              inventory?.summary.memoryMiCapacity ?? 0,
            )}
          </p>
        </div>
      </div>

      {healthQuery.error instanceof Error ? (
        <p className="error-text">{healthQuery.error.message}</p>
      ) : null}
      {!hasAdminToken ? (
        <div className="callout">
          <p>输入 admin token 后加载 cloud-plane、service 和 inventory。</p>
        </div>
      ) : null}
      {inventoryError instanceof Error ? (
        <p className="error-text">{inventoryError.message}</p>
      ) : null}
    </section>
  );
}
