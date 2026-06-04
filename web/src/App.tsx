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
  servicesIdle: number;
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
  deploymentsTotal: number;
  deploymentsPending: number;
  deploymentsScheduling: number;
  deploymentsAssigned: number;
  deploymentsDeploying: number;
  deploymentsRunning: number;
  deploymentsFailed: number;
};

type ServiceSpec = {
  region: string;
  replicas: number;
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
  registryCredentialID?: string;
};

type RevisionSummary = {
  id: string;
  label: string;
};

type RolloutStatus = {
  phase: string;
  message: string;
  candidateRevision?: RevisionSummary;
};

type ServiceStatus = {
  phase: string;
  healthy: boolean;
  message: string;
  currentRevision?: RevisionSummary;
  rollout: RolloutStatus;
};

type ServiceResource = {
  id: string;
  name: string;
  displayName: string;
  spec: ServiceSpec;
  status: ServiceStatus;
  createdAt: string;
  updatedAt: string;
};

type RevisionResource = {
  id: string;
  serviceID: string;
  number: number;
  label: string;
  image: string;
  command: string[];
  args: string[];
  env: Record<string, string>;
  registryCredentialID: string;
  port: number;
  readinessPath: string;
  createdAt: string;
};

type DeploymentResource = {
  id: string;
  serviceID: string;
  revisionID: string;
  desiredReplicas: number;
  readyReplicas: number;
  availableReplicas: number;
  status: string;
  statusReason: string;
  createdAt: string;
  updatedAt: string;
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

type ServiceUpdateResponse = {
  service: ServiceResource;
  rolloutTriggered: boolean;
};

type RevisionListResponse = {
  items: RevisionResource[];
};

type DeploymentListResponse = {
  items: DeploymentResource[];
};

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

type RegistryCredentialResource = {
  id: string;
  name: string;
  server: string;
  username: string;
  passwordConfigured: boolean;
  createdAt: string;
  updatedAt: string;
};

type RegistryCredentialListResponse = {
  items: RegistryCredentialResource[];
};

type ConfigSetFormState = {
  name: string;
  valuesText: string;
};

type SecretSetFormState = {
  name: string;
  valuesText: string;
};

type RegistryCredentialFormState = {
  name: string;
  server: string;
  username: string;
  password: string;
};

type ServiceFormState = {
  name: string;
  displayName: string;
  region: string;
  replicas: string;
  instanceClass: string;
  exposure: string;
  image: string;
  defaultPort: string;
  readinessPath: string;
  envText: string;
  configSetID: string;
  secretSetID: string;
  registryCredentialID: string;
};

type ServiceEditFormState = {
  displayName: string;
  region: string;
  replicas: string;
  instanceClass: string;
  exposure: string;
  image: string;
  defaultPort: string;
  readinessPath: string;
  envText: string;
  configSetID: string;
  secretSetID: string;
  registryCredentialID: string;
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

function defaultRegistryCredentialForm(): RegistryCredentialFormState {
  return {
    name: "",
    server: "",
    username: "",
    password: "",
  };
}

function defaultCreateServiceForm(): ServiceFormState {
  return {
    name: "",
    displayName: "",
    region: "cn-beijing",
    replicas: "1",
    instanceClass: "small",
    exposure: "public",
    image: "nginx:1.27-alpine",
    defaultPort: "8080",
    readinessPath: "/healthz",
    envText: "PORT=8080",
    configSetID: "",
    secretSetID: "",
    registryCredentialID: "",
  };
}

function defaultEditServiceForm(): ServiceEditFormState {
  return {
    displayName: "",
    region: "cn-beijing",
    replicas: "1",
    instanceClass: "small",
    exposure: "public",
    image: "nginx:1.27-alpine",
    defaultPort: "8080",
    readinessPath: "/healthz",
    envText: "",
    configSetID: "",
    secretSetID: "",
    registryCredentialID: "",
  };
}

function editFormFromService(service: ServiceResource): ServiceEditFormState {
  return {
    displayName: service.displayName,
    region: service.spec.region,
    replicas: String(service.spec.replicas),
    instanceClass: service.spec.instanceClass,
    exposure: service.spec.exposure,
    image: service.spec.image,
    defaultPort: String(service.spec.defaultPort),
    readinessPath: service.spec.readinessPath,
    envText: stringifyKeyValueMap(service.spec.env),
    configSetID: service.spec.configSetID ?? "",
    secretSetID: service.spec.secretSetID ?? "",
    registryCredentialID: service.spec.registryCredentialID ?? "",
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
      replicas: parseIntegerField("副本数", form.replicas, { min: 1 }),
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
      registryCredentialID: form.registryCredentialID.trim(),
    },
  };
}

function toUpdateServicePayload(form: ServiceEditFormState) {
  return {
    displayName: form.displayName.trim(),
    spec: {
      region: form.region.trim(),
      replicas: parseIntegerField("副本数", form.replicas, { min: 1 }),
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
      registryCredentialID: form.registryCredentialID.trim(),
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
  const [registryCredentialForm, setRegistryCredentialForm] =
    useState<RegistryCredentialFormState>(defaultRegistryCredentialForm());
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

  const registryCredentialsQuery = useQuery({
    queryKey: ["registry-credentials"],
    queryFn: () =>
      fetchJSON<RegistryCredentialListResponse>(
        "/api/v1/registry-credentials",
      ),
  });

  const serviceItems = servicesQuery.data?.items ?? [];
  const effectiveServiceID =
    selectedServiceID || serviceItems[0]?.service.id || "";

  const serviceDetailQuery = useQuery({
    queryKey: ["service", effectiveServiceID],
    queryFn: () =>
      fetchJSON<ServiceDetailResponse>(
        `/api/v1/services/${effectiveServiceID}`,
      ),
    enabled: effectiveServiceID !== "",
  });

  const revisionsQuery = useQuery({
    queryKey: ["revisions", effectiveServiceID],
    queryFn: () =>
      fetchJSON<RevisionListResponse>(
        `/api/v1/services/${effectiveServiceID}/revisions`,
      ),
    enabled: effectiveServiceID !== "",
  });

  const deploymentsQuery = useQuery({
    queryKey: ["deployments", effectiveServiceID],
    queryFn: () =>
      fetchJSON<DeploymentListResponse>(
        `/api/v1/services/${effectiveServiceID}/deployments`,
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
      setSelectedServiceID(serviceItems[0].service.id);
      return;
    }
    if (!serviceItems.some((item) => item.service.id === selectedServiceID)) {
      setSelectedServiceID(serviceItems[0].service.id);
    }
  }, [serviceItems, selectedServiceID]);

  useEffect(() => {
    if (!currentService) {
      return;
    }
    const serviceChanged = currentService.id !== editFormSourceServiceID;
    if (!serviceChanged && isEditFormDirty) {
      return;
    }
    setEditForm(editFormFromService(currentService));
    setEditFormSourceServiceID(currentService.id);
    setIsEditFormDirty(false);
  }, [currentService, editFormSourceServiceID, isEditFormDirty]);

  const invalidateResourceArea = async (serviceID?: string) => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["platform-overview"] }),
      queryClient.invalidateQueries({ queryKey: ["services"] }),
      queryClient.invalidateQueries({ queryKey: ["config-sets"] }),
      queryClient.invalidateQueries({ queryKey: ["secret-sets"] }),
      queryClient.invalidateQueries({ queryKey: ["registry-credentials"] }),
      serviceID
        ? queryClient.invalidateQueries({ queryKey: ["service", serviceID] })
        : Promise.resolve(),
      serviceID
        ? queryClient.invalidateQueries({ queryKey: ["revisions", serviceID] })
        : Promise.resolve(),
      serviceID
        ? queryClient.invalidateQueries({
            queryKey: ["deployments", serviceID],
          })
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

  const createRegistryCredential = useMutation({
    mutationFn: (form: RegistryCredentialFormState) =>
      fetchJSON<RegistryCredentialResource>(
        "/api/v1/registry-credentials",
        {
          method: "POST",
          body: JSON.stringify({
            name: form.name.trim(),
            server: form.server.trim(),
            username: form.username.trim(),
            password: form.password,
          }),
        },
      ),
    onSuccess: async () => {
      setRegistryCredentialForm(defaultRegistryCredentialForm());
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
      setSelectedServiceID(response.service.id);
      await invalidateResourceArea(response.service.id);
    },
  });

  const updateService = useMutation({
    mutationFn: (input: { serviceID: string; form: ServiceEditFormState }) =>
      fetchJSON<ServiceUpdateResponse>(`/api/v1/services/${input.serviceID}`, {
        method: "PUT",
        body: JSON.stringify(toUpdateServicePayload(input.form)),
      }),
    onSuccess: async (response) => {
      queryClient.setQueryData<ServiceDetailResponse>(["service", response.service.id], {
        service: response.service,
      });
      setEditForm(editFormFromService(response.service));
      setEditFormSourceServiceID(response.service.id);
      setIsEditFormDirty(false);
      await invalidateResourceArea(response.service.id);
    },
  });

  const retryService = useMutation({
    mutationFn: (serviceID: string) =>
      fetchJSON<ServiceMutationResponse>(
        `/api/v1/services/${serviceID}/actions/retry`,
        {
          method: "POST",
        },
      ),
    onSuccess: async (response) => {
      await invalidateResourceArea(response.service.id);
    },
  });

  const selectedServiceDetail = serviceDetailQuery.data ?? null;
  const selectedStatus = selectedServiceDetail?.service.status ?? null;
  const revisions = revisionsQuery.data?.items ?? [];
  const deployments = deploymentsQuery.data?.items ?? [];

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
          <h1>service / resource / revision / deployment</h1>
          <p>
            control-plane 对外保留全局资源、服务、修订和部署视图；cloud-plane 只作为内部 gRPC 执行面。
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
              <span className="status-card__label">Deployments</span>
              <strong>{overviewQuery.data?.deploymentsTotal ?? "-"}</strong>
              <p>
                running {overviewQuery.data?.deploymentsRunning ?? "-"} / failed{" "}
                {overviewQuery.data?.deploymentsFailed ?? "-"}
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
                      {createConfigSet.isPending
                        ? "创建中..."
                        : "创建 config set"}
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
                    <p className="error-text">
                      {createConfigSet.error.message}
                    </p>
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
                      {createSecretSet.isPending
                        ? "创建中..."
                        : "创建 secret set"}
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
                    <p className="error-text">
                      {createSecretSet.error.message}
                    </p>
                  ) : null}
                </article>

                <article className="app-card">
                  <div className="app-card__header">
                    <div>
                      <strong>Registry Credentials</strong>
                      <p>私有镜像仓库访问凭据</p>
                    </div>
                  </div>
                  <form
                    className="project-form"
                    onSubmit={(event) => {
                      event.preventDefault();
                      createRegistryCredential.mutate(registryCredentialForm);
                    }}
                  >
                    <label>
                      <span>名称</span>
                      <input
                        value={registryCredentialForm.name}
                        onChange={(event) =>
                          setRegistryCredentialForm((current) => ({
                            ...current,
                            name: event.target.value,
                          }))
                        }
                        placeholder="acr-main"
                      />
                    </label>
                    <label>
                      <span>Registry</span>
                      <input
                        value={registryCredentialForm.server}
                        onChange={(event) =>
                          setRegistryCredentialForm((current) => ({
                            ...current,
                            server: event.target.value,
                          }))
                        }
                        placeholder="registry.example.com"
                      />
                    </label>
                    <label>
                      <span>用户名</span>
                      <input
                        value={registryCredentialForm.username}
                        onChange={(event) =>
                          setRegistryCredentialForm((current) => ({
                            ...current,
                            username: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <label>
                      <span>密码</span>
                      <input
                        type="password"
                        value={registryCredentialForm.password}
                        onChange={(event) =>
                          setRegistryCredentialForm((current) => ({
                            ...current,
                            password: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <button
                      type="submit"
                      disabled={createRegistryCredential.isPending}
                    >
                      {createRegistryCredential.isPending
                        ? "创建中..."
                        : "创建 registry credential"}
                    </button>
                  </form>
                  <div className="history-list">
                    {(registryCredentialsQuery.data?.items ?? []).map(
                      (item) => (
                        <div key={item.id} className="history-row">
                          <div>
                            <strong>{item.name}</strong>
                            <p>
                              {item.server} · {item.username}
                            </p>
                          </div>
                          <div>
                            <p>
                              {item.passwordConfigured ? "password set" : "-"}
                            </p>
                          </div>
                        </div>
                      ),
                    )}
                  </div>
                  {createRegistryCredential.error instanceof Error ? (
                    <p className="error-text">
                      {createRegistryCredential.error.message}
                    </p>
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
                  <span>副本数</span>
                  <input
                    type="number"
                    min={1}
                    step={1}
                    value={serviceForm.replicas}
                    onChange={(event) =>
                      setServiceForm((current) => ({
                        ...current,
                        replicas: event.target.value,
                      }))
                    }
                  />
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
                  <span>Registry Credential</span>
                  <select
                    value={serviceForm.registryCredentialID}
                    onChange={(event) =>
                      setServiceForm((current) => ({
                        ...current,
                        registryCredentialID: event.target.value,
                      }))
                    }
                  >
                    <option value="">不使用</option>
                    {(registryCredentialsQuery.data?.items ?? []).map(
                      (item) => (
                        <option key={item.id} value={item.id}>
                          {item.name}
                        </option>
                      ),
                    )}
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
                  <span>Inline Env</span>
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
                  <article key={item.service.id} className="app-card">
                    <div className="app-card__header">
                      <div>
                        <strong>{item.service.displayName}</strong>
                        <p>
                          {item.service.name} · {item.service.status.phase} ·{" "}
                          {item.service.status.healthy
                            ? "healthy"
                            : "not healthy"}
                        </p>
                      </div>
                      <button
                        className="inline-button"
                        type="button"
                        onClick={() => setSelectedServiceID(item.service.id)}
                      >
                        {item.service.id === effectiveServiceID
                          ? "当前服务"
                          : "查看"}
                      </button>
                    </div>
                    <div className="app-card__section">
                      <p className="app-card__section-title">spec</p>
                      <p>
                        {item.service.spec.region} ·{" "}
                        {item.service.spec.instanceClass} · replicas{" "}
                        {item.service.spec.replicas} ·{" "}
                        {item.service.spec.exposure}
                      </p>
                      <p>{item.service.spec.image}</p>
                      <p>
                        config {item.service.spec.configSetID || "-"} · secret{" "}
                        {item.service.spec.secretSetID || "-"}
                      </p>
                      <p>
                        registry {item.service.spec.registryCredentialID || "-"}
                      </p>
                    </div>
                    <div className="app-card__section">
                      <p className="app-card__section-title">status</p>
                      <p>{item.service.status.message}</p>
                      <p>
                        current revision{" "}
                        {item.service.status.currentRevision?.label ?? "-"}
                      </p>
                      <p>
                        rollout {item.service.status.rollout.phase} · candidate{" "}
                        {item.service.status.rollout.candidateRevision?.label ??
                          "-"}
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
                  ? `${selectedServiceDetail.service.displayName} 的 revision / deployment`
                  : "选择一个服务查看详情"}
              </h2>
            </div>
          </div>

          {selectedServiceDetail ? (
            <>
              <div className="status-grid">
                <div className="status-card">
                  <span className="status-card__label">Current revision</span>
                  <strong>
                    {selectedStatus?.currentRevision?.label ?? "-"}
                  </strong>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Rollout</span>
                  <strong>{selectedStatus?.rollout.phase ?? "-"}</strong>
                  <p>
                    candidate{" "}
                    {selectedStatus?.rollout.candidateRevision?.label ?? "-"}
                  </p>
                  <p>{selectedStatus?.rollout.message ?? "-"}</p>
                </div>
                <div className="status-card">
                  <span className="status-card__label">Phase</span>
                  <strong>{selectedStatus?.phase ?? "-"}</strong>
                  <p>{selectedStatus?.message ?? "-"}</p>
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
                    replicas {selectedServiceDetail.service.spec.replicas} ·{" "}
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
                  <p>
                    registry{" "}
                    {selectedServiceDetail.service.spec.registryCredentialID ||
                      "-"}
                  </p>
                </div>
              </div>

              <form
                className="project-form"
                onSubmit={(event) => {
                  event.preventDefault();
                  updateService.mutate({
                    serviceID: selectedServiceDetail.service.id,
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
                  <span>副本数</span>
                  <input
                    type="number"
                    min={1}
                    step={1}
                    value={editForm.replicas}
                    onChange={(event) =>
                      updateEditFormField("replicas", event.target.value)
                    }
                  />
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
                  <span>Registry Credential</span>
                  <select
                    value={editForm.registryCredentialID}
                    onChange={(event) =>
                      updateEditFormField(
                        "registryCredentialID",
                        event.target.value,
                      )
                    }
                  >
                    <option value="">不使用</option>
                    {(registryCredentialsQuery.data?.items ?? []).map(
                      (item) => (
                        <option key={item.id} value={item.id}>
                          {item.name}
                        </option>
                      ),
                    )}
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
                  <span>Inline Env</span>
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

              <div className="button-row">
                <button
                  className="inline-button"
                  type="button"
                  disabled={
                    retryService.isPending ||
                    (selectedStatus?.phase !== "failed" &&
                      selectedStatus?.rollout.phase !== "failed")
                  }
                  onClick={() =>
                    retryService.mutate(selectedServiceDetail.service.id)
                  }
                >
                  {retryService.isPending
                    ? "重试中..."
                    : "重试当前失败 revision"}
                </button>
              </div>

              {updateService.error instanceof Error ? (
                <p className="error-text">{updateService.error.message}</p>
              ) : null}
              {retryService.error instanceof Error ? (
                <p className="error-text">{retryService.error.message}</p>
              ) : null}
              {configSetsQuery.error instanceof Error ? (
                <p className="error-text">{configSetsQuery.error.message}</p>
              ) : null}
              {secretSetsQuery.error instanceof Error ? (
                <p className="error-text">{secretSetsQuery.error.message}</p>
              ) : null}
              {registryCredentialsQuery.error instanceof Error ? (
                <p className="error-text">
                  {registryCredentialsQuery.error.message}
                </p>
              ) : null}

              <div className="app-card__section">
                <p className="app-card__section-title">revisions</p>
                <div className="history-list">
                  {revisions.map((revision) => (
                    <div key={revision.id} className="history-row">
                      <div>
                        <strong>
                          {revision.label} · {revision.image}
                        </strong>
                        <p>
                          revision #{revision.number} · port {revision.port} ·{" "}
                          {formatTime(revision.createdAt)}
                        </p>
                      </div>
                      {selectedStatus?.currentRevision?.id === revision.id ? (
                        <span className="status-chip status-chip--ready">
                          当前 revision
                        </span>
                      ) : null}
                    </div>
                  ))}
                </div>
              </div>

              <div className="app-card__section">
                <p className="app-card__section-title">deployments</p>
                <div className="history-list">
                  {deployments.map((deployment) => (
                    <div key={deployment.id} className="history-row">
                      <div>
                        <strong>
                          {deployment.status} · revision {deployment.revisionID}
                        </strong>
                        <p>{deployment.statusReason || "-"}</p>
                        <p>
                          desired {deployment.desiredReplicas} · ready{" "}
                          {deployment.readyReplicas} · available{" "}
                          {deployment.availableReplicas}
                        </p>
                      </div>
                      <span>{formatTime(deployment.createdAt)}</span>
                    </div>
                  ))}
                </div>
              </div>
            </>
          ) : (
            <div className="callout">
              <p>
                从项目列表里选一个 service，这里会展示当前 revision、deployment
                和操作按钮。
              </p>
            </div>
          )}

          {serviceDetailQuery.error instanceof Error ? (
            <p className="error-text">{serviceDetailQuery.error.message}</p>
          ) : null}
          {revisionsQuery.error instanceof Error ? (
            <p className="error-text">{revisionsQuery.error.message}</p>
          ) : null}
          {deploymentsQuery.error instanceof Error ? (
            <p className="error-text">{deploymentsQuery.error.message}</p>
          ) : null}
        </section>
      </main>
    </div>
  );
}

export default App;
