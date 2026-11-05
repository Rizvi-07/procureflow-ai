import { ref } from "vue";
import {
  api,
  type CreatePurchase,
  type DecisionInput,
  type Purchase,
  type PurchaseApi,
} from "../lib/api";

export function usePurchases(client: PurchaseApi = api) {
  const purchases = ref<Purchase[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const mutating = ref(false);
  const loadError = ref("");
  const success = ref("");
  const lastSynced = ref<Date | null>(null);

  async function refresh() {
    if (loading.value || mutating.value) return;
    loading.value = true;
    loadError.value = "";
    try {
      purchases.value = await client.list();
      loaded.value = true;
      lastSynced.value = new Date();
    } catch (error) {
      loadError.value =
        error instanceof Error ? error.message : "Could not load purchases.";
    } finally {
      loading.value = false;
    }
  }

  async function create(input: CreatePurchase) {
    mutating.value = true;
    success.value = "";
    try {
      const purchase = await client.create(input);
      purchases.value = [
        purchase,
        ...purchases.value.filter((item) => item.id !== purchase.id),
      ];
      success.value = `${purchase.item_name} request saved. Status: ${purchase.status.toLowerCase()}.`;
      return purchase;
    } finally {
      mutating.value = false;
    }
  }

  async function decide(id: string, input: DecisionInput) {
    mutating.value = true;
    success.value = "";
    try {
      const decision = await client.decide(id, input);
      // Update only after the real gateway confirms the saved, propagated decision.
      purchases.value = purchases.value.map((item) =>
        item.id === id ? { ...item, status: decision.status } : item,
      );
      success.value = `Request ${id.slice(0, 8)} ${decision.status.toLowerCase()}.`;
    } finally {
      mutating.value = false;
    }
  }

  return {
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
  };
}
