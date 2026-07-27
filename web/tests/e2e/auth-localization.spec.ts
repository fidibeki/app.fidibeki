import {
  expect,
  test,
  type Page,
  type Route,
  type TestInfo,
} from "@playwright/test";
import { allTenantPermissions } from "../../data/permissions";

const port = process.env.PLAYWRIGHT_PORT || "4321";
const baseOrigin = `http://localhost:${port}`;
const tenantOrigin = `http://acme.localhost:${port}`;
const now = "2026-07-21T12:00:00.000Z";
const plans = [
  {
    plan: "starter",
    monthly_price_cents: 999,
    yearly_discount_percent: 7,
    limits: {
      projects: 1,
      feedback_per_month: 500,
      users: 1,
      storage_days: 30,
    },
    features: {
      ai: false,
      session_replay: false,
      slack: false,
      video: false,
      audio: false,
      dom_capture: false,
      custom_domain: false,
    },
  },
  {
    plan: "growth",
    monthly_price_cents: 2999,
    yearly_discount_percent: 14,
    limits: {
      projects: 4,
      feedback_per_month: 2500,
      users: 3,
      storage_days: 90,
    },
    features: {
      ai: true,
      session_replay: true,
      slack: false,
      video: false,
      audio: true,
      dom_capture: true,
      custom_domain: false,
    },
  },
];

test.use({
  extraHTTPHeaders: {
    "Accept-Language": "pt-AO,pt;q=0.9,en;q=0.7",
  },
  locale: "pt-AO",
  timezoneId: "Africa/Luanda",
});

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);
  await page.screenshot({
    path: testInfo.outputPath(`${name}.png`),
    fullPage: false,
    animations: "disabled",
  });
}

function mockTurnstile(page: Page) {
  return page.route("**/turnstile/**", (route) =>
    route.fulfill({
      contentType: "application/javascript",
      body: `window.turnstile={render:function(el,options){el.textContent='Verificado';setTimeout(function(){options.callback('pt-token')},0);return 'pt-widget'},reset:function(){},remove:function(){}};`,
    }),
  );
}

