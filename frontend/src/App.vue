<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { api, ApiError, type User } from "./lib/api";
import ProcurementDashboard from "./ProcurementDashboard.vue";
import AdminUsers from "./components/AdminUsers.vue";

const user = ref<User | null>(null);
const checking = ref(true);
const busy = ref(false);
const username = ref("");
const password = ref("");
const name = ref("");
const confirmPassword = ref("");
const signingUp = ref(false);
const showAdmin = ref(false);
const notice = ref("");
function toggleSignup() {
  signingUp.value = !signingUp.value;
  error.value = "";
  notice.value = "";
  password.value = "";
  confirmPassword.value = "";
}
const error = ref("");
function expired() {
  user.value = null;
  showAdmin.value = false;
  error.value = "Your session ended. Please sign in again.";
}
onMounted(async () => {
  window.addEventListener("session-expired", expired);
  try {
    user.value = (await api.me()).user;
    error.value = "";
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401))
      error.value =
        e instanceof Error ? e.message : "Could not check your session.";
    else error.value = "";
  } finally {
    checking.value = false;
  }
});
onUnmounted(() => window.removeEventListener("session-expired", expired));
async function login() {
  if (signingUp.value && password.value !== confirmPassword.value) {
    error.value = "Passwords must match.";
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    if (signingUp.value) {
      await api.signup(username.value, name.value, password.value);
      signingUp.value = false;
      notice.value = "Account created. Sign in with your new employee account.";
    } else {
      user.value = (await api.login(username.value, password.value)).user;
      showAdmin.value = false;
      notice.value = "";
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : "Sign-in failed.";
  } finally {
    password.value = "";
    confirmPassword.value = "";
    busy.value = false;
  }
}
async function logout() {
  busy.value = true;
  error.value = "";
  try {
    await api.logout();
    user.value = null;
  } catch (e) {
    error.value =
      e instanceof Error ? e.message : "Sign-out failed. Please try again.";
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <p v-if="checking" class="auth-status" role="status">
    Checking your session…
  </p>
  <template v-else-if="user">
    <header class="account-bar">
      <button
        v-if="user.role === 'admin'"
        class="button secondary"
        type="button"
        @click="showAdmin = !showAdmin"
      >
        {{ showAdmin ? "Purchases" : "Manage users" }}
      </button>
      <span>{{ user.name }} · {{ user.role }}</span
      ><button type="button" :disabled="busy" @click="logout">Sign out</button>
    </header>
    <p v-if="error" role="alert" class="auth-error">{{ error }}</p>
    <AdminUsers v-if="showAdmin && user.role === 'admin'" :user="user" />
    <ProcurementDashboard v-else :key="user.id" :user="user" />
  </template>
  <main v-else class="login-page">
    <form class="login-card" @submit.prevent="login">
      <p class="eyebrow">PROCUREFLOW</p>
      <h1>{{ signingUp ? "Create an account" : "Welcome back" }}</h1>
      <p>
        {{
          signingUp
            ? "New accounts start with the employee role. An admin can assign additional access."
            : "Sign in to your local procurement workspace."
        }}
      </p>
      <p class="auth-note">
        Fictional portfolio accounts. Use the credentials generated locally in
        <code>secrets/demo-credentials.json</code> or
        <code>secrets/admin-credentials.json</code>.
      </p>
      <p v-if="error" role="alert" class="auth-error">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <template v-if="signingUp"
        ><label for="signup-name">Display name</label
        ><input
          id="signup-name"
          v-model="name"
          required
          maxlength="120"
          autocomplete="name"
          :disabled="busy"
      /></template>
      <label for="username">Username</label
      ><input
        id="username"
        v-model="username"
        autocomplete="username"
        required
        maxlength="80"
        :disabled="busy"
      />
      <label for="password">Password</label
      ><input
        id="password"
        v-model="password"
        type="password"
        :autocomplete="signingUp ? 'new-password' : 'current-password'"
        :minlength="signingUp ? 12 : undefined"
        required
        maxlength="128"
        :disabled="busy"
      />
      <template v-if="signingUp"
        ><label for="confirm-password">Confirm password</label
        ><input
          id="confirm-password"
          v-model="confirmPassword"
          type="password"
          autocomplete="new-password"
          required
          maxlength="128"
          :disabled="busy"
        />
        <p class="auth-note">
          Username: 3–80 letters, digits, dots, underscores or hyphens; start
          with a letter or digit. Password: 12–128 bytes. Use fictional demo
          information.
        </p></template
      >
      <button class="button primary" type="submit" :disabled="busy">
        {{ busy ? "Please wait…" : signingUp ? "Create account" : "Sign in" }}
      </button>
      <button
        type="button"
        class="button secondary"
        :disabled="busy"
        @click="toggleSignup"
      >
        {{ signingUp ? "Back to sign in" : "Create an account" }}
      </button>
      <p class="auth-note">
        Employees see their own requests. Managers can review other people’s
        requests.
      </p>
    </form>
  </main>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 2rem;
  background: #f3f6fa;
}
.login-card {
  width: min(100%, 420px);
  padding: 2rem;
  border: 1px solid #dce3ed;
  border-radius: 20px;
  background: white;
  box-shadow: 0 12px 40px #12223a0c;
}
.login-card h1 {
  margin: 0.5rem 0;
}
.login-card label {
  display: block;
  font-weight: 600;
  margin: 1rem 0 0.4rem;
}
.login-card input {
  width: 100%;
  padding: 0.8rem;
  border: 1px solid #9aa8ba;
  border-radius: 8px;
  font: inherit;
}
.login-card button {
  margin-top: 1.4rem;
  width: 100%;
}
.auth-note {
  font-size: 0.85rem;
  color: #526174;
  line-height: 1.6;
}
.auth-error {
  color: #9b2222;
  padding: 0.8rem;
  background: #fff0f0;
}
.account-bar {
  display: flex;
  gap: 1rem;
  justify-content: flex-end;
  align-items: center;
  padding: 0.6rem 2rem;
  background: #edf3fb;
  color: #203b61;
}
.account-bar button {
  padding: 0.4rem 0.8rem;
  cursor: pointer;
}
.auth-status {
  padding: 3rem;
  text-align: center;
}
</style>
