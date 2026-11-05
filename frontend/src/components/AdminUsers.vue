<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api, type User } from "../lib/api";
defineProps<{ user: User }>();
const users = ref<User[]>([]);
const roles = ref<Record<string, User["role"]>>({});
const loading = ref(false);
const saving = ref("");
const error = ref("");
const success = ref("");
async function refresh() {
  loading.value = true;
  error.value = "";
  try {
    users.value = await api.adminUsers();
    for (const u of users.value) roles.value[u.id] = u.role;
  } catch (e) {
    error.value = e instanceof Error ? e.message : "Could not load users.";
  } finally {
    loading.value = false;
  }
}
async function save(user: User) {
  saving.value = user.id;
  error.value = "";
  success.value = "";
  try {
    const updated = await api.setRole(user.id, roles.value[user.id]);
    users.value = users.value.map((u) => (u.id === updated.id ? updated : u));
    success.value = `Role saved for ${updated.username}. Their existing sessions are invalidated when the role changes.`;
    // A self-demotion invalidates this admin's session too.
    await api.me();
  } catch (e) {
    error.value =
      e instanceof Error
        ? e.message
        : "Could not save role. Refresh to check the saved state.";
  } finally {
    saving.value = "";
  }
}
onMounted(refresh);
</script>
<template>
  <main class="admin-page">
    <header>
      <div>
        <p class="eyebrow">ADMINISTRATION</p>
        <h1>Manage users</h1>
      </div>
      <button
        class="button secondary"
        type="button"
        :disabled="loading || !!saving"
        @click="refresh"
      >
        Refresh users
      </button>
    </header>
    <p>
      Assign employee, manager or admin access. Managers and admins can review
      other people's purchases. The last admin cannot be demoted.
    </p>
    <p role="alert" v-if="error" class="error-banner">{{ error }}</p>
    <p role="status" v-if="success" class="success-banner">{{ success }}</p>
    <p role="status" v-if="loading">Loading users…</p>
    <div class="admin-table" v-else>
      <table>
        <caption class="sr-only">
          Registered users and their roles
        </caption>
        <thead>
          <tr>
            <th scope="col">User</th>
            <th scope="col">Identity</th>
            <th scope="col">Current role</th>
            <th scope="col">Assign role</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="account in users" :key="account.id">
            <td>
              {{ account.name }} <span v-if="account.id === user.id">(you)</span
              ><br /><small>{{ account.username }}</small>
            </td>
            <td>
              <code>{{ account.id }}</code>
            </td>
            <td>{{ account.role }}</td>
            <td>
              <form @submit.prevent="save(account)">
                <label class="sr-only" :for="`role-${account.id}`"
                  >Role for {{ account.username }}</label
                ><select
                  :id="`role-${account.id}`"
                  v-model="roles[account.id]"
                  :disabled="!!saving"
                >
                  <option value="employee">Employee</option>
                  <option value="manager">Manager</option>
                  <option value="admin">Admin</option></select
                ><button
                  type="submit"
                  class="button secondary"
                  :disabled="!!saving || roles[account.id] === account.role"
                >
                  {{ saving === account.id ? "Saving…" : "Save role" }}
                </button>
              </form>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="users.length === 0">No users found.</p>
    </div>
  </main>
</template>
<style scoped>
.admin-page {
  max-width: 1200px;
  margin: 0 auto;
  padding: 2rem;
}
.admin-page header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
.admin-page h1 {
  margin: 0.5rem 0;
}
.admin-table {
  overflow-x: auto;
  margin-top: 2rem;
  background: white;
  border: 1px solid #d8e2dc;
  border-radius: 12px;
}
table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
}
th,
td {
  padding: 1rem;
  border-bottom: 1px solid #e3e9e5;
}
form {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
select {
  padding: 0.6rem;
  border-radius: 6px;
  font: inherit;
}
small {
  color: #526174;
}
</style>
