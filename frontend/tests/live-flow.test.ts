// Opt-in acceptance check: writes fictional requests through the REAL gateway.
// No mocked API responses or seeded dashboard data are used in this file.
import { createApp, nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";
import App from "../src/App.vue";

async function fill(root: HTMLElement, selector: string, value: string) {
  const input = root.querySelector<HTMLInputElement | HTMLTextAreaElement>(
    selector,
  )!;
  input.value = value;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  await nextTick();
}

describe.skipIf(process.env.RUN_LIVE_API_TESTS !== "1")(
  "live gateway acceptance (creates real fictional records)",
  () => {
    it.each(["APPROVED", "REJECTED"] as const)(
      "creates, lists, decides %s, and refreshes a real request",
      async (status) => {
        const root = document.createElement("div");
        document.body.append(root);
        const originalFetch = globalThis.fetch;
        let listCalls = 0;
        vi.stubGlobal(
          "fetch",
          async (url: RequestInfo | URL, options?: RequestInit) => {
            if (
              String(url).endsWith("/api/purchases") &&
              options?.method === "GET"
            )
              listCalls++;
            const headers = new Headers(options?.headers);
            headers.set("Origin", "http://localhost:5173");
            const response = await originalFetch(url, { ...options, headers });
            expect(response.headers.get("Access-Control-Allow-Origin")).toBe(
              "http://localhost:5173",
            );
            return response;
          },
        );
        const app = createApp(App);
        app.mount(root);
        try {
          await vi.waitFor(
            () => expect(root.querySelector(".count-badge")).not.toBeNull(),
            { timeout: 8000 },
          );
          await vi.waitFor(() =>
            expect(
              root.querySelector<HTMLButtonElement>(".refresh-button")!
                .disabled,
            ).toBe(false),
          );
          expect(root.querySelector(".error-banner")).toBeNull();
          const item = `Phase 05 ${status.toLowerCase()} check ${crypto.randomUUID().slice(0, 8)}`;
          await fill(root, "#employee-id", "emp-demo-505");
          await fill(root, "#item-name", item);
          await fill(root, "#quantity", "2");
          await fill(root, "#unit-price", "800.29");
          root
            .querySelector("form")!
            .dispatchEvent(
              new Event("submit", { bubbles: true, cancelable: true }),
            );
          const findRow = () =>
            Array.from(
              root.querySelectorAll<HTMLTableRowElement>("tbody tr"),
            ).find((row) => row.textContent?.includes(item));
          await vi.waitFor(() => expect(findRow()).toBeDefined(), {
            timeout: 8000,
          });
          expect(findRow()!.textContent).toContain("€1,600.58");
          expect(findRow()!.textContent).toContain("Not provided");
          findRow()!
            .querySelector<HTMLButtonElement>(".review-button")!
            .click();
          await nextTick();
          expect(root.querySelector("#decision-panel")).not.toBeNull();
          await fill(
            root,
            "#decision-comment",
            "Fictional local acceptance check",
          );
          const action = status === "APPROVED" ? "Approve" : "Reject";
          Array.from(
            root.querySelectorAll<HTMLButtonElement>("#decision-panel button"),
          )
            .find((button) => button.textContent?.trim() === action)!
            .click();
          await vi.waitFor(
            () =>
              expect(
                findRow()!.querySelector(
                  `.status-badge.${status.toLowerCase()}`,
                ),
              ).not.toBeNull(),
            { timeout: 8000 },
          );
          expect(findRow()!.querySelector(".review-button")).toBeNull();
          const previousCalls = listCalls;
          root.querySelector<HTMLButtonElement>(".refresh-button")!.click();
          await vi.waitFor(() =>
            expect(listCalls).toBeGreaterThan(previousCalls),
          );
          await vi.waitFor(() =>
            expect(
              root.querySelector<HTMLButtonElement>(".refresh-button")!
                .disabled,
            ).toBe(false),
          );
          expect(
            findRow()!.querySelector(`.status-badge.${status.toLowerCase()}`),
          ).not.toBeNull();
          expect(root.querySelector(".error-banner")).toBeNull();
          console.info(
            `Live UI flow passed: ${findRow()!.querySelector(".request-id .sr-only")!.textContent} ${status}, 160058 integer cents`,
          );
        } finally {
          app.unmount();
          root.remove();
          vi.unstubAllGlobals();
        }
      },
      20000,
    );
  },
);
