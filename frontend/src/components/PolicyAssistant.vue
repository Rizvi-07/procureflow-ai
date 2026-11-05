<script setup lang="ts">
import { ref } from "vue";
import { api, type PolicyAnswer } from "../lib/api";

const question = ref("");
const busy = ref(false);
const error = ref("");
const result = ref<PolicyAnswer | null>(null);
async function ask() {
  if (busy.value) return;
  error.value = "";
  result.value = null;
  if (
    !question.value.trim() ||
    new TextEncoder().encode(question.value).length > 1000
  ) {
    error.value = "Enter a policy question using 1–1000 UTF-8 bytes.";
    return;
  }
  busy.value = true;
  try {
    result.value = await api.askPolicy(question.value);
  } catch (failure) {
    error.value =
      failure instanceof Error
        ? failure.message
        : "Policy assistant unavailable. Procurement remains available.";
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <details class="policy-assistant">
    <summary>Optional policy assistant · read only</summary>
    <p>
      Fictional documents only. Cannot create, approve, reject, or look up live
      orders. No chat history is sent.
    </p>
    <p>
      Disabled by default; availability is checked when you send a question.
      Configured requests send the question and retrieved policy text to OpenAI.
    </p>
    <form @submit.prevent="ask" :aria-busy="busy">
      <label for="policy-question">Policy question</label>
      <textarea
        id="policy-question"
        v-model="question"
        maxlength="1000"
        rows="3"
        :disabled="busy"
        placeholder="What is the fictional meal reimbursement limit?"
        required
      ></textarea>
      <button type="submit" :disabled="busy">
        {{ busy ? "Checking policy…" : "Ask about policy" }}
      </button>
    </form>
    <p v-if="error" role="alert">{{ error }}</p>
    <div v-if="result" aria-live="polite" class="policy-answer">
      <p v-if="!result.configured"><strong>Not configured</strong></p>
      <p>{{ result.answer }}</p>
      <ul v-if="result.sources.length" aria-label="Retrieved policy sources">
        <li
          v-for="source in result.sources"
          :key="`${source.document}:${source.section}:${source.chunk_index}`"
        >
          <strong
            >{{ source.document }} · {{ source.section }} · chunk
            {{ source.chunk_index }}</strong
          >
          <blockquote>{{ source.quote }}</blockquote>
        </li>
      </ul>
    </div>
  </details>
</template>

<style scoped>
.policy-assistant {
  margin-top: 1rem;
  padding: 1.2rem;
  border: 1px solid #d8e2df;
  border-radius: 16px;
  background: white;
  color: #263e37;
}
summary {
  cursor: pointer;
  font-weight: 700;
}
p,
li {
  font-size: 0.85rem;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
label {
  display: block;
  font-size: 0.85rem;
  font-weight: 600;
}
textarea {
  width: 100%;
  box-sizing: border-box;
  margin: 0.5rem 0;
  padding: 0.6rem;
  font: inherit;
  border: 1px solid #7c9189;
  border-radius: 8px;
}
button {
  padding: 0.6rem 0.9rem;
  border: none;
  border-radius: 8px;
  background: #165d4b;
  color: white;
  font: inherit;
  cursor: pointer;
}
button:disabled {
  opacity: 0.65;
  cursor: wait;
}
summary:focus-visible,
textarea:focus-visible,
button:focus-visible {
  outline: 3px solid #197d63;
  outline-offset: 3px;
}
blockquote {
  margin: 0.5rem 0;
  padding-left: 0.7rem;
  border-left: 3px solid #a4c5b9;
  white-space: pre-wrap;
}
.policy-answer {
  white-space: pre-wrap;
}
</style>
