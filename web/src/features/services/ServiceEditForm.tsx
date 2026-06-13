import type { PlaneResource } from "../../api/types";
import { planeLabel } from "../../lib/format";
import type { ServiceEditFormState } from "./forms";
import { ServiceWorkloadFields } from "./ServiceForm";

type Props = {
  form: ServiceEditFormState;
  planeID: string;
  planes: PlaneResource[];
  isSubmitting: boolean;
  onChange: (field: keyof ServiceEditFormState, value: string) => void;
  onSubmit: () => void;
};

export function ServiceEditForm({
  form,
  planeID,
  planes,
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
        <span>显示名</span>
        <input
          value={form.displayName}
          onChange={(event) => onChange("displayName", event.target.value)}
        />
      </label>
      <label>
        <span>Cloud plane</span>
        <input value={planeLabel(planes, planeID)} readOnly />
      </label>
      <ServiceWorkloadFields
        form={form}
        onChange={(patch) => {
          for (const [field, value] of Object.entries(patch)) {
            onChange(field as keyof ServiceEditFormState, value);
          }
        }}
      />
      <button type="submit" disabled={isSubmitting}>
        {isSubmitting ? "更新中..." : "更新服务"}
      </button>
    </form>
  );
}