test("Portuguese public authentication and signup surfaces keep safe localized copy", async ({
  page,
}, testInfo) => {
  await mockTurnstile(page);
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") return route.continue();
    const path = new URL(request.url()).pathname;
    const method = request.method();
    if (path === "/v1/config" && method === "GET") {
      return json(route, {
        tenant: {
          id: "tenant-pt",
          name: "Acme Angola",
          slug: "acme",
          plan: "growth",
          status: "active",
        },
        plan: plans[1],
      });
    }
    if (path === "/auth/login" && method === "POST")
      return json(route, {}, 202);
    if (path === "/v1/plans" && method === "GET") return json(route, { plans });
    if (path === "/v1/signup" && method === "POST") {
      return json(
        route,
        {
          tenant: {
            id: "tenant-new",
            name: "Acme Angola",
            slug: "acme-angola",
            plan: "growth",
            status: "trial",
            trial_ends_at: "2026-08-04T12:00:00.000Z",
          },
        },
        201,
      );
    }
    if (path === "/auth/member-domains/verify" && method === "POST") {
      return json(route, {
        member_domain: {
          id: "domain-pt",
          tenant_id: "tenant-pt",
          domain: "acme.ao",
          verification_email: "security@acme.ao",
          verification_expires_at: now,
          verified_at: now,
          status: "verified",
          created_at: now,
          updated_at: now,
        },
      });
    }
    if (path === "/auth/verify" && method === "POST") {
      return json(
        route,
        {
          error: {
            code: "invalid_token",
            message: "provider secret must never reach the browser",
          },
        },
        401,
      );
    }
    return json(
      route,
      { error: { code: "not_found", message: "unhandled localized route" } },
      404,
    );
  });

  await page.goto(`${tenantOrigin}/login`);
  await expect(page.locator("html")).toHaveAttribute("lang", "pt-AO");
  await expect(page).toHaveTitle("Iniciar sessão · Fidibeki");
  await expect(
    page.getByRole("heading", { name: "Iniciar sessão na Fidibeki" }),
  ).toBeVisible();
  // Submit is disabled until the email ref has a value, and v-model does not read
  // the DOM back on hydration: filling the server-rendered input before Vue
  // attaches leaves the ref empty and the button disabled for good. Wait for the
  // app to mount first, as activation-flow does.
  await page.waitForFunction(() =>
    Boolean(
      (document.querySelector("#__nuxt") as Element & { __vue_app__?: unknown })
        ?.__vue_app__,
    ),
  );
  await page.getByLabel("Email profissional").fill("owner@acme.ao");
  await expect(
    page.getByRole("button", { name: "Enviar link de acesso" }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "Enviar link de acesso" }).click();
  await expect(page.getByRole("status")).toContainText(
    "Consulte a sua caixa de entrada",
  );

  await page.goto(`${baseOrigin}/signup`);
  await expect(page).toHaveTitle("Criar workspace · Fidibeki");
  await expect(
    page.getByRole("heading", { name: "Crie o seu workspace" }),
  ).toBeVisible();
  await expect(page.locator(".signup-plan-grid")).toContainText("9,99");
  await page.getByLabel("Nome do workspace").fill("Acme Angola");
  await page.getByLabel("Email do proprietário").fill("owner@acme.ao");
  await expect(page.getByLabel("URL do workspace")).toHaveValue("acme-angola");
  await page.getByRole("button", { name: "Criar workspace" }).click();
  await expect(
    page.getByRole("heading", { name: "Consulte owner@acme.ao" }),
  ).toBeVisible();
  await capture(page, testInfo, `auth-signup-pt-${testInfo.project.name}`);

  await page.goto(`${tenantOrigin}/auth/domain-verify#token=domain-token`);
  await expect(
    page.getByRole("heading", { name: "acme.ao está verificado" }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Continuar para iniciar sessão" }),
  ).toBeVisible();

  await page.goto(`${tenantOrigin}/auth/verify#token=invalid-token`);
  await expect(
    page.getByRole("heading", {
      name: "Não foi possível verificar o link de acesso",
    }),
  ).toBeVisible();
  await expect(
    page.getByText("Este link de acesso é inválido ou expirou."),
  ).toBeVisible();
  await expect(
    page.getByText("provider secret must never reach the browser"),
  ).toHaveCount(0);
});

