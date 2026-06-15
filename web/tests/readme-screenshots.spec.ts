import { expect, test, type Page } from "@playwright/test";

const outputDir = "../doc/assets";
const adminToken = "demo-admin-token";

const planes = [
  {
    id: "pln_tencent",
    name: "mini-cloud-tencent-ops",
    displayName: "Tencent cloud-plane",
    provider: "tencent",
    region: "ap-guangzhou",
    grpcEndpoint: "10.88.1.10:18081",
    status: {
      status: "ready",
      message: "sync healthy: 1 nodes, 1 active runs",
      lastHeartbeatAt: "2026-06-16T08:00:00Z",
      lastSyncAt: "2026-06-16T08:00:00Z",
    },
  },
  {
    id: "pln_aliyun",
    name: "mini-cloud-aliyun-ops",
    displayName: "Aliyun cloud-plane",
    provider: "aliyun",
    region: "cn-beijing",
    grpcEndpoint: "10.0.1.10:18081",
    status: {
      status: "ready",
      message: "sync healthy: 1 nodes, 1 active runs",
      lastHeartbeatAt: "2026-06-16T08:00:00Z",
      lastSyncAt: "2026-06-16T08:00:00Z",
    },
  },
];

const services = [
  {
    metadata: {
      id: "svc_web",
      name: "demo-web",
      displayName: "Demo Web",
      host: "demo-web.apps.example.com",
      generation: 1,
    },
    spec: {
      planeID: "pln_tencent",
      instanceClass: "small",
      exposure: "public",
      image: "nginx:1.27-alpine",
      command: [],
      args: [],
      env: { APP_ENV: "demo" },
      defaultPort: 80,
      readinessPath: "/",
    },
    status: {
      phase: "ready",
      message: "readiness check passed",
      observedGeneration: 1,
      lastObservedAt: "2026-06-16T08:04:00Z",
      run: {
        phase: "running",
        message: "container running on worker node",
      },
      frontDoor: {
        cname: "demo-web.apps.example.com.cdn.example.net",
      },
    },
  },
  {
    metadata: {
      id: "svc_api",
      name: "demo-api",
      displayName: "Demo API",
      host: "demo-api.apps.example.com",
      generation: 1,
    },
    spec: {
      planeID: "pln_aliyun",
      instanceClass: "small",
      exposure: "public",
      image: "hashicorp/http-echo:1.0",
      command: [],
      args: ["-text=hello from mini-cloud"],
      env: {},
      defaultPort: 5678,
      readinessPath: "/",
    },
    status: {
      phase: "ready",
      message: "readiness check passed",
      observedGeneration: 1,
      lastObservedAt: "2026-06-16T08:05:00Z",
      run: {
        phase: "running",
        message: "container running on worker node",
      },
      frontDoor: {
        cname: "demo-api.apps.example.com.w.kunlunsl.com",
      },
    },
  },
];

test("generates README screenshots", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await installMocks(page);

  await page.goto("/");
  await page.getByLabel("Admin token").fill(adminToken);
  await page.getByRole("button", { name: "登录" }).click();

  await expect(page.getByRole("heading", { name: "平台概览" })).toBeVisible();
  await expect(page.locator("#planes").getByText("Tencent cloud-plane")).toBeVisible();
  await expect(page.locator("#services").getByText("Demo Web")).toBeVisible();
  await page.screenshot({
    path: `${outputDir}/web-console-overview.png`,
  });

  const createForm = page
    .locator("form")
    .filter({ has: page.getByRole("button", { name: "创建服务" }) });
  await createForm.getByLabel("服务名").fill("new-service");
  await createForm.getByLabel("显示名").fill("New Service");
  await createForm.getByLabel("Cloud plane").selectOption("pln_aliyun");
  await createForm.getByLabel("镜像").fill("nginx:alpine");
  await page.locator("#services").screenshot({
    path: `${outputDir}/web-console-create-service.png`,
  });

  await page
    .getByRole("article")
    .filter({ hasText: "Demo API" })
    .getByRole("button", { name: /查看|当前服务/ })
    .click();
  await expect(page.getByText("Demo API 的运行详情")).toBeVisible();
  await page.locator("#detail").screenshot({
    path: `${outputDir}/web-console-service-detail.png`,
  });
});

async function installMocks(page: Page) {
  await page.route("**/api/healthz", async (route) => {
    await route.fulfill({
      status: 200,
      json: {
        service: "control-plane",
        status: "ok",
        time: "2026-06-16T08:00:00Z",
      },
    });
  });

  await page.route("**/api/v1/login", async (route) => {
    await route.fulfill({ status: 200, json: { ok: true } });
  });

  await page.route("**/api/v1/logout", async (route) => {
    await route.fulfill({ status: 200, json: { ok: true } });
  });

  await page.route("**/api/v1/control/planes", async (route) => {
    await route.fulfill({ status: 200, json: { items: planes } });
  });

  await page.route("**/api/v1/control/inventory", async (route) => {
    await route.fulfill({
      status: 200,
      json: {
        summary: {
          planesTotal: 2,
          planesReady: 2,
          planesDegraded: 0,
          planesOffline: 0,
          nodesTotal: 2,
          nodesReady: 2,
          nodesUnavailable: 0,
          cpuMilliCapacity: 3600,
          cpuMilliAllocated: 1000,
          cpuMilliFree: 2600,
          memoryMiCapacity: 4096,
          memoryMiAllocated: 1024,
          memoryMiFree: 3072,
        },
        planes: [
          {
            id: "pln_tencent",
            name: "mini-cloud-tencent-ops",
            displayName: "Tencent cloud-plane",
            provider: "tencent",
            region: "ap-guangzhou",
            status: "ready",
            statusMessage: "sync healthy",
            nodesTotal: 1,
            nodesReady: 1,
            nodesUnavailable: 0,
            cpuMilliCapacity: 1800,
            cpuMilliAllocated: 500,
            memoryMiCapacity: 2048,
            memoryMiAllocated: 512,
          },
          {
            id: "pln_aliyun",
            name: "mini-cloud-aliyun-ops",
            displayName: "Aliyun cloud-plane",
            provider: "aliyun",
            region: "cn-beijing",
            status: "ready",
            statusMessage: "sync healthy",
            nodesTotal: 1,
            nodesReady: 1,
            nodesUnavailable: 0,
            cpuMilliCapacity: 1800,
            cpuMilliAllocated: 500,
            memoryMiCapacity: 2048,
            memoryMiAllocated: 512,
          },
        ],
      },
    });
  });

  await page.route("**/api/v1/services", async (route) => {
    await route.fulfill({ status: 200, json: { items: services } });
  });

  await page.route("**/api/v1/services/*", async (route) => {
    const url = new URL(route.request().url());
    const serviceID = url.pathname.split("/").at(-1);
    const service =
      services.find((candidate) => candidate.metadata.id === serviceID) ??
      services[0];
    await route.fulfill({ status: 200, json: service });
  });
}
