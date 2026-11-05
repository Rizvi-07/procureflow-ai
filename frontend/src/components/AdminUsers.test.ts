import { createApp, nextTick, type App as VueApp } from "vue";
import { afterEach, expect, it, vi } from "vitest";
import AdminUsers from "./AdminUsers.vue";
const { adminUsers, setRole, me } = vi.hoisted(() => ({
  adminUsers: vi.fn(),
  setRole: vi.fn(),
  me: vi.fn(),
}));
vi.mock("../lib/api", async (original) => ({
  ...(await original<typeof import("../lib/api")>()),
  api: { adminUsers, setRole, me },
}));
let app: VueApp | undefined;
afterEach(() => {
  app?.unmount();
  document.body.innerHTML = "";
  vi.resetAllMocks();
});
const admin = {
  id: "admin-demo",
  username: "admin.demo",
  name: "Admin",
  role: "admin" as const,
};
const employee = {
  id: "employee-new",
  username: "employee.new",
  name: "New Employee",
  role: "employee" as const,
};
function mount() {
  const root = document.createElement("div");
  document.body.append(root);
  app = createApp(AdminUsers, { user: admin });
  app.mount(root);
  return root;
}
it("lists users and saves an explicitly selected role", async () => {
  adminUsers.mockResolvedValue([admin, employee]);
  setRole.mockResolvedValue({ ...employee, role: "manager" });
  me.mockResolvedValue({ user: admin });
  const root = mount();
  await vi.waitFor(() =>
    expect(root.querySelector("#role-employee-new")).not.toBeNull(),
  );
  const select = root.querySelector<HTMLSelectElement>("#role-employee-new")!;
  select.value = "manager";
  select.dispatchEvent(new Event("change", { bubbles: true }));
  await nextTick();
  select
    .closest("form")!
    .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  await vi.waitFor(() =>
    expect(setRole).toHaveBeenCalledWith("employee-new", "manager"),
  );
  await vi.waitFor(() =>
    expect(root.textContent).toContain("Role saved for employee.new"),
  );
  expect(me).toHaveBeenCalledOnce();
});
it("shows a last-admin rejection without claiming the role was saved", async () => {
  adminUsers.mockResolvedValue([admin]);
  setRole.mockRejectedValue(new Error("The last admin cannot be demoted"));
  const root = mount();
  await vi.waitFor(() => expect(root.querySelector("select")).not.toBeNull());
  const select = root.querySelector<HTMLSelectElement>("select")!;
  select.value = "employee";
  select.dispatchEvent(new Event("change", { bubbles: true }));
  await nextTick();
  root
    .querySelector("form")!
    .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  await vi.waitFor(() =>
    expect(root.querySelector('[role="alert"]')?.textContent).toContain(
      "last admin",
    ),
  );
  expect(root.querySelector('[role="status"]')).toBeNull();
  expect(me).not.toHaveBeenCalled();
});
