<script setup lang="ts">
import { nextTick, onMounted, ref } from "vue";
import type { Decision, DecisionInput, Purchase } from "../lib/api";
import { formatEuro } from "../lib/money";
import AppIcon from "./AppIcon.vue";

const props = defineProps<{
  purchase: Purchase;
  disabled: boolean;
  submitDecision: (id: string, input: DecisionInput) => Promise<void>;
}>();
const emit = defineEmits<{ close: []; done: [] }>();
const manager = ref("manager-demo");
const comment = ref("");
const error = ref("");
const working = ref<Decision | null>(null);
const managerInput = ref<HTMLInputElement | null>(null);
onMounted(async () => {
  await nextTick();
  managerInput.value?.focus();
});

async function decide(decision: Decision) {
  if (props.disabled || working.value) return;
  error.value = "";
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(manager.value.trim())) {
    error.value =
      "Enter a demo manager ID using letters, digits, underscores or hyphens, starting alphanumeric.";
    managerInput.value?.focus();
    return;
  }
  if (Array.from(comment.value).length > 1000 || comment.value.includes("\0")) {
    error.value =
      "Comment must contain at most 1000 characters and no NUL characters.";
    return;
  }
  if (props.purchase.status !== "PENDING") {
    error.value = "This request is already decided. Refresh the purchase list.";
    return;
  }
  working.value = decision;
  try {
    await props.submitDecision(props.purchase.id, {
      manager_id: manager.value.trim(),
      decision,
      comment: comment.value,
    });
    emit("done");
  } catch (cause) {
    error.value =
      cause instanceof Error
        ? cause.message
        : "The decision could not be confirmed.";
  } finally {
    working.value = null;
  }
}
</script>

<template>
  <section
    id="decision-panel"
    class="card decision-card"
    aria-labelledby="decision-heading"
  >
    <div class="section-heading">
      <div>
        <p class="eyebrow">Manager review</p>
        <h2 id="decision-heading">Make a decision</h2>
      </div>
      <button
        type="button"
        class="icon-button"
        aria-label="Close manager review"
        :disabled="!!working"
        @click="emit('close')"
      >
        <AppIcon name="close" />
      </button>
    </div>
    <div class="review-summary">
      <strong>{{ purchase.item_name }}</strong
      ><span
        >{{ formatEuro(purchase.total_cents) }} ·
        {{ purchase.quantity }} units</span
      ><span class="mono">{{ purchase.id }}</span>
    </div>
    <p class="field-hint demo-label">
      Demo identity only. Manager ID and comment are not verified or retained.
    </p>
    <form novalidate @submit.prevent>
      <fieldset :disabled="disabled || !!working">
        <legend class="sr-only">Manual manager decision</legend>
        <div class="field">
          <label for="manager-id">Demo manager ID</label
          ><input
            id="manager-id"
            ref="managerInput"
            v-model="manager"
            name="manager_id"
            required
            maxlength="128"
            autocomplete="off"
            aria-describedby="decision-caveat"
          />
        </div>
        <div class="field">
          <label for="decision-comment"
            >Comment <span class="optional">(optional)</span></label
          ><textarea
            id="decision-comment"
            v-model="comment"
            name="comment"
            rows="3"
            maxlength="1000"
            placeholder="A short reason for your decision"
            aria-describedby="decision-caveat"
          ></textarea>
        </div>
      </fieldset>
      <p id="decision-caveat" class="field-hint">
        This demo does not store manager IDs or comments as an audit record.
      </p>
      <div v-if="error" class="inline-error" role="alert">
        <strong>Decision not confirmed</strong>
        <p>{{ error }}</p>
        <p>
          If a service timed out, retry the same decision to finish
          synchronization. Refresh to check the current state.
        </p>
      </div>
      <div class="decision-buttons">
        <button
          class="button primary"
          type="button"
          :disabled="disabled || !!working"
          @click="decide('APPROVED')"
        >
          <AppIcon name="check" />{{
            working === "APPROVED" ? "Approving…" : "Approve"
          }}</button
        ><button
          class="button danger-outline"
          type="button"
          :disabled="disabled || !!working"
          @click="decide('REJECTED')"
        >
          {{ working === "REJECTED" ? "Rejecting…" : "Reject" }}
        </button>
      </div>
    </form>
  </section>
</template>
