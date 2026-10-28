import { expect, test } from "@playwright/test";
import { marketingRoutes } from "../../data/site";

// Raw-HTML checks: everything here must hold without executing any JavaScript,
// which is exactly what crawlers see. Run once, on the desktop project.

const siteUrl = "https://fidibeki.com";

async function fetchHtml(
  request: import("@playwright/test").APIRequestContext,
  route: string,
) {
  const response = await request.get(route);
  expect(response.status(), `${route} should prerender`).toBe(200);
  return response.text();
}

test("every route has unique, complete metadata", async ({ request }) => {
  const titles = new Map<string, string>();
  const descriptions = new Map<string, string>();

  for (const route of marketingRoutes) {
    const html = await fetchHtml(request, route);

    const title = html.match(/<title>([^<]+)<\/title>/)?.[1];
    expect(title, `${route} title`).toBeTruthy();
    expect(
      titles.get(title!),
      `duplicate title "${title}" on ${route}`,
    ).toBeUndefined();
    titles.set(title!, route);

    const description = html.match(
      /<meta name="description" content="([^"]+)"/,
    )?.[1];
    expect(description, `${route} description`).toBeTruthy();
    expect(description!.length, `${route} description length`).toBeGreaterThan(
      40,
    );
    expect(
      descriptions.get(description!),
      `duplicate description on ${route} and ${descriptions.get(description!)}`,
    ).toBeUndefined();
    descriptions.set(description!, route);

    const canonical = html.match(/<link rel="canonical" href="([^"]+)"/)?.[1];
    const expectedCanonical =
      route === "/" ? `${siteUrl}/` : `${siteUrl}${route}`;
    expect(canonical, `${route} canonical`).toBe(expectedCanonical);

    expect(html, `${route} og:title`).toMatch(
      /<meta property="og:title" content="[^"]+"/,
    );
    expect(html, `${route} og:image`).toMatch(
      /<meta property="og:image" content="[^"]+"/,
    );
    expect(html, `${route} twitter card`).toMatch(
      /<meta name="twitter:card" content="summary_large_image"/,
    );
    expect(html, `${route} lang`).toContain('lang="en"');
  }
});

test("structured data parses on every route", async ({ request }) => {
  for (const route of marketingRoutes) {
    const html = await fetchHtml(request, route);
    const blocks = [
      ...html.matchAll(
        /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
      ),
    ];
    expect(blocks.length, `${route} should embed JSON-LD`).toBeGreaterThan(0);
    const types = new Set<string>();
    for (const block of blocks) {
      const parsed = JSON.parse(block[1] ?? "{}") as { "@type"?: string };
      expect(parsed["@type"], `${route} JSON-LD type`).toBeTruthy();
      types.add(parsed["@type"]!);
    }
    expect(types, `${route} should include Organization`).toContain(
      "Organization",
    );
    expect(types, `${route} should include WebSite`).toContain("WebSite");
    if (route !== "/") {
      expect(types, `${route} should include BreadcrumbList`).toContain(
        "BreadcrumbList",
      );
    }
  }
});

test("home and pricing expose verified offers", async ({ request }) => {
  for (const route of ["/", "/pricing"]) {
    const html = await fetchHtml(request, route);
    const blocks = [
      ...html.matchAll(
        /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
      ),
    ];
    const app = blocks
      .map((block) => JSON.parse(block[1] ?? "{}") as Record<string, unknown>)
      .find((data) => data["@type"] === "SoftwareApplication");
    expect(app, `${route} SoftwareApplication`).toBeTruthy();
    const offers = app!.offers as { price: string }[];
    expect(offers.map((offer) => offer.price)).toEqual([
      "9.99",
      "29.99",
      "67.99",
    ]);
  }
});

test("content is served without client-side JavaScript", async ({
  request,
}) => {
  for (const route of marketingRoutes) {
    const html = await fetchHtml(request, route);
    const h1Matches = [...html.matchAll(/<h1[\s>]/g)];
    expect(h1Matches.length, `${route} must have exactly one h1`).toBe(1);
    const mainContent = html.match(/<main[\s\S]*<\/main>/)?.[0] ?? "";
    const textOnly = mainContent
      .replace(/<[^>]+>/g, " ")
      .replace(/\s+/g, " ")
      .trim();
    expect(
      textOnly.length,
      `${route} must ship real prerendered content`,
    ).toBeGreaterThan(200);
  }
});

test("unknown routes return 404", async ({ request }) => {
  const response = await request.get("/this-page-does-not-exist");
  expect(response.status()).toBe(404);
});