test("Portuguese magic-link onboarding completes project targeting and connection", async ({
  page,
}, testInfo) => {
  const tenant = {
    id: "tenant-pt",
    name: "Acme Angola",
    slug: "acme",
    plan: "growth",
    status: "trial",
    trial_ends_at: "2026-08-04T12:00:00.000Z",
    deletion_requested_at: null,
    reporter_access_enabled: true,
    default_locale: "en",
    default_timezone: "UTC",
  };
  const member = {
    id: "member-pt",
    tenant_id: tenant.id,
    email: "owner@acme.ao",
    name: "Ana",
    locale: "pt-AO",
    timezone: "Africa/Luanda",
    permissions: [...allTenantPermissions],
    status: "active",
    role: "owner",
    verified: true,
  };
  const projects: Array<Record<string, unknown>> = [];
  let createdRule: Record<string, unknown> = {};

  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async () => undefined },
    });
  });
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") return route.continue();
    const path = new URL(request.url()).pathname;
    const method = request.method();
    if (path === "/auth/verify" && method === "POST") {
      return json(route, {
        access_token: "pt-access-token",
        token_type: "Bearer",
        expires_in: 900,
        member,
        tenant,
      });
    }
    if (path === "/v1/projects" && method === "GET")
      return json(route, { projects });
    if (path === "/v1/projects" && method === "POST") {
      const body = request.postDataJSON() as Record<string, unknown>;
      const project = {
        id: "project-pt",
        tenant_id: tenant.id,
        name: body.name,
        public_key: "pk_live_portuguese000000",
        allowed_domains: body.allowed_domains,
        default_locale: "pt-AO",
        widget_last_seen_at: null,
        created_at: now,
        updated_at: now,
      };
      projects.push(project);
      return json(route, { project }, 201);
    }
    if (path === "/v1/projects/project-pt/widget-rules" && method === "POST") {
      createdRule = request.postDataJSON() as Record<string, unknown>;
      return json(
        route,
        {
          widget_rule: {
            id: "rule-pt",
            project_id: "project-pt",
            ...createdRule,
            created_at: now,
            updated_at: now,
          },
        },
        201,
      );
    }
    if (path === "/v1/projects/project-pt" && method === "GET") {
      return json(route, {
        project: { ...projects[0], widget_last_seen_at: now },
      });
    }
    return json(
      route,
      { error: { code: "not_found", message: "unhandled onboarding route" } },
      404,
    );
  });

  await page.goto(`${tenantOrigin}/auth/verify#token=pt-magic-link`);
  await expect(page).toHaveURL(`${tenantOrigin}/getting-started`);
  await expect(page.locator("html")).toHaveAttribute("lang", "pt-AO");
  await expect(page).toHaveTitle("Ligar o primeiro projeto · Fidibeki");
  await expect(
    page.getByRole("heading", { name: "Ligue o seu primeiro projeto" }),
  ).toBeVisible();

  await page
    .getByLabel("Domínios permitidos")
    .fill("acme.ao\n*.preview.acme.ao");
  await page.getByRole("button", { name: "Criar e configurar" }).click();
  await expect(
    page.getByRole("heading", { name: "Escolher páginas alvo" }),
  ).toBeVisible();
  await page.getByRole("radio", { name: "Páginas específicas" }).click();
  await page
    .getByLabel("Valor de inclusão 1")
    .fill("https://acme.ao/chart?asset=BAI#price");
  await page
    .getByRole("button", { name: "Continuar para a instalação" })
    .click();

  await expect(page.getByText("Instalar loader")).toBeVisible();
  await expect(page.getByTitle("Copiar código de instalação")).toBeVisible();
  expect(createdRule).toMatchObject({
    matcher_type: "url_exact",
    matcher_value: "https://acme.ao/chart",
    include_conditions: [
      {
        matcher_type: "url_exact",
        matcher_value: "https://acme.ao/chart",
      },
    ],
  });
  await expect(
    page.getByRole("heading", { name: "Widget ligado" }),
  ).toBeVisible({ timeout: 8_000 });
  await expect(page.getByText(/21\/07\/2026/)).toBeVisible();
  await capture(page, testInfo, `onboarding-pt-${testInfo.project.name}`);
});

test("Portuguese members without assigned sections see localized access guidance", async ({
  page,
}) => {
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") return route.continue();
    const path = new URL(request.url()).pathname;
    if (path === "/auth/verify" && request.method() === "POST") {
      return json(route, {
        access_token: "restricted-pt-token",
        token_type: "Bearer",
        expires_in: 900,
        member: {
          id: "restricted-pt",
          tenant_id: "tenant-pt",
          email: "viewer@acme.ao",
          name: "Maria",
          locale: "pt-AO",
          timezone: "Africa/Luanda",
          permissions: [],
          status: "active",
          role: "member",
          verified: true,
        },
        tenant: {
          id: "tenant-pt",
          name: "Acme Angola",
          slug: "acme",
          plan: "growth",
          status: "active",
          reporter_access_enabled: true,
        },
      });
    }
    return json(
      route,
      { error: { code: "not_found", message: "unhandled access route" } },
      404,
    );
  });

  await page.goto(`${tenantOrigin}/auth/verify#token=restricted-token`);
  await expect(page).toHaveURL(`${tenantOrigin}/access-denied`);
  await expect(page).toHaveTitle("Acesso necessário · Fidibeki");
  await expect(
    page.getByRole("heading", { name: "Nenhuma secção atribuída" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Permissão necessária" }),
  ).toBeVisible();
});
