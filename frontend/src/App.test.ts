import { createApp, nextTick, type App as VueApp } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App.vue";

const { list, create, decide } = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  decide: vi.fn(),
}));
vi.mock("./lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./lib/api")>()),
  api: { list, create, decide },
}));
let app: VueApp | undefined;
afterEach(() => {
  app?.unmount();
  document.body.innerHTML = "";
  vi.resetAllMocks();
});
function mount() {
  const root = document.createElement("div");
  document.body.append(root);
  app = createApp(App);
  app.mount(root);
  return root;
}

describe("dashboard states", () => {
  it("shows loading then an honest empty state", async () => {
    let finish!: (value: never[]) => void;
    list.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    const root = mount();
    await nextTick();
    expect(root.textContent).toContain("Loading your requests");
    finish([]);
    await vi.waitFor(() =>
      expect(root.textContent).toContain("Your next purchase starts here"),
    );
    expect(root.textContent).toContain("Demo identity only");
    expect(root.querySelectorAll("tbody tr")).toHaveLength(0);
  });
  it("lists purchases, hides final actions, and refreshes actual data", async () => {
    const base = {
      client_request_id: "key",
      employee_id: "emp-101",
      quantity: 2,
      unit_price_cents: 80000,
      total_cents: 160000,
    };
    list.mockResolvedValue([
      { ...base, id: "pending-1", item_name: "Laptop", status: "PENDING" },
      { ...base, id: "approved-1", item_name: "Monitor", status: "APPROVED" },
      { ...base, id: "rejected-1", item_name: "Chair", status: "REJECTED" },
    ]);
    const root = mount();
    await vi.waitFor(() =>
      expect(root.querySelectorAll("tbody tr")).toHaveLength(3),
    );
    expect(root.querySelectorAll("tbody .review-button")).toHaveLength(1);
    expect(root.querySelector("tbody")?.textContent).toContain("Not provided");
    expect(root.querySelector("tbody")?.textContent).toContain("€1,600.00");
    const refresh = root.querySelector<HTMLButtonElement>(".refresh-button")!;
    refresh.click();
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    await vi.waitFor(() => expect(refresh.disabled).toBe(false));
    root.querySelector<HTMLButtonElement>(".review-button")!.click();
    await nextTick();
    expect(root.querySelector("#decision-panel")).not.toBeNull();
  });
  it("shows request errors and recovers with refresh", async () => {
    list
      .mockRejectedValueOnce(new Error("Could not reach the API"))
      .mockResolvedValueOnce([]);
    const root = mount();
    await vi.waitFor(() =>
      expect(root.querySelector('[role="alert"]')?.textContent).toContain(
        "Could not reach the API",
      ),
    );
    expect(root.textContent).not.toContain("Your next purchase starts here");
    root.querySelector<HTMLButtonElement>(".refresh-button")!.click();
    await vi.waitFor(() =>
      expect(root.textContent).toContain("Your next purchase starts here"),
    );
  });
});
