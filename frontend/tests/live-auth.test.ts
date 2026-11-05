// Opt-in: real signup and admin UI backed by the running gateway/PostgreSQL.
import { createApp, nextTick } from "vue";
import { readFileSync } from "node:fs";
import { describe, expect, it, vi } from "vitest";
import App from "../src/App.vue";
import { api } from "../src/lib/api";

describe.skipIf(process.env.RUN_LIVE_API_TESTS !== "1")(
  "live signup and role editor",
  () => {
    it("signs up an employee and lets an admin assign manager access", async () => {
      const admin = JSON.parse(
        readFileSync(
          process.env.ADMIN_CREDENTIALS_FILE ||
            "../secrets/admin-credentials.json",
          "utf8",
        ),
      )[0] as { username: string; password: string };
      const username = `ui.signup.${crypto.randomUUID().replaceAll("-", "")}`;
      const password = crypto.randomUUID() + "!Aa9";
      const root = document.createElement("div");
      document.body.append(root);
      const originalFetch = globalThis.fetch;
      let cookie = "";
      let registered = false;
      // Node has no cookie jar: emulate browser cookie transport only. API responses
      // and all identity/role checks remain real, with no fake purchase/user data.
      vi.stubGlobal(
        "fetch",
        async (url: RequestInfo | URL, options?: RequestInit) => {
          const headers = new Headers(options?.headers);
          headers.set("Origin", "http://localhost:5173");
          if (cookie) headers.set("Cookie", cookie);
          const response = await originalFetch(url, { ...options, headers });
          const setCookie = response.headers.get("set-cookie");
          if (setCookie) cookie = setCookie.split(";")[0];
          return response;
        },
      );
      const app = createApp(App);
      app.mount(root);
      async function fill(id: string, value: string) {
        const input = root.querySelector<HTMLInputElement>(`#${id}`)!;
        input.value = value;
        input.dispatchEvent(new Event("input", { bubbles: true }));
        await nextTick();
      }
      function button(label: string) {
        return Array.from(
          root.querySelectorAll<HTMLButtonElement>("button"),
        ).find((b) => b.textContent?.trim() === label)!;
      }
      function submit() {
        root
          .querySelector("form")!
          .dispatchEvent(
            new Event("submit", { bubbles: true, cancelable: true }),
          );
      }
      async function signIn(name: string, secret: string) {
        await vi.waitFor(() =>
          expect(root.querySelector("#username")).not.toBeNull(),
        );
        await fill("username", name);
        await fill("password", secret);
        submit();
        await vi.waitFor(
          () => expect(root.querySelector(".account-bar")).not.toBeNull(),
          { timeout: 8000 },
        );
      }
      try {
        await vi.waitFor(() =>
          expect(root.querySelector("#username")).not.toBeNull(),
        );
        button("Create an account").click();
        await nextTick();
        expect(button("Create account")).toBeDefined();
        await fill("signup-name", "Fictional UI Signup User");
        await fill("username", username);
        await fill("password", password);
        await fill("confirm-password", password);
        submit();
        await vi.waitFor(
          () => expect(root.textContent).toContain("Account created"),
          { timeout: 8000 },
        );
        registered = true;
        await signIn(username, password);
        expect(root.querySelector(".account-bar")?.textContent).toContain(
          "employee",
        );
        expect(button("Manage users")).toBeUndefined();
        button("Sign out").click();
        await signIn(admin.username, admin.password);
        button("Manage users").click();
        const row = () =>
          Array.from(
            root.querySelectorAll<HTMLTableRowElement>(".admin-page tbody tr"),
          ).find((r) => r.textContent?.includes(username));
        await vi.waitFor(() => expect(row()).toBeDefined(), { timeout: 8000 });
        const select = row()!.querySelector<HTMLSelectElement>("select")!;
        select.value = "manager";
        select.dispatchEvent(new Event("change", { bubbles: true }));
        await nextTick();
        row()!
          .querySelector("form")!
          .dispatchEvent(
            new Event("submit", { bubbles: true, cancelable: true }),
          );
        await vi.waitFor(
          () =>
            expect(root.textContent).toContain(`Role saved for ${username}`),
          { timeout: 8000 },
        );
        expect(row()!.querySelectorAll("td")[2].textContent).toBe("manager");
        button("Sign out").click();
        await signIn(username, password);
        expect(root.querySelector(".account-bar")?.textContent).toContain(
          "manager",
        );
        expect(button("Manage users")).toBeUndefined();
        console.info(
          `Live signup and admin role assignment passed for ${username}`,
        );
      } finally {
        app.unmount();
        try {
          // Retain test data but avoid leaving an extra privileged synthetic account.
          if (registered) {
            await api.login(admin.username, admin.password);
            const account = (await api.adminUsers()).find(
              (u) => u.username === username,
            )!;
            await api.setRole(account.id, "employee");
            await api.logout();
          }
        } finally {
          root.remove();
          vi.unstubAllGlobals();
        }
      }
    }, 30000);
  },
);
