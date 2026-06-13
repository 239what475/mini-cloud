import type { PlaneResource, ServiceResource } from "../../api/types";
import { formatTime, generationText } from "../../lib/format";
import type { ServiceEditFormState } from "./forms";
import { ServiceEditForm } from "./ServiceEditForm";

type Props = {
  service: ServiceResource | null;
  planes: PlaneResource[];
  form: ServiceEditFormState;
  hasAdminToken: boolean;
  isUpdating: boolean;
  isDeleting: boolean;
  updateError?: unknown;
  deleteError?: unknown;
  detailError?: unknown;
  onFormChange: (field: keyof ServiceEditFormState, value: string) => void;
  onUpdate: () => void;
  onDelete: () => void;
};

export function ServiceDetailPanel({
  service,
  planes,
  form,
  hasAdminToken,
  isUpdating,
  isDeleting,
  updateError,
  deleteError,
  detailError,
  onFormChange,
  onUpdate,
  onDelete,
}: Props) {
  const status = service?.status ?? null;

  return (
    <section id="detail" className="panel stacked-panel">
      <div className="panel__header">
        <div>
          <p className="eyebrow">detail</p>
          <h2>
            {service
              ? `${service.metadata.displayName} 的运行详情`
              : "选择一个服务查看详情"}
          </h2>
        </div>
      </div>

      {service ? (
        <>
          <div className="status-grid">
            <div className="status-card">
              <span className="status-card__label">Host</span>
              <strong>{service.metadata.host}</strong>
              <p>{service.spec.exposure}</p>
            </div>
            <div className="status-card">
              <span className="status-card__label">Phase</span>
              <strong>{status?.phase ?? "-"}</strong>
              <p>{status?.message ?? "-"}</p>
            </div>
            <div className="status-card">
              <span className="status-card__label">Generation</span>
              <strong>{generationText(service)}</strong>
              <p>observed / desired</p>
            </div>
            <div className="status-card">
              <span className="status-card__label">Run</span>
              <strong>{status?.run.phase ?? "-"}</strong>
              <p>{status?.run.message ?? "-"}</p>
            </div>
            <div className="status-card">
              <span className="status-card__label">Image</span>
              <strong>{service.spec.image}</strong>
              <p>
                {service.spec.instanceClass} · {service.spec.defaultPort}
              </p>
            </div>
            <div className="status-card">
              <span className="status-card__label">Last observed</span>
              <strong>{formatTime(status?.lastObservedAt)}</strong>
              <p>{service.spec.planeID}</p>
            </div>
          </div>

          <ServiceEditForm
            form={form}
            planeID={service.spec.planeID}
            planes={planes}
            isSubmitting={isUpdating}
            onChange={onFormChange}
            onSubmit={onUpdate}
          />

          <div className="button-row">
            <button
              className="inline-button"
              type="button"
              disabled={isDeleting || !hasAdminToken}
              onClick={onDelete}
            >
              {isDeleting ? "删除中..." : "删除服务"}
            </button>
          </div>

          {updateError instanceof Error ? (
            <p className="error-text">{updateError.message}</p>
          ) : null}
          {deleteError instanceof Error ? (
            <p className="error-text">{deleteError.message}</p>
          ) : null}

          <div className="app-card__section">
            <p className="app-card__section-title">run</p>
            <div className="history-list">
              <div className="history-row">
                <div>
                  <strong>{status?.run.phase ?? "-"}</strong>
                  <p>{status?.run.message ?? "-"}</p>
                </div>
                <span>{formatTime(status?.lastObservedAt)}</span>
              </div>
            </div>
          </div>
        </>
      ) : (
        <div className="callout">
          <p>从服务列表里选择一个 service，这里会展示运行状态和更新入口。</p>
        </div>
      )}

      {detailError instanceof Error ? (
        <p className="error-text">{detailError.message}</p>
      ) : null}
    </section>
  );
}
