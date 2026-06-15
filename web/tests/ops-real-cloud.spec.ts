import { expect, test } from "@playwright/test";

const adminToken = requiredEnv("MINI_CLOUD_ADMIN_TOKEN");
const planeIDs = requiredEnv("MINI_CLOUD_E2E_PLANES")
  .split(",")
  .map((value) => value.trim())
  .filter(Boolean);
const baseDomain = requiredEnv("MINI_CLOUD_E2E_BASE_DOMAIN").replace(
  /^\.+|\.+$/g,
  "",
);
const e2eMode = process.env.MINI_CLOUD_E2E_MODE?.trim() || "full";

test.setTimeout(25 * 60 * 1000);

test("validates the real control-plane UI", async ({
  page,
}) => {
  if (planeIDs.length === 0) {
    throw new Error("MINI_CLOUD_E2E_PLANES must contain at least one plane id");
  }

  const serviceNames = planeIDs.map((planeID, index) =>
    serviceNameForPlane(planeID, index),
  );
  const createdServices: Array<{ name: string; host: string }> = [];

  await openControlPlane(page);
  await loginControlPlane(page);

  await waitForPlanesReady(page);

  if (e2eMode === "smoke") {
    await expectServicesAPI(page);
    return;
  }
  if (e2eMode !== "full") {
    throw new Error(`unsupported MINI_CLOUD_E2E_MODE ${e2eMode}`);
  }

  try {
    for (const [index, planeID] of planeIDs.entries()) {
      const serviceName = serviceNames[index];
      const host = `${serviceName}.${baseDomain}`;
      console.log(`[ops-e2e] create service ${serviceName} on ${planeID}`);
      await createService(page, {
        name: serviceName,
        planeID,
      });

      const card = page.getByRole("article").filter({ hasText: serviceName });
      await expect(card).toBeVisible({ timeout: 30_000 });
      createdServices.push({ name: serviceName, host });
      console.log(`[ops-e2e] wait service ${serviceName} ready`);
      await expect(card).toContainText("ready / running", {
        timeout: 12 * 60 * 1000,
      });
      console.log(`[ops-e2e] wait service ${serviceName} frontdoor`);
      await waitForPublicEntry(page, serviceName, planeID);

      console.log(`[ops-e2e] wait public HTTP 200 for ${host}`);
      await expectHTTP200(host);
    }
  } finally {
    for (const service of createdServices.reverse()) {
      console.log(`[ops-e2e] delete service ${service.name}`);
      await deleteService(page, service.name);
    }
  }
});

async function waitForPlanesReady(page: import("@playwright/test").Page) {
  for (const planeID of planeIDs) {
    await expect(
      page.getByLabel("Cloud plane").locator(`option[value="${planeID}"]`),
    ).toHaveCount(1, { timeout: 90_000 });
  }
  await expect(page.locator("#planes .status-pill--ready")).toHaveCount(
    planeIDs.length,
    {
      timeout: 90_000,
    },
  );
}

async function expectServicesAPI(page: import("@playwright/test").Page) {
  await expect
    .poll(
      async () => {
        const response = await page.request.get("/api/v1/services", {
          headers: { Authorization: `Bearer ${adminToken}` },
        });
        return response.status();
      },
      { timeout: 90_000, intervals: [5_000, 10_000] },
    )
    .toBe(200);
}

async function openControlPlane(page: import("@playwright/test").Page) {
  const deadline = Date.now() + 5 * 60 * 1000;
  let lastError: unknown;
  while (Date.now() < deadline) {
    try {
      await page.goto("/", { waitUntil: "domcontentloaded", timeout: 30_000 });
      await expect(page.getByLabel("Admin token")).toBeVisible({
        timeout: 10_000,
      });
      return;
    } catch (error) {
      lastError = error;
      await page.waitForTimeout(10_000);
    }
  }
  throw lastError instanceof Error
    ? lastError
    : new Error("timed out opening control-plane UI");
}

async function loginControlPlane(page: import("@playwright/test").Page) {
  await page.getByLabel("Admin token").fill(adminToken);
  await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().includes("/api/v1/login") &&
        response.ok(),
      { timeout: 30_000 },
    ),
    page.getByRole("button", { name: "登录" }).click(),
  ]);
}

async function createService(
  page: import("@playwright/test").Page,
  input: { name: string; planeID: string },
) {
  const form = page
    .locator("form")
    .filter({ has: page.getByRole("button", { name: "创建服务" }) });
  await form.getByLabel("服务名").fill(input.name);
  await form.getByLabel("显示名").fill(input.name);
  await form.getByLabel("Cloud plane").selectOption(input.planeID);
  await form.getByLabel("规格档位").selectOption("small");
  await form.getByLabel("暴露方式").selectOption("public");
  await form.getByLabel("镜像").fill("nginx:alpine");
  await form.getByLabel("容器端口").fill("80");
  await form.getByLabel("健康检查路径").fill("/");
  await form.getByRole("button", { name: "创建服务" }).click();
}

async function deleteService(
  page: import("@playwright/test").Page,
  serviceName: string,
) {
  const card = page.getByRole("article").filter({ hasText: serviceName });
  if ((await card.count()) === 0) {
    return;
  }
  const viewButton = card.getByRole("button", { name: /查看|当前服务/ });
  if ((await viewButton.count()) > 0) {
    await viewButton.first().click();
  }
  await expect(
    page.getByRole("heading", { name: new RegExp(`${serviceName} 的运行详情`) }),
  ).toBeVisible({ timeout: 30_000 });
  await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === "DELETE" &&
        response.url().includes("/api/v1/services/"),
      { timeout: 30_000 },
    ),
    page.getByRole("button", { name: "删除服务" }).click(),
  ]);
  await expect
    .poll(
      async () => {
        await page.reload({ waitUntil: "domcontentloaded" });
        await page.getByLabel("Admin token").fill(adminToken);
        return await page
          .getByRole("article")
          .filter({ hasText: serviceName })
          .count();
      },
      {
        timeout: 5 * 60 * 1000,
        intervals: [5_000, 10_000, 15_000],
      },
    )
    .toBe(0);
}

async function waitForPublicEntry(
  page: import("@playwright/test").Page,
  serviceName: string,
  planeID: string,
) {
  await expect
    .poll(
      async () => {
        const response = await page.request.get("/api/v1/services", {
          headers: { Authorization: `Bearer ${adminToken}` },
        });
        if (!response.ok()) {
          return "";
        }
        const payload = (await response.json()) as {
          items?: Array<{
            metadata?: { name?: string };
            spec?: { planeID?: string };
            status?: { frontDoor?: { cname?: string } };
          }>;
        };
        const service = payload.items?.find(
          (item) =>
            item.metadata?.name === serviceName &&
            item.spec?.planeID === planeID,
        );
        return service?.status?.frontDoor?.cname ?? "";
      },
      {
        timeout: 12 * 60 * 1000,
        intervals: [10_000, 15_000, 20_000],
      },
    )
    .not.toBe("");
}

async function expectHTTP200(host: string) {
  await expect
    .poll(
      async () => {
        try {
          const response = await fetch(`http://${host}`, {
            method: "GET",
            signal: AbortSignal.timeout(20_000),
          });
          return response.status;
        } catch {
          return 0;
        }
      },
      {
        timeout: 12 * 60 * 1000,
        intervals: [10_000, 15_000, 20_000],
      },
    )
    .toBe(200);
}

function serviceNameForPlane(planeID: string, index: number): string {
  const provider = planeID.includes("tencent") ? "tx" : "ali";
  return `e2e-ui-${provider}-${Date.now().toString(36)}-${index}`;
}

function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}
