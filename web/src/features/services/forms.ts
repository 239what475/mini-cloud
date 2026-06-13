import type { ServiceResource } from "../../api/types";

export type ServiceFormState = {
  name: string;
  displayName: string;
  planeID: string;
  instanceClass: string;
  exposure: string;
  image: string;
  commandText: string;
  argsText: string;
  defaultPort: string;
  readinessPath: string;
  envText: string;
};

export type ServiceEditFormState = Omit<ServiceFormState, "name" | "planeID">;

export function defaultCreateServiceForm(): ServiceFormState {
  return {
    name: "",
    displayName: "",
    planeID: "",
    instanceClass: "small",
    exposure: "public",
    image: "nginx:1.27-alpine",
    commandText: "",
    argsText: "",
    defaultPort: "80",
    readinessPath: "/",
    envText: "",
  };
}

export function defaultEditServiceForm(): ServiceEditFormState {
  return {
    displayName: "",
    instanceClass: "small",
    exposure: "public",
    image: "nginx:1.27-alpine",
    commandText: "",
    argsText: "",
    defaultPort: "80",
    readinessPath: "/",
    envText: "",
  };
}

export function editFormFromService(
  service: ServiceResource,
): ServiceEditFormState {
  return {
    displayName: service.metadata.displayName,
    instanceClass: service.spec.instanceClass,
    exposure: service.spec.exposure,
    image: service.spec.image,
    commandText: (service.spec.command ?? []).join("\n"),
    argsText: (service.spec.args ?? []).join("\n"),
    defaultPort: String(service.spec.defaultPort),
    readinessPath: service.spec.readinessPath,
    envText: stringifyKeyValueMap(service.spec.env ?? {}),
  };
}

function parseLines(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "" && !line.startsWith("#"));
}

function parseKeyValueText(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  const invalidLineNumbers: number[] = [];
  for (const [index, rawLine] of text.split("\n").entries()) {
    const line = rawLine.trim();
    if (line === "" || line.startsWith("#")) {
      continue;
    }
    const separatorIndex = line.indexOf("=");
    if (separatorIndex <= 0) {
      invalidLineNumbers.push(index + 1);
      continue;
    }
    const key = line.slice(0, separatorIndex).trim();
    const value = line.slice(separatorIndex + 1).trim();
    if (key === "") {
      invalidLineNumbers.push(index + 1);
      continue;
    }
    out[key] = value;
  }
  if (invalidLineNumbers.length > 0) {
    throw new Error(
      `以下行不是合法的 KEY=VALUE 格式: ${invalidLineNumbers.join(", ")}`,
    );
  }
  return out;
}

function stringifyKeyValueMap(values: Record<string, string>): string {
  return Object.entries(values)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
}

function parseIntegerField(
  label: string,
  rawValue: string,
  options?: { min?: number; max?: number },
): number {
  const trimmed = rawValue.trim();
  if (trimmed === "") {
    throw new Error(`${label}不能为空`);
  }
  const value = Number(trimmed);
  if (!Number.isInteger(value)) {
    throw new Error(`${label}必须是整数`);
  }
  if (options?.min !== undefined && value < options.min) {
    throw new Error(`${label}必须大于等于 ${options.min}`);
  }
  if (options?.max !== undefined && value > options.max) {
    throw new Error(`${label}必须小于等于 ${options.max}`);
  }
  return value;
}

function serviceSpecPayload(form: ServiceFormState) {
  return {
    planeID: form.planeID.trim(),
    instanceClass: form.instanceClass,
    exposure: form.exposure,
    image: form.image.trim(),
    command: parseLines(form.commandText),
    args: parseLines(form.argsText),
    defaultPort: parseIntegerField("容器端口", form.defaultPort, {
      min: 1,
      max: 65535,
    }),
    readinessPath: form.readinessPath.trim(),
    env: parseKeyValueText(form.envText),
  };
}

function workloadSpecPayload(form: ServiceEditFormState) {
  return {
    instanceClass: form.instanceClass,
    exposure: form.exposure,
    image: form.image.trim(),
    command: parseLines(form.commandText),
    args: parseLines(form.argsText),
    defaultPort: parseIntegerField("容器端口", form.defaultPort, {
      min: 1,
      max: 65535,
    }),
    readinessPath: form.readinessPath.trim(),
    env: parseKeyValueText(form.envText),
  };
}

export function toCreateServicePayload(form: ServiceFormState) {
  return {
    name: form.name.trim(),
    displayName: form.displayName.trim(),
    spec: serviceSpecPayload(form),
  };
}

export function toUpdateServicePayload(form: ServiceEditFormState) {
  return {
    displayName: form.displayName.trim(),
    spec: workloadSpecPayload(form),
  };
}
