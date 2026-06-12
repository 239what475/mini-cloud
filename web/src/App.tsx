import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

type HealthzResponse = {
  service: string;
  status: string;
  time: string;
};

type InventorySummary = {
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

type InventoryPlane = {
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

type InventoryView = {
  summary: InventorySummary;
  planes: InventoryPlane[];
};

type PlaneResource = {
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

type PlaneListResponse = {
  items: PlaneResource[];
};

type ServiceSpec = {
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

type ServiceRunStatus = {
  phase: string;
  message?: string;
};

type ServiceStatus = {
  phase: string;
  message?: string;
  observedGeneration: number;
  lastObservedAt?: string;
  run: ServiceRunStatus;
};

type ServiceMetadata = {
  id: string;
  name: string;
  displayName: string;
  host: string;
  generation: number;
};

type ServiceResource = {
  metadata: ServiceMetadata;
  spec: ServiceSpec;
  status: ServiceStatus;
};

type ServiceListResponse = {
  items: ServiceResource[];
};

type ServiceFormState = {
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

type ServiceEditFormState = Omit<ServiceFormState, "name" | "planeID">;

async function fetchJSON<T>(
  input: RequestInfo,
  init?: RequestInit,
  token?: string,
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(init?.body ? { "Content-Type": "application/json" } : {}),
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };

  const response = await fetch(input, {
    ...init,
    headers: {
      ...headers,
      ...(init?.headers ?? {}),
    },
  });

  const payload = (await response.json().catch(() => null)) as Record<
    string,
    unknown
  > | null;

  if (!response.ok) {
    const message =
      typeof payload?.error === "string"
        ? payload.error
        : `${response.status} ${response.statusText}`;
    throw new Error(message);
  }

  return payload as T;
}

function defaultCreateServiceForm(): ServiceFormState {
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

function defaultEditServiceForm(): ServiceEditFormState {
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

function editFormFromService(service: ServiceResource): ServiceEditFormState {
  return {
    displayName: service.metadata.displayName,
    instanceClass: service.spec.instanceClass,
    exposure: service.spec.exposure,
    image: service.spec.image,
    commandText: service.spec.command.join("\n"),
    argsText: service.spec.args.join("\n"),
    defaultPort: String(service.spec.defaultPort),
    readinessPath: service.spec.readinessPath,
    envText: stringifyKeyValueMap(service.spec.env),
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

function toCreateServicePayload(form: ServiceFormState) {
  return {
    name: form.name.trim(),
    displayName: form.displayName.trim(),
    spec: serviceSpecPayload(form),
  };
}

function toUpdateServicePayload(form: ServiceEditFormState) {
  return {
    displayName: form.displayName.trim(),
    spec: workloadSpecPayload(form),
  };
}

function formatTime(value?: string | null) {
  if (!value) {
    return "-";
  }
  return new Date(value).toLocaleString();
}

function percent(used: number, total: number): string {
  if (total <= 0) {
    return "-";
  }
  return `${Math.round((used / total) * 100)}%`;
}

function phaseText(status?: ServiceStatus | null): string {
  if (!status) {
    return "-";
  }
  return `${status.phase} / ${status.run.phase}`;
}

function generationText(service?: ServiceResource | null): string {
  if (!service) {
    return "-";
  }
  return `gen ${service.status.observedGeneration}/${service.metadata.generation}`;
}

function planeLabel(planes: PlaneResource[], planeID: string): string {
  const plane = planes.find((item) => item.id === planeID);
  if (!plane) {
    return planeID;
  }
  return `${plane.displayName} (${plane.provider}/${plane.region})`;
}

function App() {
  const queryClient = useQueryClient();

  const [adminToken, setAdminToken] = useState(
    () => window.localStorage.getItem("mini-cloud-admin-token") ?? "",
  );
  const [selectedServiceID, setSelectedServiceID] = useState("");
  const [serviceForm, setServiceForm] = useState<ServiceFormState>(
    defaultCreateServiceForm(),
  );
  const [editForm, setEditForm] = useState<ServiceEditFormState>(
    defaultEditServiceForm(),
  );
  const [editFormSourceServiceID, setEditFormSourceServiceID] = useState("");
  const [isEditFormDirty, setIsEditFormDirty] = useState(false);
  const hasAdminToken = adminToken.trim() !== "";

  const healthQuery = useQuery({
    queryKey: ["healthz"],
    queryFn: () => fetchJSON<HealthzResponse>("/api/healthz"),
    refetchInterval: 5_000,
  });

  useEffect(() => {
    const token = adminToken.trim();
    if (token === "") {
      window.localStorage.removeItem("mini-cloud-admin-token");
      void queryClient.invalidateQueries({ queryKey: ["control-inventory"] });
      void queryClient.invalidateQueries({ queryKey: ["control-planes"] });
      void queryClient.invalidateQueries({ queryKey: ["services"] });
      return;
    }
    window.localStorage.setItem("mini-cloud-admin-token", token);
    void queryClient.invalidateQueries({ queryKey: ["control-inventory"] });
    void queryClient.invalidateQueries({ queryKey: ["control-planes"] });
    void queryClient.invalidateQueries({ queryKey: ["services"] });
  }, [queryClient, adminToken]);

  const inventoryQuery = useQuery({
    queryKey: ["control-inventory"],
    queryFn: () =>
      fetchJSON<InventoryView>(
        "/api/v1/control/inventory",
        undefined,
        adminToken,
      ),
    enabled: hasAdminToken,
    refetchInterval: 10_000,
  });

  const planesQuery = useQuery({
    queryKey: ["control-planes"],
    queryFn: () =>
      fetchJSON<PlaneListResponse>(
        "/api/v1/control/planes",
        undefined,
        adminToken,
      ),
    enabled: hasAdminToken,
    refetchInterval: 10_000,
  });

  const servicesQuery = useQuery({
    queryKey: ["services"],
    queryFn: () =>
      fetchJSON<ServiceListResponse>("/api/v1/services", undefined, adminToken),
    enabled: hasAdminToken,
    refetchInterval: 10_000,
  });

  const serviceItems = servicesQuery.data?.items ?? [];
  const effectiveServiceID =
    selectedServiceID || serviceItems[0]?.metadata.id || "";

  const serviceDetailQuery = useQuery({
    queryKey: ["service", effectiveServiceID],
    queryFn: () =>
      fetchJSON<ServiceResource>(
        `/api/v1/services/${effectiveServiceID}`,
        undefined,
        adminToken,
      ),
    enabled: effectiveServiceID !== "" && hasAdminToken,
    refetchInterval: 10_000,
  });

  const currentService = serviceDetailQuery.data ?? null;
  const planes = planesQuery.data?.items ?? [];
  const inventory = inventoryQuery.data;

  useEffect(() => {
    if (planes.length === 0 || serviceForm.planeID !== "") {
      return;
    }
    setServiceForm((current) => ({ ...current, planeID: planes[0].id }));
  }, [planes, serviceForm.planeID]);

  useEffect(() => {
    if (serviceItems.length === 0) {
      if (selectedServiceID !== "") {
        setSelectedServiceID("");
      }
      return;
    }
    if (selectedServiceID === "") {
      setSelectedServiceID(serviceItems[0].metadata.id);
      return;
    }
    if (!serviceItems.some((item) => item.metadata.id === selectedServiceID)) {
      queryClient.removeQueries({ queryKey: ["service", selectedServiceID] });
      setSelectedServiceID(serviceItems[0].metadata.id);
    }
  }, [queryClient, serviceItems, selectedServiceID]);

  useEffect(() => {
    if (!currentService) {
      return;
    }
    const serviceChanged =
      currentService.metadata.id !== editFormSourceServiceID;
    if (!serviceChanged && isEditFormDirty) {
      return;
    }
    setEditForm(editFormFromService(currentService));
    setEditFormSourceServiceID(currentService.metadata.id);
    setIsEditFormDirty(false);
  }, [currentService, editFormSourceServiceID, isEditFormDirty]);

  const invalidateServiceArea = async (serviceID?: string) => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["control-inventory"] }),
      queryClient.invalidateQueries({ queryKey: ["control-planes"] }),
      queryClient.invalidateQueries({ queryKey: ["services"] }),
      serviceID
        ? queryClient.invalidateQueries({ queryKey: ["service", serviceID] })
        : Promise.resolve(),
    ]);
  };

  const createService = useMutation({
    mutationFn: (form: ServiceFormState) =>
      fetchJSON<ServiceResource>(
        "/api/v1/services",
        {
          method: "POST",
          body: JSON.stringify(toCreateServicePayload(form)),
        },
        adminToken,
      ),
    onSuccess: async (response) => {
      setServiceForm((current) => ({
        ...defaultCreateServiceForm(),
        planeID: current.planeID,
      }));
      setSelectedServiceID(response.metadata.id);
      await invalidateServiceArea(response.metadata.id);
    },
  });

  const updateService = useMutation({
    mutationFn: (input: { serviceID: string; form: ServiceEditFormState }) =>
      fetchJSON<ServiceResource>(
        `/api/v1/services/${input.serviceID}`,
        {
          method: "PUT",
          body: JSON.stringify(toUpdateServicePayload(input.form)),
        },
        adminToken,
      ),
    onSuccess: async (response) => {
      queryClient.setQueryData<ServiceResource>(
        ["service", response.metadata.id],
        response,
      );
      setEditForm(editFormFromService(response));
      setEditFormSourceServiceID(response.metadata.id);
      setIsEditFormDirty(false);
      await invalidateServiceArea(response.metadata.id);
    },
  });

  const deleteService = useMutation({
    mutationFn: (serviceID: string) =>
      fetchJSON<ServiceResource>(
        `/api/v1/services/${serviceID}`,
        { method: "DELETE" },
        adminToken,
      ),
    onSuccess: async (response) => {
      queryClient.setQueryData<ServiceResource>(
        ["service", response.metadata.id],
        response,
      );
      await invalidateServiceArea(response.metadata.id);
    },
  });

  const selectedStatus = currentService?.status ?? null;

  const updateEditFormField = <K extends keyof ServiceEditFormState>(
    field: K,
    value: ServiceEditFormState[K],
  ) => {
    setEditForm((current) => ({
      ...current,
      [field]: value,
    }));
    setIsEditFormDirty(true);
  };

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <h1 className="brand">mini-cloud</h1>
        <nav className="nav">
          <a href="#overview">概览</a>
          <a href="#planes">cloud planes</a>
          <a href="#services">services</a>
          <a href="#detail">detail</a>
        </nav>
        <div className="auth-panel">
          <label>
            <span>Admin token</span>
            <input
              type="password"
              value={adminToken}
              onChange={(event) => setAdminToken(event.target.value)}
              placeholder="Bearer token"
            />
          </label>
        </div>
      </aside>

      <main className="content">
        <section className="hero">
          <p className="eyebrow">control-plane</p>
          <h1>CaaS 运维门户</h1>
          <p>
            通过 control-plane 创建 service、选择 cloud-plane、维护全局入口；
            运行态由目标 cloud-plane 自治闭环。
          </p>
        </section>

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
          {inventoryQuery.error instanceof Error ? (
            <p className="error-text">{inventoryQuery.error.message}</p>
          ) : null}
        </section>

        <section id="planes" className="panel" style={{ marginTop: 24 }}>
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

          {planesQuery.error instanceof Error ? (
            <p className="error-text">{planesQuery.error.message}</p>
          ) : null}
        </section>

        <section id="services" className="panel" style={{ marginTop: 24 }}>
          <div className="panel__header">
            <div>
              <p className="eyebrow">services</p>
              <h2>服务</h2>
            </div>
          </div>

          <form
            className="project-form"
            onSubmit={(event) => {
              event.preventDefault();
              createService.mutate(serviceForm);
            }}
          >
            <label>
              <span>服务名</span>
              <input
                value={serviceForm.name}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
                placeholder="demo-web"
              />
            </label>
            <label>
              <span>显示名</span>
              <input
                value={serviceForm.displayName}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    displayName: event.target.value,
                  }))
                }
                placeholder="Demo Web"
              />
            </label>
            <label>
              <span>Cloud plane</span>
              <select
                value={serviceForm.planeID}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    planeID: event.target.value,
                  }))
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
            <label>
              <span>规格档位</span>
              <select
                value={serviceForm.instanceClass}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    instanceClass: event.target.value,
                  }))
                }
              >
                <option value="small">small</option>
                <option value="medium">medium</option>
                <option value="large">large</option>
              </select>
            </label>
            <label>
              <span>暴露方式</span>
              <select
                value={serviceForm.exposure}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    exposure: event.target.value,
                  }))
                }
              >
                <option value="public">public</option>
                <option value="private">private</option>
              </select>
            </label>
            <label>
              <span>镜像</span>
              <input
                value={serviceForm.image}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    image: event.target.value,
                  }))
                }
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
                value={serviceForm.defaultPort}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    defaultPort: event.target.value,
                  }))
                }
              />
            </label>
            <label>
              <span>健康检查路径</span>
              <input
                value={serviceForm.readinessPath}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    readinessPath: event.target.value,
                  }))
                }
                placeholder="/healthz"
              />
            </label>
            <label>
              <span>Command</span>
              <textarea
                rows={3}
                value={serviceForm.commandText}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    commandText: event.target.value,
                  }))
                }
              />
            </label>
            <label>
              <span>Args</span>
              <textarea
                rows={3}
                value={serviceForm.argsText}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    argsText: event.target.value,
                  }))
                }
              />
            </label>
            <label style={{ gridColumn: "1 / -1" }}>
              <span>Env</span>
              <textarea
                rows={5}
                value={serviceForm.envText}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    envText: event.target.value,
                  }))
                }
              />
            </label>
            <button
              type="submit"
              disabled={createService.isPending || !hasAdminToken}
            >
              {createService.isPending ? "创建中..." : "创建服务"}
            </button>
          </form>

          {createService.error instanceof Error ? (
            <p className="error-text">{createService.error.message}</p>
          ) : null}

          <div className="app-grid">
            {serviceItems.map((service) => (
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
                    onClick={() => setSelectedServiceID(service.metadata.id)}
                  >
                    {service.metadata.id === effectiveServiceID
                      ? "当前服务"
                      : "查看"}
                  </button>
                </div>
                <div className="app-card__section">
                  <p className="app-card__section-title">spec</p>
                  <p>
                    plane {service.spec.planeID} · {service.spec.instanceClass}{" "}
                    · {service.spec.exposure}
                  </p>
                  <p>{service.spec.image}</p>
                  <p>
                    {service.metadata.host} :{service.spec.defaultPort}
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

          {servicesQuery.error instanceof Error ? (
            <p className="error-text">{servicesQuery.error.message}</p>
          ) : null}
        </section>

        <section id="detail" className="panel" style={{ marginTop: 24 }}>
          <div className="panel__header">
            <div>
              <p className="eyebrow">detail</p>
              <h2>
                {currentService
                  ? `${currentService.metadata.displayName} 的运行详情`
                  : "选择一个服务查看详情"}
              </h2>
            </div>
          </div>

          {currentService ? (
            <>
              <div className="status-grid">
                <div className="status-card">
                  <span className="status-card__label">Host</span>
                  <strong>{currentService.metadata.host}</strong>
                  <p>{currentService.spec.exposure}</p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Phase</span>
                  <strong>{selectedStatus?.phase ?? "-"}</strong>
                  <p>{selectedStatus?.message ?? "-"}</p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Generation</span>
                  <strong>{generationText(currentService)}</strong>
                  <p>observed / desired</p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Run</span>
                  <strong>{selectedStatus?.run.phase ?? "-"}</strong>
                  <p>{selectedStatus?.run.message ?? "-"}</p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Image</span>
                  <strong>{currentService.spec.image}</strong>
                  <p>
                    {currentService.spec.instanceClass} ·{" "}
                    {currentService.spec.defaultPort}
                  </p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Last observed</span>
                  <strong>{formatTime(selectedStatus?.lastObservedAt)}</strong>
                  <p>{currentService.spec.planeID}</p>
                </div>
              </div>

              <form
                className="project-form"
                onSubmit={(event) => {
                  event.preventDefault();
                  updateService.mutate({
                    serviceID: currentService.metadata.id,
                    form: editForm,
                  });
                }}
              >
                <label>
                  <span>显示名</span>
                  <input
                    value={editForm.displayName}
                    onChange={(event) =>
                      updateEditFormField("displayName", event.target.value)
                    }
                  />
                </label>
                <label>
                  <span>Cloud plane</span>
                  <input
                    value={planeLabel(planes, currentService.spec.planeID)}
                    readOnly
                  />
                </label>
                <label>
                  <span>规格档位</span>
                  <select
                    value={editForm.instanceClass}
                    onChange={(event) =>
                      updateEditFormField("instanceClass", event.target.value)
                    }
                  >
                    <option value="small">small</option>
                    <option value="medium">medium</option>
                    <option value="large">large</option>
                  </select>
                </label>
                <label>
                  <span>暴露方式</span>
                  <select
                    value={editForm.exposure}
                    onChange={(event) =>
                      updateEditFormField("exposure", event.target.value)
                    }
                  >
                    <option value="public">public</option>
                    <option value="private">private</option>
                  </select>
                </label>
                <label>
                  <span>镜像</span>
                  <input
                    value={editForm.image}
                    onChange={(event) =>
                      updateEditFormField("image", event.target.value)
                    }
                  />
                </label>
                <label>
                  <span>容器端口</span>
                  <input
                    type="number"
                    min={1}
                    max={65535}
                    step={1}
                    value={editForm.defaultPort}
                    onChange={(event) =>
                      updateEditFormField("defaultPort", event.target.value)
                    }
                  />
                </label>
                <label>
                  <span>健康检查路径</span>
                  <input
                    value={editForm.readinessPath}
                    onChange={(event) =>
                      updateEditFormField("readinessPath", event.target.value)
                    }
                  />
                </label>
                <label>
                  <span>Command</span>
                  <textarea
                    rows={3}
                    value={editForm.commandText}
                    onChange={(event) =>
                      updateEditFormField("commandText", event.target.value)
                    }
                  />
                </label>
                <label>
                  <span>Args</span>
                  <textarea
                    rows={3}
                    value={editForm.argsText}
                    onChange={(event) =>
                      updateEditFormField("argsText", event.target.value)
                    }
                  />
                </label>
                <label style={{ gridColumn: "1 / -1" }}>
                  <span>Env</span>
                  <textarea
                    rows={5}
                    value={editForm.envText}
                    onChange={(event) =>
                      updateEditFormField("envText", event.target.value)
                    }
                  />
                </label>
                <button type="submit" disabled={updateService.isPending}>
                  {updateService.isPending ? "更新中..." : "更新服务"}
                </button>
              </form>

              <div className="button-row">
                <button
                  className="inline-button"
                  type="button"
                  disabled={deleteService.isPending || !hasAdminToken}
                  onClick={() =>
                    deleteService.mutate(currentService.metadata.id)
                  }
                >
                  {deleteService.isPending ? "删除中..." : "删除服务"}
                </button>
              </div>

              {updateService.error instanceof Error ? (
                <p className="error-text">{updateService.error.message}</p>
              ) : null}
              {deleteService.error instanceof Error ? (
                <p className="error-text">{deleteService.error.message}</p>
              ) : null}

              <div className="app-card__section">
                <p className="app-card__section-title">run</p>
                <div className="history-list">
                  <div className="history-row">
                    <div>
                      <strong>{selectedStatus?.run.phase ?? "-"}</strong>
                      <p>{selectedStatus?.run.message ?? "-"}</p>
                    </div>
                    <span>{formatTime(selectedStatus?.lastObservedAt)}</span>
                  </div>
                </div>
              </div>
            </>
          ) : (
            <div className="callout">
              <p>
                从服务列表里选择一个 service，这里会展示运行状态和更新入口。
              </p>
            </div>
          )}

          {serviceDetailQuery.error instanceof Error ? (
            <p className="error-text">{serviceDetailQuery.error.message}</p>
          ) : null}
        </section>
      </main>
    </div>
  );
}

export default App;
