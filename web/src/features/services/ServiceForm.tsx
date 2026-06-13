import type { PlaneResource } from "../../api/types";
import type { ServiceFormState } from "./forms";

type Props = {
  form: ServiceFormState;
  planes: PlaneResource[];
  disabled: boolean;
  isSubmitting: boolean;
  onChange: (form: ServiceFormState) => void;
  onSubmit: () => void;
};

export function ServiceForm({
  form,
  planes,
  disabled,
  isSubmitting,
  onChange,
  onSubmit,
}: Props) {
  return (
    <form
      className="project-form"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <label>
        <span>服务名</span>
        <input
          value={form.name}
          onChange={(event) => onChange({ ...form, name: event.target.value })}
          placeholder="demo-web"
        />
      </label>
      <label>
        <span>显示名</span>
        <input
          value={form.displayName}
          onChange={(event) =>
            onChange({ ...form, displayName: event.target.value })
          }
          placeholder="Demo Web"
        />
      </label>
      <label>
        <span>Cloud plane</span>
        <select
          value={form.planeID}
          onChange={(event) =>
            onChange({ ...form, planeID: event.target.value })
          }
        >
          <option value="">请选择</option>
          {planes.map((plane) => (
            <option key={plane.id} value={plane.id}>
              {plane.displayName} ({plane.provider}/{plane.region})
            </option>
          ))}
        </select>
      </label>
      <ServiceWorkloadFields
        form={form}
        onChange={(patch) => onChange({ ...form, ...patch })}
      />
      <button type="submit" disabled={disabled || isSubmitting}>
        {isSubmitting ? "创建中..." : "创建服务"}
      </button>
    </form>
  );
}

type WorkloadForm = Omit<ServiceFormState, "name" | "displayName" | "planeID">;

export function ServiceWorkloadFields({
  form,
  onChange,
}: {
  form: WorkloadForm;
  onChange: (patch: Partial<WorkloadForm>) => void;
}) {
  return (
    <>
      <label>
        <span>规格档位</span>
        <select
          value={form.instanceClass}
          onChange={(event) => onChange({ instanceClass: event.target.value })}
        >
          <option value="small">small</option>
          <option value="medium">medium</option>
          <option value="large">large</option>
        </select>
      </label>
      <label>
        <span>暴露方式</span>
        <select
          value={form.exposure}
          onChange={(event) => onChange({ exposure: event.target.value })}
        >
          <option value="public">public</option>
          <option value="private">private</option>
        </select>
      </label>
      <label>
        <span>镜像</span>
        <input
          value={form.image}
          onChange={(event) => onChange({ image: event.target.value })}
          placeholder="nginx:1.27-alpine"
        />
      </label>
      <label>
        <span>容器端口</span>
        <input
          type="number"
          min={1}
          max={65535}
          step={1}
          value={form.defaultPort}
          onChange={(event) => onChange({ defaultPort: event.target.value })}
        />
      </label>
      <label>
        <span>健康检查路径</span>
        <input
          value={form.readinessPath}
          onChange={(event) => onChange({ readinessPath: event.target.value })}
          placeholder="/healthz"
        />
      </label>
      <label>
        <span>Command</span>
        <textarea
          rows={3}
          value={form.commandText}
          onChange={(event) => onChange({ commandText: event.target.value })}
        />
      </label>
      <label>
        <span>Args</span>
        <textarea
          rows={3}
          value={form.argsText}
          onChange={(event) => onChange({ argsText: event.target.value })}
        />
      </label>
      <label className="form-field--wide">
        <span>Env</span>
        <textarea
          rows={5}
          value={form.envText}
          onChange={(event) => onChange({ envText: event.target.value })}
        />
      </label>
    </>
  );
}
