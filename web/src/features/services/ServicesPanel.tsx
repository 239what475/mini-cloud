import type { PlaneResource, ServiceResource } from "../../api/types";
import { generationText, phaseText } from "../../lib/format";
import type { ServiceFormState } from "./forms";
import { ServiceForm } from "./ServiceForm";

type Props = {
  hasAdminToken: boolean;
  services: ServiceResource[];
  planes: PlaneResource[];
  selectedServiceID: string;
  form: ServiceFormState;
  isCreating: boolean;
  createError?: unknown;
  listError?: unknown;
  onFormChange: (form: ServiceFormState) => void;
  onCreate: () => void;
  onSelectService: (serviceID: string) => void;
};

export function ServicesPanel({
  hasAdminToken,
  services,
  planes,
  selectedServiceID,
  form,
  isCreating,
  createError,
  listError,
  onFormChange,
  onCreate,
  onSelectService,
}: Props) {
  return (
    <section id="services" className="panel stacked-panel">
      <div className="panel__header">
        <div>
          <p className="eyebrow">services</p>
          <h2>服务</h2>
        </div>
      </div>

      <ServiceForm
        form={form}
        planes={planes}
        disabled={!hasAdminToken}
        isSubmitting={isCreating}
        onChange={onFormChange}
        onSubmit={onCreate}
      />

      {createError instanceof Error ? (
        <p className="error-text">{createError.message}</p>
      ) : null}

      <div className="app-grid">
        {services.map((service) => (
          <article key={service.metadata.id} className="app-card">
            <div className="app-card__header">
              <div>
                <strong>{service.metadata.displayName}</strong>
                <p>
                  {service.metadata.name} · {phaseText(service.status)} ·{" "}
                  {generationText(service)}
                </p>
              </div>
              <button
                className="inline-button"
                type="button"
                onClick={() => onSelectService(service.metadata.id)}
              >
                {service.metadata.id === selectedServiceID
                  ? "当前服务"
                  : "查看"}
              </button>
            </div>
            <div className="app-card__section">
              <p className="app-card__section-title">spec</p>
              <p>
                plane {service.spec.planeID} · {service.spec.instanceClass} ·{" "}
                {service.spec.exposure}
              </p>
              <p>{service.spec.image}</p>
              <p>
                {service.metadata.host} :{service.spec.defaultPort}
              </p>
              <p>
                entry {service.status.frontDoor.cname ? "ready" : "pending"}
              </p>
            </div>
            <div className="app-card__section">
              <p className="app-card__section-title">status</p>
              <p>{service.status.message || "-"}</p>
              <p>run phase {service.status.run.phase}</p>
              <p>{generationText(service)}</p>
            </div>
          </article>
        ))}
      </div>

      {listError instanceof Error ? (
        <p className="error-text">{listError.message}</p>
      ) : null}
    </section>
  );
}
