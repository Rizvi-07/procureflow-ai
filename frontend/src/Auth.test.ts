import { createApp, nextTick, type App as VueApp } from "vue";
import { afterEach, expect, it, vi } from "vitest";
import App from "./App.vue";
const { me, login, logout, list, signup } = vi.hoisted(() => ({
  signup: vi.fn(),
  me: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  list: vi.fn(),
}));
vi.mock("./lib/api", async (original) => ({
  ...(await original<typeof import("./lib/api")>()),
  api: { me, login, logout, list, signup },
}));
let app: VueApp | undefined;
afterEach(() => {
  app?.unmount();
  document.body.innerHTML = "";
  vi.resetAllMocks();
});
const user = {
  id: "emp-101",
  username: "employee.demo",
  name: "Demo Employee",
  role: "employee",
};
it("requires sign-in, binds the employee ID, and removes data on sign-out", async () => {
  me.mockRejectedValue(new Error("Sign in to continue"));
  login.mockResolvedValue({ user });
  logout.mockResolvedValue({});
  list.mockResolvedValue([]);
  const root = document.createElement("div");
  document.body.append(root);
  app = createApp(App);
  app.mount(root);
  await vi.waitFor(() =>
    expect(root.querySelector("#username")).not.toBeNull(),
  );
  expect(list).not.toHaveBeenCalled();
  for (const [id, value] of [
    ["username", "employee.demo"],
    ["password", "fictional-test-password"],
  ]) {
    const input = root.querySelector<HTMLInputElement>(`#${id}`)!;
    input.value = value;
    input.dispatchEvent(new Event("input", { bubbles: true }));
  }
  await nextTick();
  root
    .querySelector("form")!
    .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  await vi.waitFor(() =>
    expect(root.querySelector<HTMLInputElement>("#employee-id")?.value).toBe(
      "emp-101",
    ),
  );
  expect(root.querySelector<HTMLInputElement>("#employee-id")!.readOnly).toBe(
    true,
  );
  expect(root.querySelector("#password")).toBeNull();
  root.querySelector<HTMLButtonElement>(".account-bar button")!.click();
  await vi.waitFor(() =>
    expect(root.querySelector("#username")).not.toBeNull(),
  );
  expect(root.querySelector("tbody")).toBeNull();
  expect(logout).toHaveBeenCalledOnce();
});
it("clears the dashboard immediately when a session expires", async () => {
  me.mockResolvedValue({ user });
  list.mockResolvedValue([]);
  const root = document.createElement("div");
  document.body.append(root);
  app = createApp(App);
  app.mount(root);
  await vi.waitFor(() =>
    expect(root.querySelector("#employee-id")).not.toBeNull(),
  );
  window.dispatchEvent(new Event("session-expired"));
  await nextTick();
  expect(root.querySelector("#username")).not.toBeNull();
  expect(root.textContent).toContain("Your session ended");
  expect(root.querySelector("tbody")).toBeNull();
});

it("creates an employee account through signup and returns to sign-in", async () => {
  me.mockRejectedValue(new Error("Sign in"));
  signup.mockResolvedValue(user);
  const root = document.createElement("div");
  document.body.append(root);
  app = createApp(App);
  app.mount(root);
  await vi.waitFor(() =>
    expect(root.querySelector("#username")).not.toBeNull(),
  );
  Array.from(root.querySelectorAll<HTMLButtonElement>("button"))
    .find((b) => b.textContent?.trim() === "Create an account")!
    .click();
  await nextTick();
  for (const [id, value] of [
    ["username", "employee.new"],
    ["signup-name", "Fictional New User"],
    ["password", "fictional-test-password"],
    ["confirm-password", "fictional-test-password"],
  ]) {
    const input = root.querySelector<HTMLInputElement>(`#${id}`)!;
    input.value = value;
    input.dispatchEvent(new Event("input", { bubbles: true }));
  }
  await nextTick();
  root
    .querySelector("form")!
    .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  await vi.waitFor(() =>
    expect(signup).toHaveBeenCalledWith(
      "employee.new",
      "Fictional New User",
      "fictional-test-password",
    ),
  );
  await vi.waitFor(() => expect(root.textContent).toContain("Account created"));
  expect(root.querySelector("#signup-name")).toBeNull();
  expect(root.querySelector<HTMLInputElement>("#password")!.value).toBe("");
  expect(login).not.toHaveBeenCalled();
});
