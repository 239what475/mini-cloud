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
const controlPlaneURLs = (
  process.env.MINI_CLOUD_CONTROL_PLANE_URLS?.trim() ||
  requiredEnv("PLAYWRIGHT_BASE_URL")
)
  .split(",")
  .map((value) => value.trim().replace(/\/+$/g, ""))
  .filter(Boolean);

test.setTimeout(25 * 60 * 1000);

test("validates the real control-plane UI", async ({ browser, page }) => {
  if (planeIDs.length === 0) {
    throw new Error("MINI_CLOUD_E2E_PLANES must contain at least one plane id");
  }

  const servicePlans = planeIDs.map((planeID, index) => {
    const name = serviceNameForPlane(planeID, index);
    return { planeID, name, host: `${name}.${baseDomain}` };
  });
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

  const serviceResults = await Promise.allSettled(
    servicePlans.map(async (service) => {
      const servicePage = await browser.newPage();
      let stage = "open control-plane UI";
      try {
        await openControlPlane(servicePage);
        stage = "login";
        await loginControlPlane(servicePage);
        console.log(
          `[ops-e2e] create service ${service.name} on ${service.planeID}`,
        );
        stage = "create service";
        await createService(servicePage, {
          name: service.name,
          planeID: service.planeID,
        });

        stage = "find service card";
        const card = servicePage
          .getByRole("article")
          .filter({ hasText: service.name });
        await expect(card).toBeVisible({ timeout: 30_000 });
        createdServices.push({ name: service.name, host: service.host });
        console.log(`[ops-e2e] wait service ${service.name} ready`);
        stage = "wait service ready";
        await expect(card).toContainText("ready / running", {
          timeout: 12 * 60 * 1000,
        });
        console.log(`[ops-e2e] wait service ${service.name} frontdoor`);
        stage = "wait frontdoor";
        await waitForPublicEntry(servicePage, service.name, service.planeID);

        console.log(`[ops-e2e] wait public HTTP 200 for ${service.host}`);
        stage = "wait public HTTP 200";
        await expectHTTP200(service.host);
      } catch (error) {
        throw new Error(
          `${service.name} on ${service.planeID} failed during ${stage}: ${errorMessage(error)}`,
        );
      } finally {
        await servicePage.close();
      }
    }),
  );

  const serviceErrors = rejectedReasons(serviceResults);
  const cleanupResults = await Promise.allSettled(
    createdServices.map(async (service) => {
      const servicePage = await browser.newPage();
      try {
        await openControlPlane(servicePage);
        await loginControlPlane(servicePage);
        console.log(`[ops-e2e] delete service ${service.name}`);
        await deleteService(servicePage, service.name);
      } finally {
        await servicePage.close();
      }
    }),
  );
  const cleanupErrors = rejectedReasons(cleanupResults);
  for (const error of cleanupErrors) {
    console.error(`[ops-e2e] cleanup failed: ${errorMessage(error)}`);
  }
  if (serviceErrors.length > 0) {
    throw new Error(
      `service validation failed:\n${serviceErrors
        .map((error) => `- ${errorMessage(error)}`)
        .join("\n")}`,
    );
  }
  if (cleanupErrors.length > 0) {
    throw new Error(
      `service cleanup failed:\n${cleanupErrors
        .map((error) => `- ${errorMessage(error)}`)
        .join("\n")}`,
    );
  }
});

function rejectedReasons<T>(results: PromiseSettledResult<T>[]): unknown[] {
  return results.flatMap((result) =>
    result.status === "rejected" ? [result.reason] : [],
  );
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

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
        const response = await page.request.get(
          apiURL(page, "/api/v1/services"),
          {
            headers: { Authorization: `Bearer ${adminToken}` },
          },
        );
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
    for (const url of controlPlaneURLs) {
      try {
        await page.goto(url, { waitUntil: "domcontentloaded", timeout: 10_000 });
        await expect(page.getByLabel("Admin token")).toBeVisible({
          timeout: 10_000,
        });
        return;
      } catch (error) {
        lastError = error;
      }
    }
    await page.waitForTimeout(5_000);
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
        const response = await page.request.get(
          apiURL(page, "/api/v1/services"),
          {
            headers: { Authorization: `Bearer ${adminToken}` },
          },
        );
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

function apiURL(page: import("@playwright/test").Page, path: string): string {
  const currentURL = page.url();
  if (!currentURL || currentURL === "about:blank") {
    throw new Error("control-plane page is not open");
  }
  return new URL(path, currentURL).toString();
}

async function expectHTTP200(host: string) {
  let lastResult = "no request attempted";
  await expect
    .poll(
      async () => {
        try {
          const response = await fetch(`http://${host}`, {
            method: "GET",
            signal: AbortSignal.timeout(20_000),
          });
          lastResult = `HTTP ${response.status}`;
          return response.status;
        } catch (error) {
          lastResult = errorMessage(error);
          return 0;
        }
      },
      {
        message: `wait for http://${host} to return 200; last result: ${lastResult}`,
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
