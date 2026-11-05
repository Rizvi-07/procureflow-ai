import { createApp, nextTick, type App } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import PurchaseForm from "./PurchaseForm.vue";
import DecisionPanel from "./DecisionPanel.vue";
import { ApiError } from "../lib/api";

const mounted: App[] = [];
function mount(
  component: Parameters<typeof createApp>[0],
  props: Record<string, unknown>,
) {
  const target = document.createElement("div");
  document.body.append(target);
  const app = createApp(component, props);
  mounted.push(app);
  app.mount(target);
  return target;
}
async function fill(root: HTMLElement, selector: string, value: string) {
  const field = root.querySelector<HTMLInputElement>(selector)!;
  field.value = value;
  field.dispatchEvent(new Event("input", { bubbles: true }));
  await nextTick();
}
async function submit(root: HTMLElement) {
  root
    .querySelector("form")!
    .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  await nextTick();
}
afterEach(() => {
  mounted.splice(0).forEach((app) => app.unmount());
  document.body.innerHTML = "";
});

describe("purchase form", () => {
  it("validates fields before submitting and safely converts EUR into cents", async () => {
    const send = vi.fn().mockResolvedValue({});
    const root = mount(PurchaseForm, { disabled: false, submitRequest: send });
    await submit(root);
    expect(send).not.toHaveBeenCalled();
    expect(
      root.querySelector("#employee-id")?.getAttribute("aria-invalid"),
    ).toBe("true");
    expect(root.querySelector("#price-error")).not.toBeNull();
    await fill(root, "#employee-id", "emp-101");
    await fill(root, "#item-name", "Laptop");
    await fill(root, "#quantity", "2");
    await fill(root, "#unit-price", "800.29");
    await submit(root);
    await vi.waitFor(() => expect(send).toHaveBeenCalledOnce());
    expect(send.mock.calls[0][0]).toMatchObject({
      employee_id: "emp-101",
      item_name: "Laptop",
      quantity: 2,
      unit_price_cents: 80029,
    });
    expect(send.mock.calls[0][0].client_request_id).toMatch(/^[a-f0-9-]{36}$/);
  });
  it("retries an uncertain create using the identical key and frozen details", async () => {
    const send = vi
      .fn()
      .mockRejectedValueOnce(
        new ApiError("Backend unavailable", "UNAVAILABLE", true, 503),
      )
      .mockResolvedValueOnce({});
    const root = mount(PurchaseForm, { disabled: false, submitRequest: send });
    await fill(root, "#employee-id", "emp-101");
    await fill(root, "#item-name", "Lamp");
    await fill(root, "#unit-price", "0.29");
    await submit(root);
    await vi.waitFor(() =>
      expect(root.querySelector('[role="alert"]')?.textContent).toContain(
        "Backend unavailable",
      ),
    );
    expect(root.querySelector<HTMLInputElement>("#item-name")?.readOnly).toBe(
      true,
    );
    await submit(root);
    await vi.waitFor(() => expect(send).toHaveBeenCalledTimes(2));
    expect(send.mock.calls[1][0]).toEqual(send.mock.calls[0][0]);
  });
});

describe("manager form", () => {
  it.each(["APPROVED", "REJECTED"] as const)(
    "submits the explicit %s decision with demo identity",
    async (decision) => {
      const send = vi.fn().mockResolvedValue(undefined);
      const done = vi.fn();
      const root = mount(DecisionPanel, {
        purchase: {
          id: "request-1",
          item_name: "Laptop",
          quantity: 2,
          total_cents: 160000,
          status: "PENDING",
        },
        disabled: false,
        submitDecision: send,
        onDone: done,
      });
      await nextTick();
      expect(document.activeElement?.id).toBe("manager-id");
      await fill(root, "#decision-comment", "Within budget");
      const label = decision === "APPROVED" ? "Approve" : "Reject";
      const button = Array.from(root.querySelectorAll("button")).find(
        (element) => element.textContent?.trim() === label,
      )!;
      button.click();
      await vi.waitFor(() =>
        expect(send).toHaveBeenCalledWith("request-1", {
          manager_id: "manager-demo",
          decision,
          comment: "Within budget",
        }),
      );
      await vi.waitFor(() => expect(done).toHaveBeenCalledOnce());
      expect(root.textContent).toContain(
        "Your signed-in manager identity is verified",
      );
    },
  );
  it("keeps a failed decision visible with useful retry guidance", async () => {
    const send = vi
      .fn()
      .mockRejectedValue(
        new ApiError(
          "Backend deadline exceeded",
          "DEADLINE_EXCEEDED",
          true,
          504,
        ),
      );
    const root = mount(DecisionPanel, {
      purchase: {
        id: "request-1",
        item_name: "Laptop",
        quantity: 2,
        total_cents: 160000,
        status: "PENDING",
      },
      disabled: false,
      submitDecision: send,
    });
    Array.from(root.querySelectorAll("button"))
      .find((button) => button.textContent?.trim() === "Approve")!
      .click();
    await vi.waitFor(() =>
      expect(root.querySelector('[role="alert"]')?.textContent).toContain(
        "retry the same decision",
      ),
    );
  });
});
