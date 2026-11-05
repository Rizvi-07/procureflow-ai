<script setup lang="ts">
import type { PurchaseStatus } from "../lib/api";
defineProps<{ status: PurchaseStatus }>();
const labels = {
  PENDING: "Pending review",
  APPROVED: "Approved",
  REJECTED: "Rejected",
};
</script>

<template>
  <span class="status-badge" :class="status.toLowerCase()"
    ><span class="status-dot" aria-hidden="true"></span
    >{{ labels[status] }}</span
  >
</template>
