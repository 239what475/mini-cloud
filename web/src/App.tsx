import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

type HealthzResponse = {
  service: string;
  status: string;
  database?: string;
  time: string;
};

type PlatformOverview = {
  servicesTotal: number;
  servicesDeploying: number;
  servicesRunning: number;
  servicesDegraded: number;
  servicesFailed: number;
  nodesTotal: number;
  nodesRegistering: number;
  nodesReady: number;
  nodesNotReady: number;
  nodesDraining: number;
  nodesOffline: number;
};

type ServiceSpec = {
  region: string;
  instanceClass: string;
  exposure: string;
  image: string;
  command: string[];
  args: string[];
  env: Record<string, string>;
  defaultPort: number;
  readinessPath: string;
  configSetID?: string;
  secretSetID?: string;
};

type ServiceRunStatus = {
  currentRunID?: string;
  latestRunID?: string;
  phase: string;
  message: string;
  lastObservedAt?: string;
};

type ServiceStatus = {
  observedGeneration: number;
  desiredState: string;
  phase: string;
  healthy: boolean;
  message: string;
  run: ServiceRunStatus;
};

type ServiceMetadata = {
  id: string;
  name: string;
  displayName: string;
  generation: number;
};

type ServiceResource = {
  metadata: ServiceMetadata;
  spec: ServiceSpec;
  status: ServiceStatus;
};

type ServiceListItem = {
  service: ServiceResource;
};

type ServiceListResponse = {
  items: ServiceListItem[];
};

type ServiceDetailResponse = {
  service: ServiceResource;
};

type ServiceMutationResponse = {
  service: ServiceResource;
};

type ServiceUpdateResponse = ServiceMutationResponse;

type ConfigSetResource = {
  id: string;
  name: string;
  values: Record<string, string>;
  createdAt: string;
  updatedAt: string;
};

type ConfigSetListResponse = {
  items: ConfigSetResource[];
};

type SecretSetResource = {
  id: string;
  name: string;
  keys: string[];
  createdAt: string;
  updatedAt: string;
};

type SecretSetListResponse = {
  items: SecretSetResource[];
};

type ConfigSetFormState = {
  name: string;
  valuesText: string;
};

type SecretSetFormState = {
  name: string;
  valuesText: string;
};

type ServiceFormState = {
  name: string;
  displayName: string;
  region: string;
  instanceClass: string;
  exposure: string;
  image: string;
  defaultPort: string;
  readinessPath: string;
  envText: string;
  configSetID: string;
  secretSetID: string;
};

type ServiceEditFormState = {
  displayName: string;
  region: string;
  instanceClass: string;
  exposure: string;
  image: string;
  defaultPort: string;
  readinessPath: string;
  envText: string;
  configSetID: string;
  secretSetID: string;
};

