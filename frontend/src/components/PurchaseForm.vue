<script setup lang="ts">
import { computed, nextTick, reactive, ref } from "vue";
import { ApiError, type CreatePurchase, type Purchase } from "../lib/api";
import {
  eurosToCents,
  formatEuro,
  parseQuantity,
  totalCents,
} from "../lib/money";
import AppIcon from "./AppIcon.vue";

const props = defineProps<{
  disabled: boolean;
  submitRequest: (input: CreatePurchase) => Promise<Purchase>;
}>();
const fields = reactive({ employee: "", item: "", quantity: "1", price: "" });
const errors = reactive<Record<string, string>>({});
const requestError = ref("");
const notice = ref("");
const working = ref(false);
const attempt = ref<CreatePurchase | null>(null);
const form = ref<HTMLFormElement | null>(null);
const idPattern = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/;

const preview = computed(() => {
  try {
    return formatEuro(
      totalCents(parseQuantity(fields.quantity), eurosToCents(fields.price)),
    );
  } catch {
    return "—";
  }
});

async function submit() {
  if (props.disabled || working.value) return;
  requestError.value = "";
  notice.value = "";
  for (const key of Object.keys(errors)) delete errors[key];
  if (!attempt.value) {
    if (!idPattern.test(fields.employee.trim()))
      errors.employee =
        "Use 1–128 letters, digits, underscores or hyphens, starting with a letter or digit.";
    const item = fields.item.trim();
    if (!item || Array.from(item).length > 200 || item.includes("\0"))
      errors.item =
        "Enter an item name of 1–200 characters without NUL characters.";
    let quantity = 0,
      cents = 0;
    try {
      quantity = parseQuantity(fields.quantity);
    } catch (error) {
      errors.quantity = (error as Error).message;
    }
    try {
      cents = eurosToCents(fields.price);
    } catch (error) {
      errors.price = (error as Error).message;
    }
    if (!errors.quantity && !errors.price) {
      try {
        totalCents(quantity, cents);
      } catch (error) {
        errors.price = (error as Error).message;
      }
    }
    if (Object.keys(errors).length) {
      await nextTick();
      form.value
        ?.querySelector<HTMLInputElement>('[aria-invalid="true"]')
        ?.focus();
      return;
    }
    attempt.value = {
      client_request_id: crypto.randomUUID(),
      employee_id: fields.employee.trim(),
      item_name: item,
      quantity,
      unit_price_cents: cents,
    };
  }
  working.value = true;
  try {
    await props.submitRequest({ ...attempt.value! });
    attempt.value = null;
    fields.item = "";
    fields.quantity = "1";
    fields.price = "";
  } catch (error) {
    requestError.value =
      error instanceof Error
        ? error.message
        : "The request could not be submitted.";
    // An ambiguous response can hide a committed write. Preserve its exact key.
    if (
      !(error instanceof ApiError) ||
      (!error.retryable && error.code !== "INVALID_RESPONSE")
    )
      attempt.value = null;
  } finally {
    working.value = false;
  }
}

function editNew() {
  attempt.value = null;
  requestError.value = "";
  notice.value =
    "The previous request may already exist. Refresh and review your requests before submitting different details.";
}
</script>

<template>
  <section class="card form-card" aria-labelledby="request-form">
    <div class="section-heading">
      <span class="icon-tile"><AppIcon name="plus" /></span>
      <div>
        <h2 id="request-form" tabindex="-1">New purchase request</h2>
        <p>Start with the essentials.</p>
      </div>
    </div>
    <form ref="form" novalidate @submit.prevent="submit">
      <fieldset :disabled="disabled || working">
        <legend class="sr-only">Purchase request details</legend>
        <div class="field">
          <label for="employee-id">Employee ID</label>
          <input
            id="employee-id"
            v-model="fields.employee"
            name="employee_id"
            placeholder="e.g. emp-101"
            autocomplete="off"
            maxlength="128"
            required
            :readonly="!!attempt"
            :aria-invalid="!!errors.employee"
            :aria-describedby="errors.employee ? 'employee-error' : undefined"
          />
          <p v-if="errors.employee" id="employee-error" class="field-error">
            {{ errors.employee }}
          </p>
        </div>
        <div class="field">
          <label for="item-name">Item name</label>
          <input
            id="item-name"
            v-model="fields.item"
            name="item_name"
            placeholder="What do you need?"
            maxlength="200"
            required
            :readonly="!!attempt"
            :aria-invalid="!!errors.item"
            :aria-describedby="errors.item ? 'item-error' : undefined"
          />
          <p v-if="errors.item" id="item-error" class="field-error">
            {{ errors.item }}
          </p>
        </div>
        <div class="field-row">
          <div class="field">
            <label for="quantity">Quantity</label>
            <input
              id="quantity"
              v-model="fields.quantity"
              name="quantity"
              inputmode="numeric"
              required
              :readonly="!!attempt"
              :aria-invalid="!!errors.quantity"
              :aria-describedby="errors.quantity ? 'quantity-error' : undefined"
            />
            <p v-if="errors.quantity" id="quantity-error" class="field-error">
              {{ errors.quantity }}
            </p>
          </div>
          <div class="field">
            <label for="unit-price">Unit price (EUR)</label>
            <input
              id="unit-price"
              v-model="fields.price"
              name="unit_price_eur"
              inputmode="decimal"
              placeholder="0.00"
              required
              :readonly="!!attempt"
              :aria-invalid="!!errors.price"
              :aria-describedby="
                errors.price ? 'price-help price-error' : 'price-help'
              "
            />
            <p v-if="errors.price" id="price-error" class="field-error">
              {{ errors.price }}
            </p>
          </div>
        </div>
        <p id="price-help" class="field-hint">
          Use a dot or comma and up to two decimal places. No thousands
          separators.
        </p>
      </fieldset>
      <div class="total-preview">
        <span>Request total</span
        ><output aria-live="polite">{{ preview }}</output>
      </div>
      <div v-if="requestError" class="inline-error" role="alert">
        <strong>Request not confirmed</strong>
        <p>{{ requestError }}</p>
        <p v-if="attempt">
          Your details are kept unchanged. Retry sends the same request key, so
          it can repair an incomplete submission.
        </p>
      </div>
      <p v-if="notice" class="inline-notice" role="status">{{ notice }}</p>
      <button
        class="button primary full-width"
        type="submit"
        :disabled="disabled || working"
      >
        <AppIcon
          :name="working ? 'refresh' : 'arrow'"
          :class="{ spinning: working }"
        />{{
          working ? "Submitting…" : attempt ? "Retry request" : "Submit request"
        }}
      </button>
      <button
        v-if="attempt && requestError"
        class="button text-button full-width"
        type="button"
        :disabled="disabled || working"
        @click="editNew"
      >
        Edit as a new request
      </button>
      <p class="form-footnote">Requests are submitted for manager review.</p>
    </form>
  </section>
</template>
