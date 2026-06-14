import { expect, test, type Page, type Request } from "@playwright/test";

const adminToken = "test-admin-token";
const plane = {
  id: "pln_test",
  name: "mini-cloud-test-plane",
  displayName: "Test Plane",
  provider: "tencent",
  region: "ap-guangzhou",
  grpcEndpoint: "10.0.0.10:18081",
  status: {
    status: "ready",
    message: "sync healthy: 1 nodes, 1 active runs",
    lastHeartbeatAt: "2026-06-13T08:00:00Z",
    lastSyncAt: "2026-06-13T08:00:00Z",
  },
};

const service = {
  metadata: {
    id: "svc_demo",
    name: "demo-web",
    displayName: "Demo Web",
    host: "demo-web.apps.example.com",
    generation: 1,
  },
  spec: {
    planeID: plane.id,
    instanceClass: "small",
    exposure: "public",
    image: "nginx:1.27-alpine",
    command: [],
    args: [],
    env: { FOO: "bar" },
    defaultPort: 80,
    readinessPath: "/",
  },
  status: {
    phase: "ready",
    message: "readiness check passed",
    observedGeneration: 1,
    lastObservedAt: "2026-06-13T08:01:00Z",
    run: {
      phase: "running",
      message: "container running",
    },
    frontDoor: {
      cname: "demo-web.apps.example.com.cdn.dnsv1.com",
    },
  },
};

type ServiceFixture = typeof service;
type RecordedRequest = {
  method: string;
  url: string;
  body: unknown;
};

test.setTimeout(60_000);

test("creates, reads, updates, and deletes services through the HTTP API contract", async ({
  page,
}) => {
  const requests: RecordedRequest[] = [];
  let serviceItems: ServiceFixture[] = [service];

  await installControlPlaneMocks(
    page,
    requests,
    () => serviceItems,
    (items) => {
      serviceItems = items;
    },
  );

  await page.goto("/");
  await page.getByLabel("Admin token").fill(adminToken);

  await expect(
    page.getByRole("article").filter({ hasText: "Test Plane" }),
  ).toBeVisible();
  await expect(
    page.getByRole("article").filter({ hasText: "Demo Web" }),
  ).toBeVisible();

  await page.getByLabel("服务名").fill("new-web");
  await page.getByLabel("显示名").first().fill("New Web");
  await page.getByLabel("镜像").first().fill("nginx:1.27-alpine");
  await page.getByLabel("Env").first().fill("HELLO=world");
  await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().endsWith("/api/v1/services"),
    ),
    page.getByRole("button", { name: "创建服务" }).click(),
  ]);

  await expect
    .poll(() => requests.find((request) => request.method === "POST"))
    .toBeTruthy();
  const createRequest = requests.find((request) => request.method === "POST");
  expect(createRequest?.url).toBe("/api/v1/services");
  expect(createRequest?.body).toMatchObject({
    name: "new-web",
    displayName: "New Web",
    spec: {
      planeID: plane.id,
      image: "nginx:1.27-alpine",
      defaultPort: 80,
      env: { HELLO: "world" },
    },
  });

  await expect(
    page.getByRole("article").filter({ hasText: "New Web" }),
  ).toBeVisible({ timeout: 15_000 });

  await page
    .getByRole("article")
    .filter({ hasText: "New Web" })
    .getByRole("button", { name: /查看|当前服务/ })
    .click();
  await expect(page.getByText("New Web 的运行详情")).toBeVisible({
    timeout: 15_000,
  });

  await page.getByLabel("镜像").last().fill("nginx:1.28-alpine");
  await page.getByRole("button", { name: "更新服务" }).click();
  await expect
    .poll(() =>
      requests.find(
        (request) =>
          request.method === "PUT" &&
          request.url === `/api/v1/services/svc_new?planeID=${plane.id}`,
      ),
    )
    .toBeTruthy();
  const updateRequest = requests.find(
    (request) =>
      request.method === "PUT" &&
      request.url === `/api/v1/services/svc_new?planeID=${plane.id}`,
  );
  expect(updateRequest?.body).toMatchObject({
    displayName: "New Web",
    spec: {
      image: "nginx:1.28-alpine",
      defaultPort: 80,
    },
  });

  await page.getByRole("button", { name: "删除服务" }).click();
  await expect
    .poll(() =>
      requests.some(
        (request) =>
          request.method === "DELETE" &&
          request.url === `/api/v1/services/svc_new?planeID=${plane.id}`,
      ),
    )
    .toBe(true);
});