async function fetchJSON<T>(
  input: RequestInfo,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(input, {
    headers: {
      Accept: "application/json",
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...(init?.headers ?? {}),
    },
    ...init,
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

function defaultConfigSetForm(): ConfigSetFormState {
  return {
    name: "",
    valuesText: "APP_MODE=prod\nLOG_LEVEL=info",
  };
}

function defaultSecretSetForm(): SecretSetFormState {
  return {
    name: "",
    valuesText: "API_TOKEN=replace-me",
  };
}

function defaultCreateServiceForm(): ServiceFormState {
  return {
    name: "",
    displayName: "",
    region: "cn-beijing",
    instanceClass: "small",
    exposure: "public",
    image: "nginx:1.27-alpine",
    defaultPort: "8080",
    readinessPath: "/healthz",
    envText: "PORT=8080",
    configSetID: "",
    secretSetID: "",
  };
}

function defaultEditServiceForm(): ServiceEditFormState {
  return {
    displayName: "",
    region: "cn-beijing",
    instanceClass: "small",
    exposure: "public",
    image: "nginx:1.27-alpine",
    defaultPort: "8080",
    readinessPath: "/healthz",
    envText: "",
    configSetID: "",
    secretSetID: "",
  };
}

function editFormFromService(service: ServiceResource): ServiceEditFormState {
  return {
    displayName: service.metadata.displayName,
    region: service.spec.region,
    instanceClass: service.spec.instanceClass,
    exposure: service.spec.exposure,
    image: service.spec.image,
    defaultPort: String(service.spec.defaultPort),
    readinessPath: service.spec.readinessPath,
    envText: stringifyKeyValueMap(service.spec.env),
    configSetID: service.spec.configSetID ?? "",
    secretSetID: service.spec.secretSetID ?? "",
  };
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

function toCreateServicePayload(form: ServiceFormState) {
  return {
    name: form.name.trim(),
    displayName: form.displayName.trim(),
    spec: {
      region: form.region.trim(),
      instanceClass: form.instanceClass,
      exposure: form.exposure,
      image: form.image.trim(),
      defaultPort: parseIntegerField("容器端口", form.defaultPort, {
        min: 1,
        max: 65535,
      }),
      readinessPath: form.readinessPath.trim(),
      env: parseKeyValueText(form.envText),
      configSetID: form.configSetID.trim(),
      secretSetID: form.secretSetID.trim(),
    },
  };
}

function toUpdateServicePayload(form: ServiceEditFormState) {
  return {
    displayName: form.displayName.trim(),
    spec: {
      region: form.region.trim(),
      instanceClass: form.instanceClass,
      exposure: form.exposure,
      image: form.image.trim(),
      defaultPort: parseIntegerField("容器端口", form.defaultPort, {
        min: 1,
        max: 65535,
      }),
      readinessPath: form.readinessPath.trim(),
      env: parseKeyValueText(form.envText),
      configSetID: form.configSetID.trim(),
      secretSetID: form.secretSetID.trim(),
    },
  };
}

function formatTime(value?: string | null) {
  if (!value) {
    return "-";
  }
  return new Date(value).toLocaleString();
}

function App() {
  const queryClient = useQueryClient();

  const [selectedServiceID, setSelectedServiceID] = useState("");
  const [configSetForm, setConfigSetForm] = useState<ConfigSetFormState>(
    defaultConfigSetForm(),
  );
  const [secretSetForm, setSecretSetForm] = useState<SecretSetFormState>(
    defaultSecretSetForm(),
  );
  const [serviceForm, setServiceForm] = useState<ServiceFormState>(
    defaultCreateServiceForm(),
  );
  const [editForm, setEditForm] = useState<ServiceEditFormState>(
    defaultEditServiceForm(),
  );
  const [editFormSourceServiceID, setEditFormSourceServiceID] = useState("");
  const [isEditFormDirty, setIsEditFormDirty] = useState(false);

  const healthQuery = useQuery({
    queryKey: ["healthz"],
    queryFn: () => fetchJSON<HealthzResponse>("/api/healthz"),
    refetchInterval: 5_000,
  });

  const overviewQuery = useQuery({
    queryKey: ["platform-overview"],
    queryFn: () => fetchJSON<PlatformOverview>("/api/v1/platform/overview"),
    refetchInterval: 5_000,
  });

  const servicesQuery = useQuery({
    queryKey: ["services"],
    queryFn: () => fetchJSON<ServiceListResponse>("/api/v1/services"),
  });

  const configSetsQuery = useQuery({
    queryKey: ["config-sets"],
    queryFn: () => fetchJSON<ConfigSetListResponse>("/api/v1/config-sets"),
  });

  const secretSetsQuery = useQuery({
    queryKey: ["secret-sets"],
    queryFn: () => fetchJSON<SecretSetListResponse>("/api/v1/secret-sets"),
  });

  const serviceItems = servicesQuery.data?.items ?? [];
  const effectiveServiceID =
    selectedServiceID || serviceItems[0]?.service.metadata.id || "";

  const serviceDetailQuery = useQuery({
    queryKey: ["service", effectiveServiceID],
    queryFn: () =>
      fetchJSON<ServiceDetailResponse>(
        `/api/v1/services/${effectiveServiceID}`,
      ),
    enabled: effectiveServiceID !== "",
  });

  const currentService = serviceDetailQuery.data?.service ?? null;

  useEffect(() => {
    if (serviceItems.length === 0) {
      if (selectedServiceID !== "") {
        setSelectedServiceID("");
      }
      return;
    }
    if (selectedServiceID === "") {
      setSelectedServiceID(serviceItems[0].service.metadata.id);
      return;
    }
    if (
      !serviceItems.some(
        (item) => item.service.metadata.id === selectedServiceID,
      )
    ) {
      setSelectedServiceID(serviceItems[0].service.metadata.id);
    }
  }, [serviceItems, selectedServiceID]);

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

  const invalidateResourceArea = async (serviceID?: string) => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["platform-overview"] }),
      queryClient.invalidateQueries({ queryKey: ["services"] }),
      queryClient.invalidateQueries({ queryKey: ["config-sets"] }),
      queryClient.invalidateQueries({ queryKey: ["secret-sets"] }),
      serviceID
        ? queryClient.invalidateQueries({ queryKey: ["service", serviceID] })
        : Promise.resolve(),
    ]);
  };

  const createConfigSet = useMutation({
    mutationFn: (form: ConfigSetFormState) =>
      fetchJSON<ConfigSetResource>("/api/v1/config-sets", {
        method: "POST",
        body: JSON.stringify({
          name: form.name.trim(),
          values: parseKeyValueText(form.valuesText),
        }),
      }),
    onSuccess: async () => {
      setConfigSetForm(defaultConfigSetForm());
      await invalidateResourceArea();
    },
  });

  const createSecretSet = useMutation({
    mutationFn: (form: SecretSetFormState) =>
      fetchJSON<SecretSetResource>("/api/v1/secret-sets", {
        method: "POST",
        body: JSON.stringify({
          name: form.name.trim(),
          values: parseKeyValueText(form.valuesText),
        }),
      }),
    onSuccess: async () => {
      setSecretSetForm(defaultSecretSetForm());
      await invalidateResourceArea();
    },
  });

  const createService = useMutation({
    mutationFn: (form: ServiceFormState) =>
      fetchJSON<ServiceMutationResponse>("/api/v1/services", {
        method: "POST",
        body: JSON.stringify(toCreateServicePayload(form)),
      }),
    onSuccess: async (response) => {
      setServiceForm(defaultCreateServiceForm());
      setSelectedServiceID(response.service.metadata.id);
      await invalidateResourceArea(response.service.metadata.id);
    },
  });

  const updateService = useMutation({
    mutationFn: (input: { serviceID: string; form: ServiceEditFormState }) =>
      fetchJSON<ServiceUpdateResponse>(`/api/v1/services/${input.serviceID}`, {
        method: "PUT",
        body: JSON.stringify(toUpdateServicePayload(input.form)),
      }),
    onSuccess: async (response) => {
      queryClient.setQueryData<ServiceDetailResponse>(
        ["service", response.service.metadata.id],
        {
          service: response.service,
        },
      );
      setEditForm(editFormFromService(response.service));
      setEditFormSourceServiceID(response.service.metadata.id);
      setIsEditFormDirty(false);
      await invalidateResourceArea(response.service.metadata.id);
    },
  });

  const selectedServiceDetail = serviceDetailQuery.data ?? null;
  const selectedStatus = selectedServiceDetail?.service.status ?? null;

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
          <a href="#resources">资源</a>
          <a href="#services">服务</a>
          <a href="#detail">详情</a>
        </nav>
      </aside>

      <main className="content">
        <section className="hero">
          <p className="eyebrow">control-plane</p>
          <h1>service / resource / run / execution</h1>
          <p>
            control-plane 对外保留全局资源、服务和运行实例视图；cloud-plane
            只作为内部 gRPC 执行面。
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
              <p>
                {healthQuery.data?.database
                  ? `DB ${healthQuery.data.database}`
                  : "-"}
              </p>
            </div>
            <div className="status-card">
              <span className="status-card__label">Services</span>
              <strong>{overviewQuery.data?.servicesTotal ?? "-"}</strong>
              <p>
                running {overviewQuery.data?.servicesRunning ?? "-"} / failed{" "}
                {overviewQuery.data?.servicesFailed ?? "-"}
              </p>
            </div>
            <div className="status-card">
              <span className="status-card__label">Nodes</span>
              <strong>{overviewQuery.data?.nodesTotal ?? "-"}</strong>
              <p>
                ready {overviewQuery.data?.nodesReady ?? "-"} / offline{" "}
                {overviewQuery.data?.nodesOffline ?? "-"}
              </p>
            </div>
            <div className="status-card">
              <span className="status-card__label">Progressing</span>
              <strong>{overviewQuery.data?.servicesDeploying ?? "-"}</strong>
              <p>
                degraded {overviewQuery.data?.servicesDegraded ?? "-"} / failed{" "}
                {overviewQuery.data?.servicesFailed ?? "-"}
              </p>
            </div>
          </div>

          {healthQuery.error instanceof Error ? (
            <p className="error-text">{healthQuery.error.message}</p>
          ) : null}
          {overviewQuery.error instanceof Error ? (
            <p className="error-text">{overviewQuery.error.message}</p>
          ) : null}
        </section>

        <section id="resources" className="panel" style={{ marginTop: 24 }}>
          <div className="panel__header">
            <div>
              <p className="eyebrow">global resources</p>
              <h2>运行时资源</h2>
            </div>
          </div>

          <div className="app-grid" style={{ marginBottom: 24 }}>
            <article className="app-card">
              <div className="app-card__header">
                <div>
                  <strong>Config Sets</strong>
                  <p>全局非敏感运行配置</p>
                </div>
              </div>
              <form
                className="project-form"
                onSubmit={(event) => {
                  event.preventDefault();
                  createConfigSet.mutate(configSetForm);
                }}
              >
                <label>
                  <span>名称</span>
                  <input
                    value={configSetForm.name}
                    onChange={(event) =>
                      setConfigSetForm((current) => ({
                        ...current,
                        name: event.target.value,
                      }))
                    }
                    placeholder="web-config"
                  />
                </label>
                <label style={{ gridColumn: "1 / -1" }}>
                  <span>键值</span>
                  <textarea
                    rows={5}
                    value={configSetForm.valuesText}
                    onChange={(event) =>
                      setConfigSetForm((current) => ({
                        ...current,
                        valuesText: event.target.value,
                      }))
                    }
                  />
                </label>
                <button type="submit" disabled={createConfigSet.isPending}>
                  {createConfigSet.isPending ? "创建中..." : "创建 config set"}
                </button>
              </form>
              <div className="history-list">
                {(configSetsQuery.data?.items ?? []).map((item) => (
                  <div key={item.id} className="history-row">
                    <div>
                      <strong>{item.name}</strong>
                      <p>{item.id}</p>
                    </div>
                    <div>
                      <p>{Object.keys(item.values).length} keys</p>
                    </div>
                  </div>
                ))}
              </div>
              {createConfigSet.error instanceof Error ? (
                <p className="error-text">{createConfigSet.error.message}</p>
              ) : null}
            </article>

            <article className="app-card">
              <div className="app-card__header">
                <div>
                  <strong>Secret Sets</strong>
                  <p>全局敏感运行配置</p>
                </div>
              </div>
              <form
                className="project-form"
                onSubmit={(event) => {
                  event.preventDefault();
                  createSecretSet.mutate(secretSetForm);
                }}
              >
                <label>
                  <span>名称</span>
                  <input
                    value={secretSetForm.name}
                    onChange={(event) =>
                      setSecretSetForm((current) => ({
                        ...current,
                        name: event.target.value,
                      }))
                    }
                    placeholder="web-secrets"
                  />
                </label>
                <label style={{ gridColumn: "1 / -1" }}>
                  <span>键值</span>
                  <textarea
                    rows={5}
                    value={secretSetForm.valuesText}
                    onChange={(event) =>
                      setSecretSetForm((current) => ({
                        ...current,
                        valuesText: event.target.value,
                      }))
                    }
                  />
                </label>
                <button type="submit" disabled={createSecretSet.isPending}>
                  {createSecretSet.isPending ? "创建中..." : "创建 secret set"}
                </button>
              </form>
              <div className="history-list">
                {(secretSetsQuery.data?.items ?? []).map((item) => (
                  <div key={item.id} className="history-row">
                    <div>
                      <strong>{item.name}</strong>
                      <p>{item.id}</p>
                    </div>
                    <div>
                      <p>{item.keys.length} keys</p>
                    </div>
                  </div>
                ))}
              </div>
              {createSecretSet.error instanceof Error ? (
                <p className="error-text">{createSecretSet.error.message}</p>
              ) : null}
            </article>
          </div>
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
              <span>地域</span>
              <input
                value={serviceForm.region}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    region: event.target.value,
                  }))
                }
              />
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
              <span>Config Set</span>
              <select
                value={serviceForm.configSetID}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    configSetID: event.target.value,
                  }))
                }
              >
                <option value="">不使用</option>
                {(configSetsQuery.data?.items ?? []).map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>Secret Set</span>
              <select
                value={serviceForm.secretSetID}
                onChange={(event) =>
                  setServiceForm((current) => ({
                    ...current,
                    secretSetID: event.target.value,
                  }))
                }
              >
                <option value="">不使用</option>
                {(secretSetsQuery.data?.items ?? []).map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </select>
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
            <button type="submit" disabled={createService.isPending}>
              {createService.isPending ? "创建中..." : "创建服务"}
            </button>
          </form>

          {createService.error instanceof Error ? (
            <p className="error-text">{createService.error.message}</p>
          ) : null}

          <div className="app-grid">
            {serviceItems.map((item) => (
              <article key={item.service.metadata.id} className="app-card">
                <div className="app-card__header">
                  <div>
                    <strong>{item.service.metadata.displayName}</strong>
                    <p>
                      {item.service.metadata.name} · {item.service.status.phase}{" "}
                      ·{" "}
                      {item.service.status.healthy ? "healthy" : "not healthy"}
                    </p>
                  </div>
                  <button
                    className="inline-button"
                    type="button"
                    onClick={() =>
                      setSelectedServiceID(item.service.metadata.id)
                    }
                  >
                    {item.service.metadata.id === effectiveServiceID
                      ? "当前服务"
                      : "查看"}
                  </button>
                </div>
                <div className="app-card__section">
                  <p className="app-card__section-title">spec</p>
                  <p>
                    {item.service.spec.region} ·{" "}
                    {item.service.spec.instanceClass} ·{" "}
                    {item.service.spec.exposure}
                  </p>
                  <p>{item.service.spec.image}</p>
                  <p>
                    config {item.service.spec.configSetID || "-"} · secret{" "}
                    {item.service.spec.secretSetID || "-"}
                  </p>
                </div>
                <div className="app-card__section">
                  <p className="app-card__section-title">status</p>
                  <p>{item.service.status.message}</p>
                  <p>
                    current run {item.service.status.run.currentRunID ?? "-"}
                  </p>
                  <p>
                    latest run {item.service.status.run.latestRunID ?? "-"} ·{" "}
                    {item.service.status.run.phase}
                  </p>
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
                {selectedServiceDetail
                  ? `${selectedServiceDetail.service.metadata.displayName} 的 run / execution`
                  : "选择一个服务查看详情"}
              </h2>
            </div>
          </div>

          {selectedServiceDetail ? (
            <>
              <div className="status-grid">
                <div className="status-card">
                  <span className="status-card__label">Current run</span>
                  <strong>{selectedStatus?.run.currentRunID ?? "-"}</strong>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Latest run</span>
                  <strong>{selectedStatus?.run.latestRunID ?? "-"}</strong>
                  <p>{selectedStatus?.run.phase ?? "-"}</p>
                  <p>{selectedStatus?.run.message ?? "-"}</p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Phase</span>
                  <strong>{selectedStatus?.phase ?? "-"}</strong>
                  <p>{selectedStatus?.message ?? "-"}</p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Generation</span>
                  <strong>
                    {selectedServiceDetail.service.metadata.generation}
                  </strong>
                  <p>observed {selectedStatus?.observedGeneration ?? 0}</p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Health</span>
                  <strong>
                    {selectedStatus?.healthy ? "healthy" : "not healthy"}
                  </strong>
                  <p>{selectedServiceDetail.service.spec.readinessPath}</p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Image</span>
                  <strong>{selectedServiceDetail.service.spec.image}</strong>
                  <p>
                    {selectedServiceDetail.service.spec.instanceClass} ·
                    {selectedServiceDetail.service.spec.exposure}
                  </p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Runtime Inputs</span>
                  <strong>
                    config{" "}
                    {selectedServiceDetail.service.spec.configSetID || "-"}
                  </strong>
                  <p>
                    secret{" "}
                    {selectedServiceDetail.service.spec.secretSetID || "-"}
                  </p>
                </div>
              </div>

              <form
                className="project-form"
                onSubmit={(event) => {
                  event.preventDefault();
                  updateService.mutate({
                    serviceID: selectedServiceDetail.service.metadata.id,
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
                  <span>地域</span>
                  <input
                    value={editForm.region}
                    onChange={(event) =>
                      updateEditFormField("region", event.target.value)
                    }
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
                  <span>Config Set</span>
                  <select
                    value={editForm.configSetID}
                    onChange={(event) =>
                      updateEditFormField("configSetID", event.target.value)
                    }
                  >
                    <option value="">不使用</option>
                    {(configSetsQuery.data?.items ?? []).map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  <span>Secret Set</span>
                  <select
                    value={editForm.secretSetID}
                    onChange={(event) =>
                      updateEditFormField("secretSetID", event.target.value)
                    }
                  >
                    <option value="">不使用</option>
                    {(secretSetsQuery.data?.items ?? []).map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name}
                      </option>
                    ))}
                  </select>
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
                  {updateService.isPending ? "更新中..." : "更新 service 规格"}
                </button>
              </form>

              {updateService.error instanceof Error ? (
                <p className="error-text">{updateService.error.message}</p>
              ) : null}
              {configSetsQuery.error instanceof Error ? (
                <p className="error-text">{configSetsQuery.error.message}</p>
              ) : null}
              {secretSetsQuery.error instanceof Error ? (
                <p className="error-text">{secretSetsQuery.error.message}</p>
              ) : null}

              <div className="app-card__section">
                <p className="app-card__section-title">run</p>
                <div className="history-list">
                  <div className="history-row">
                    <div>
                      <strong>{selectedStatus?.run.phase ?? "-"}</strong>
                      <p>{selectedStatus?.run.message ?? "-"}</p>
                    </div>
                    <span>
                      {selectedStatus?.run.lastObservedAt
                        ? formatTime(selectedStatus.run.lastObservedAt)
                        : "-"}
                    </span>
                  </div>
                </div>
              </div>

              <div className="app-card__section">
                <p className="app-card__section-title">run summary</p>
                <div className="history-list">
                  <div className="history-row">
                    <div>
                      <strong>{selectedStatus?.run.latestRunID ?? "-"}</strong>
                      <p>current {selectedStatus?.run.currentRunID ?? "-"}</p>
                    </div>
                    <span>{selectedStatus?.desiredState ?? "-"}</span>
                  </div>
                </div>
              </div>
            </>
          ) : (
            <div className="callout">
              <p>
                从项目列表里选一个 service，这里会展示当前 run、execution
                聚合和操作按钮。
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
