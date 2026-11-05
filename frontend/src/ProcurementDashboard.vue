<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from "vue";
import { usePurchases } from "./composables/usePurchases";
import type { Purchase, PurchaseStatus, User } from "./lib/api";
import { formatEuro } from "./lib/money";
import AppIcon from "./components/AppIcon.vue";
import StatusBadge from "./components/StatusBadge.vue";
import PurchaseForm from "./components/PurchaseForm.vue";
import DecisionPanel from "./components/DecisionPanel.vue";
import PolicyAssistant from "./components/PolicyAssistant.vue";
const props = defineProps<{ user: User }>();

const {
  purchases,
  loading,
  loaded,
  mutating,
  loadError,
  success,
  lastSynced,
  refresh,
  create,
  decide,
} = usePurchases();
const search = ref("");
const statusFilter = ref<PurchaseStatus | "">("");
const selected = ref<Purchase | null>(null);
const busy = computed(() => loading.value || mutating.value);
const pending = computed(
  () => purchases.value.filter((p) => p.status === "PENDING").length,
);
const approved = computed(
  () => purchases.value.filter((p) => p.status === "APPROVED").length,
);
const requestedTotal = computed(() =>
  purchases.value.reduce((sum, p) => sum + BigInt(p.total_cents), 0n),
);
const filtered = computed(() =>
  purchases.value.filter(
    (p) =>
      (!statusFilter.value || p.status === statusFilter.value) &&
      `${p.item_name} ${p.id} ${p.employee_id}`
        .toLowerCase()
        .includes(search.value.toLowerCase().trim()),
  ),
);
const syncLabel = computed(() =>
  lastSynced.value
    ? `Last refreshed ${lastSynced.value.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
    : "Waiting for first refresh",
);
onMounted(refresh);

function review(purchase: Purchase) {
  if (
    !["manager", "admin"].includes(props.user.role) ||
    purchase.employee_id === props.user.id
  )
    return;
  selected.value = { ...purchase };
}
async function closeReview(done = false) {
  const id = selected.value?.id;
  selected.value = null;
  await nextTick();
  const target = done
    ? document.getElementById("requests-heading")
    : document.getElementById(`review-${id}`);
  target?.focus();
}
async function refreshAll() {
  await refresh();
  if (selected.value) {
    const current = purchases.value.find((p) => p.id === selected.value?.id);
    if (!current || current.status !== "PENDING") await closeReview(true);
  }
}
function submittedDate(value?: string) {
  return value
    ? new Intl.DateTimeFormat("en-GB", {
        day: "2-digit",
        month: "short",
        year: "numeric",
      }).format(new Date(value))
    : "";
}
</script>

<template>
  <a class="skip-link" href="#main-content">Skip to main content</a>
  <div class="app-shell">
    <aside class="sidebar" aria-label="Workspace navigation">
      <a class="brand" href="#overview" aria-label="ProcureFlow overview"
        ><span class="brand-mark"><AppIcon name="box" /></span
        ><span
          >ProcureFlow<span class="brand-subtitle"
            >PURCHASE WORKSPACE</span
          ></span
        ></a
      >
      <p class="nav-caption">WORKSPACE</p>
      <nav>
        <a class="nav-link active" href="#overview" aria-current="page"
          ><AppIcon name="grid" />Overview</a
        ><a class="nav-link" href="#requests"
          ><AppIcon name="box" />Requests<span class="nav-count">{{
            loaded ? purchases.length : "—"
          }}</span></a
        ><a class="nav-link" href="#request-form"
          ><AppIcon name="plus" />New request</a
        >
      </nav>
      <div class="sidebar-bottom">
        <span class="sidebar-indicator" aria-hidden="true"></span>
        <div>
          <strong>Local demo workspace</strong>
          <p>Fictional data. Real workflows.</p>
        </div>
      </div>
    </aside>

    <div class="page">
      <header class="topbar">
        <div class="breadcrumb">
          Workspace <span aria-hidden="true">/</span> <strong>Overview</strong>
        </div>
        <div class="demo-profile">
          <span class="avatar" aria-hidden="true">DM</span
          ><span
            >{{ user.name
            }}<span class="profile-note">{{ user.role }}</span></span
          >
        </div>
      </header>
      <main id="main-content">
        <section
          id="overview"
          class="page-heading"
          aria-labelledby="page-title"
        >
          <div>
            <p class="eyebrow">PROCUREMENT, ORGANIZED</p>
            <h1 id="page-title">Purchase overview</h1>
            <p>From request to decision. Keep every purchase in view.</p>
          </div>
          <a class="button primary" href="#request-form"
            ><AppIcon name="plus" />New request</a
          >
        </section>

        <div class="demo-banner">
          <AppIcon name="shield" />
          <p>
            <strong>Local portfolio accounts.</strong> Permissions are enforced
            by the services. Manager IDs and comments are not retained.
          </p>
          <span class="demo-tag">LOCAL DEMO</span>
        </div>
        <p v-if="success" class="success-banner" role="status">
          <AppIcon name="check" />{{ success }}
        </p>
        <div v-if="loadError" class="error-banner" role="alert">
          <strong>Could not refresh purchases.</strong> {{ loadError }}
          <span v-if="loaded">Previously loaded results are still shown.</span
          ><button
            class="button text-button"
            :disabled="busy"
            @click="refreshAll"
          >
            Try again
          </button>
        </div>

        <section class="stats-grid" aria-label="Purchase summary">
          <article class="stat-card">
            <div class="stat-top">
              <span>Total requested</span><AppIcon name="wallet" />
            </div>
            <strong>{{ loaded ? formatEuro(requestedTotal) : "—" }}</strong>
            <p>Across all purchase requests</p>
          </article>
          <article class="stat-card">
            <div class="stat-top">
              <span>Awaiting review</span><AppIcon name="clock" />
            </div>
            <strong
              >{{ loaded ? pending : "—"
              }}<span v-if="loaded" class="metric-unit">requests</span></strong
            >
            <p>
              <span class="tiny-dot pending-dot"></span>Ready for a manager
              decision
            </p>
          </article>
          <article class="stat-card">
            <div class="stat-top">
              <span>Approved</span><AppIcon name="check" />
            </div>
            <strong
              >{{ loaded ? approved : "—"
              }}<span v-if="loaded" class="metric-unit">requests</span></strong
            >
            <p><span class="tiny-dot approved-dot"></span>Review completed</p>
          </article>
        </section>

        <div class="workspace-grid">
          <section
            id="requests"
            class="card requests-card"
            aria-labelledby="requests-heading"
            :aria-busy="loading"
          >
            <div class="requests-header">
              <div>
                <h2 id="requests-heading" tabindex="-1">
                  Purchase requests
                  <span v-if="loaded" class="count-badge">{{
                    purchases.length
                  }}</span>
                </h2>
                <p>Review what’s needed and what’s next.</p>
              </div>
              <button
                class="button secondary refresh-button"
                :disabled="busy"
                @click="refreshAll"
              >
                <AppIcon name="refresh" :class="{ spinning: loading }" />{{
                  loading ? "Refreshing…" : "Refresh"
                }}
              </button>
            </div>
            <div class="table-toolbar">
              <div class="search-field">
                <AppIcon name="search" /><label
                  class="sr-only"
                  for="request-search"
                  >Search purchases by item, request ID or employee</label
                ><input
                  id="request-search"
                  v-model="search"
                  type="search"
                  placeholder="Find a request…"
                />
              </div>
              <label class="sr-only" for="status-filter">Filter by status</label
              ><select id="status-filter" v-model="statusFilter">
                <option value="">All statuses</option>
                <option value="PENDING">Pending review</option>
                <option value="APPROVED">Approved</option>
                <option value="REJECTED">Rejected</option>
              </select>
            </div>
            <div v-if="loading && !loaded" class="table-state" role="status">
              <AppIcon name="refresh" class="spinning state-icon" />
              <h3>Loading your requests</h3>
              <p>Connecting to your procurement workspace…</p>
            </div>
            <div v-else-if="!loaded && loadError" class="table-state">
              <AppIcon name="box" class="state-icon" />
              <h3>Requests are unavailable</h3>
              <p>Check the connection and select Refresh to try again.</p>
            </div>
            <div
              v-else-if="loaded && purchases.length === 0"
              class="table-state"
            >
              <span class="empty-icon"><AppIcon name="box" /></span>
              <h3>Your next purchase starts here</h3>
              <p>
                Submit your first request using the form. It will appear here
                for review.
              </p>
              <a class="button secondary" href="#request-form"
                >Create a request<AppIcon name="arrow"
              /></a>
            </div>
            <div
              v-else-if="loaded && filtered.length === 0"
              class="table-state"
            >
              <h3>No matching requests</h3>
              <p>Try another search or status.</p>
              <button
                class="button secondary"
                @click="
                  search = '';
                  statusFilter = '';
                "
              >
                Clear filters
              </button>
            </div>
            <div
              v-else
              class="table-scroll"
              tabindex="0"
              role="region"
              aria-label="Purchase request table, scroll horizontally if needed"
            >
              <table>
                <caption class="sr-only">
                  Live purchase requests. Manager review is available only for
                  pending requests.
                </caption>
                <thead>
                  <tr>
                    <th scope="col">Request ID</th>
                    <th scope="col">Item</th>
                    <th scope="col" class="number-column">Qty</th>
                    <th scope="col" class="number-column">Total EUR</th>
                    <th scope="col">Submitted</th>
                    <th scope="col">Status</th>
                    <th scope="col">
                      <span class="sr-only">Manager action</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="purchase in filtered"
                    :key="purchase.id"
                    :class="{ 'selected-row': selected?.id === purchase.id }"
                  >
                    <td class="request-id">
                      <span :title="purchase.id"
                        ><span aria-hidden="true"
                          >{{ purchase.id.slice(0, 8) }}…</span
                        ><span class="sr-only">{{ purchase.id }}</span></span
                      >
                    </td>
                    <th scope="row" class="item-cell">
                      <strong>{{ purchase.item_name }}</strong
                      ><span>{{ purchase.employee_id }}</span>
                    </th>
                    <td class="number-column">{{ purchase.quantity }}</td>
                    <td class="number-column money-cell">
                      {{ formatEuro(purchase.total_cents) }}
                    </td>
                    <td class="date-cell">
                      <time
                        v-if="purchase.created_at"
                        :datetime="purchase.created_at"
                        >{{ submittedDate(purchase.created_at) }}</time
                      ><span v-else>Not provided</span>
                    </td>
                    <td><StatusBadge :status="purchase.status" /></td>
                    <td class="action-cell">
                      <button
                        v-if="
                          purchase.status === 'PENDING' &&
                          ['manager', 'admin'].includes(user.role) &&
                          purchase.employee_id !== user.id
                        "
                        :id="`review-${purchase.id}`"
                        class="review-button"
                        :disabled="busy"
                        :aria-label="`Review ${purchase.item_name}, request ${purchase.id.slice(0, 8)}`"
                        :aria-expanded="selected?.id === purchase.id"
                        :aria-controls="
                          selected?.id === purchase.id
                            ? 'decision-panel'
                            : undefined
                        "
                        @click="review(purchase)"
                      >
                        Review<AppIcon name="arrow" /></button
                      ><span v-else class="final-label">Complete</span>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <footer class="table-footer">
              <span
                ><span
                  class="tiny-dot"
                  :class="
                    loadError
                      ? 'offline-dot'
                      : loaded
                        ? 'approved-dot'
                        : 'pending-dot'
                  "
                ></span
                >{{ loadError ? "Connection issue" : syncLabel }}</span
              ><span>Submitted dates are not exposed by the current API.</span>
            </footer>
          </section>
          <aside class="form-column" aria-label="Purchase and manager forms">
            <DecisionPanel
              :manager-id="user.id"
              v-if="selected"
              :key="selected.id"
              :purchase="selected"
              :disabled="busy"
              :submit-decision="decide"
              @close="closeReview()"
              @done="closeReview(true)"
            /><PurchaseForm
              :employee-id="user.id"
              :disabled="busy"
              :submit-request="create"
            />
            <PolicyAssistant />
            <div class="workflow-note">
              <span class="workflow-line" aria-hidden="true"
                >1 <i></i> 2 <i></i> 3</span
              >
              <h3>A clear path to a decision</h3>
              <p>
                Submit a request, review the details, then approve or reject.
              </p>
            </div>
          </aside>
        </div>
        <footer class="page-footer">
          <span>ProcureFlow AI</span><span>Local procurement demo · EUR</span>
        </footer>
      </main>
    </div>
  </div>
</template>
