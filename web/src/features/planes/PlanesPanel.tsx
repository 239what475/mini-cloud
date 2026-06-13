import type { InventoryView, PlaneResource } from "../../api/types";

type Props = {
  planes: PlaneResource[];
  inventory?: InventoryView;
  error?: unknown;
};

export function PlanesPanel({ planes, inventory, error }: Props) {
  return (
    <section id="planes" className="panel stacked-panel">
      <div className="panel__header">
        <div>
          <p className="eyebrow">cloud planes</p>
          <h2>运行面</h2>
        </div>
      </div>

      <div className="app-grid">
        {planes.map((plane) => {
          const planeInventory = inventory?.planes.find(
            (item) => item.id === plane.id,
          );
          return (
            <article key={plane.id} className="app-card">
              <div className="app-card__header">
                <div>
                  <strong>{plane.displayName}</strong>
                  <p>
                    {plane.name} · {plane.provider} · {plane.region}
                  </p>
                </div>
                <span
                  className={`status-pill status-pill--${plane.status.status}`}
                >
                  {plane.status.status}
                </span>
              </div>
              <div className="app-card__section">
                <p className="app-card__section-title">inventory</p>
                <p>
                  nodes {planeInventory?.nodesReady ?? 0}/
                  {planeInventory?.nodesTotal ?? 0}
                </p>
                <p>
                  cpu {planeInventory?.cpuMilliAllocated ?? 0}/
                  {planeInventory?.cpuMilliCapacity ?? 0}m · memory{" "}
                  {planeInventory?.memoryMiAllocated ?? 0}/
                  {planeInventory?.memoryMiCapacity ?? 0}Mi
                </p>
                <p>{plane.status.message}</p>
              </div>
            </article>
          );
        })}
      </div>

      {error instanceof Error ? (
        <p className="error-text">{error.message}</p>
      ) : null}
    </section>
  );
}