async function installControlPlaneMocks(
  page: Page,
  requests: RecordedRequest[],
  getServices: () => ServiceFixture[],
  setServices: (items: ServiceFixture[]) => void,
) {
  await page.route("**/api/healthz", async (route) => {
    await route.fulfill({
      status: 200,
      json: {
        service: "control-plane",
        status: "ok",
        time: "2026-06-13T08:00:00Z",
      },
    });
  });

  await page.route("**/api/v1/control/planes", async (route) => {
    await recordRequest(route.request(), requests);
    await route.fulfill({ status: 200, json: { items: [plane] } });
  });

  await page.route("**/api/v1/control/inventory", async (route) => {
    await recordRequest(route.request(), requests);
    await route.fulfill({
      status: 200,
      json: {
        summary: {
          planesTotal: 1,
          planesReady: 1,
          planesDegraded: 0,
          planesOffline: 0,
          nodesTotal: 1,
          nodesReady: 1,
          nodesUnavailable: 0,
          cpuMilliCapacity: 1800,
          cpuMilliAllocated: 500,
          cpuMilliFree: 1300,
          memoryMiCapacity: 2048,
          memoryMiAllocated: 512,
          memoryMiFree: 1536,
        },
        planes: [
          {
            id: plane.id,
            name: plane.name,
            displayName: plane.displayName,
            provider: plane.provider,
            region: plane.region,
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
    const request = route.request();
    await recordRequest(request, requests);
    if (request.method() === "POST") {
      const body = request.postDataJSON();
      const created: ServiceFixture = {
        ...service,
        metadata: {
          ...service.metadata,
          id: "svc_new",
          name: body.name,
          displayName: body.displayName,
          host: `${body.name}.apps.example.com`,
        },
        spec: body.spec,
        status: {
          phase: "pending",
          message: "accepted",
          observedGeneration: 1,
          lastObservedAt: "2026-06-13T08:02:00Z",
          run: { phase: "pending", message: "waiting for cloud-plane" },
          frontDoor: {},
        },
      };
      setServices([created, ...getServices()]);
      await route.fulfill({ status: 201, json: created });
      return;
    }
    await route.fulfill({ status: 200, json: { items: getServices() } });
  });

  await page.route("**/api/v1/services/*", async (route) => {
    const request = route.request();
    await recordRequest(request, requests);
    const url = new URL(request.url());
    const serviceID = url.pathname.split("/").at(-1);
    const item =
      getServices().find((candidate) => candidate.metadata.id === serviceID) ??
      service;

    if (request.method() === "PUT") {
      const body = request.postDataJSON();
      const updated: ServiceFixture = {
        ...item,
        metadata: {
          ...item.metadata,
          displayName: body.displayName,
          generation: item.metadata.generation + 1,
        },
        spec: {
          ...item.spec,
          ...body.spec,
          planeID: item.spec.planeID,
        },
        status: {
          ...item.status,
          phase: "pending",
          run: { phase: "pending", message: "waiting for update" },
        },
      };
      setServices(
        getServices().map((candidate) =>
          candidate.metadata.id === updated.metadata.id ? updated : candidate,
        ),
      );
      await route.fulfill({ status: 200, json: updated });
      return;
    }

    if (request.method() === "DELETE") {
      const deleting: ServiceFixture = {
        ...item,
        metadata: { ...item.metadata, generation: item.metadata.generation + 1 },
        status: {
          ...item.status,
          phase: "deleting",
          message: "service deletion requested",
          run: { phase: "pending", message: "waiting for cloud-plane cleanup" },
        },
      };
      setServices(
        getServices().map((candidate) =>
          candidate.metadata.id === deleting.metadata.id ? deleting : candidate,
        ),
      );
      await route.fulfill({ status: 200, json: deleting });
      return;
    }

    await route.fulfill({ status: 200, json: item });
  });
}

async function recordRequest(request: Request, requests: RecordedRequest[]) {
  const url = new URL(request.url());
  requests.push({
    method: request.method(),
    url: url.pathname + url.search,
    body: request.postData() ? request.postDataJSON() : null,
  });
}
