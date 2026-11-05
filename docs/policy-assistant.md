# Optional read-only policy RAG (Phase 08)

**Live OpenAI integration is UNVERIFIED in this implementation session.** No API
key/models were configured and no paid calls were made. Tests exercise real
pgvector retrieval, gRPC, and exact source-file citations with explicitly fake
embedding/LLM providers. They do not establish live model quality or account
compatibility. Procurement does not require this extension.

## Defaults and boundary

Normal `docker compose up -d --build --wait` starts only the MVP. The optional
Vue panel is collapsed and makes no automatic calls. With an empty gateway
`ASSISTANT_GRPC_TARGET`, POST `/api/assistant/ask` returns HTTP 200 with
`configured: false`, an explicit **Not configured** answer, and `sources: []`.
The service also reports Not configured if key, either model, or AI database
URL is missing. Invalid/unavailable optional infrastructure never gates MVP
startup. Provider/network failures become controlled errors on the assistant
route only.

`assistant.v1.AssistantService` exposes only `AnswerPolicy`. It has no purchase
clients, action RPCs, tools, or order table access. Its pool enforces
`default_transaction_read_only=on`; only `policy-import` writes `ai.*`.
Compose gives AI its own database, volume, and private network. The gateway
has no provider key or AI database URL; it separately owns identity storage via AUTH_DATABASE_URL. The procurement database image, data,
migrations, and ownership are unchanged.

The assistant can explain fictional policies, never create, approve, reject,
or reimburse purchases. A conservative keyword filter refuses explicit actions,
order-status queries, and off-topic questions before provider calls. It can
over-refuse legitimate questions and is not authentication. Future live order
lookup requires a **separate authenticated service design**, never status
inferred from documents. The MVP now uses local authenticated sessions and employee/manager roles. Policy questions require a valid session and CSRF token; they cannot bypass purchase permissions. See [authentication.md](authentication.md).

## Explicit opt-in (billable calls)

Use a funded OpenAI API account and a Responses model available to that account
with strict JSON-schema output support. No chat model is silently selected.
Keep keys in the shell or ignored local `.env`, never in source or `VITE_` vars.
From the repository root in PowerShell:

```powershell
$secureKey = Read-Host 'Local OpenAI API key' -AsSecureString
$env:OPENAI_API_KEY = [System.Net.NetworkCredential]::new('', $secureKey).Password
$env:EMBEDDING_MODEL = 'text-embedding-3-small'
$env:EMBEDDING_DIMENSIONS = '1536'
$env:CHAT_MODEL = Read-Host 'Responses model ID with structured-output support'
docker compose --profile assistant up -d --wait ai-postgres
if ($LASTEXITCODE -ne 0) { throw 'Optional database startup failed' }
docker compose --profile assistant build policy-import assistant-service
if ($LASTEXITCODE -ne 0) { throw 'Optional image build failed' }
# Explicitly applies AI migrations and embeds/upserts fictional policies.
docker compose --profile assistant run --rm policy-import
if ($LASTEXITCODE -ne 0) { throw 'Policy import failed' }
$env:ASSISTANT_GRPC_TARGET = 'assistant-service:50053'
docker compose --profile assistant up -d --wait assistant-service api-gateway
if ($LASTEXITCODE -ne 0) { throw 'Optional reader startup failed' }
# Generate local accounts first as described in authentication.md.
$account = Get-Content secrets/demo-credentials.json -Raw | ConvertFrom-Json | Where-Object username -eq 'employee.demo'
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$login = Invoke-RestMethod -Method Post -Uri http://localhost:8080/api/auth/login -WebSession $session -ContentType application/json -Body (@{username=$account.username;password=$account.password}|ConvertTo-Json)
$body = @{ question = 'What is the fictional meal reimbursement limit?' } | ConvertTo-Json
$answer = Invoke-RestMethod -Method Post -Uri http://localhost:8080/api/assistant/ask -WebSession $session -Headers @{'X-CSRF-Token'=$login.csrf_token} -ContentType application/json -Body $body
$answer | ConvertTo-Json -Depth 6
```

Open the optional panel at `http://localhost:5173`. A configured reader requires
an initialized index with the same model/dimensions; incompatible metadata or
missing schema fails its startup rather than inventing an answer. To publish
edited policy files, rebuild `policy-import` and rerun its import command.

Public DEVELOPMENT-ONLY defaults are user `policy_dev`, password
`policy_dev_only`, database `procureflow_policy_dev`. AI's named volume is
`procureflow-ai_ai_postgres_data`. No AI PostgreSQL/gRPC ports are published.
Only import/answer processes join the outbound `provider` network. They do not
join the procurement backend network. The gateway joins private `ai` for gRPC.

To disable, clear `ASSISTANT_GRPC_TARGET` in any root `.env` and the shell,
recreate the gateway, and stop optional containers, retaining both data volumes:

```powershell
Remove-Item Env:ASSISTANT_GRPC_TARGET -ErrorAction SilentlyContinue
# Ensure ASSISTANT_GRPC_TARGET= is empty in .env if that file is used.
docker compose up -d api-gateway
docker compose --profile assistant stop assistant-service ai-postgres
Remove-Item Env:OPENAI_API_KEY -ErrorAction SilentlyContinue
```

Native development uses a separate pgvector-enabled `AI_DATABASE_URL`, then
`go run ./cmd/policy-import` and `go run ./cmd/assistant-service`. Restart the
native gateway with `ASSISTANT_GRPC_TARGET=localhost:50053`. Go does not load
`.env` automatically. Compose supplies its own container database URL.

## Retrieval and evidence

Three FICTIONAL Markdown documents under `policies/` describe thresholds,
invented suppliers, and reimbursement. They are policy examples, not business
rules implemented by the MVP. Import accepts up to 10 top-level regular `.md`
files, 32 KiB each, and 64 total chunks. Symlinks/nonregular files are rejected.
UTF-8 chunks are at most 1200 bytes, with document names, heading/line-number
section identifiers, and section-local chunk indexes. Reimport after edits so
identifiers/quotes correspond to current source files.

The explicit importer applies `migrations/assistant/001_policy_chunks.sql` only
in the AI database, using a transaction, advisory lock, and checksum ledger.
`ai.policy_chunks.embedding` uses `vector(n)` from validated dimensions. Metadata
records one model/dimension pair. Supported embedding models are
`text-embedding-3-small` (1–1536 dimensions) and `text-embedding-3-large`
(1–3072), including supported reduced dimensions. Changing either requires a
separately provisioned empty AI index and full reimport. No vectors are padded,
randomly substituted, or mixed across models.

The adapter calls the maintained OpenAI HTTPS API directly: `/v1/embeddings`
for chunks/questions using the same configured model/dimensions, and
`/v1/responses` for generation. Wrong-model/dimension, zero-norm, missing, and
nonfinite embeddings are rejected. Unchanged chunks reuse stored embeddings.
After all new embeddings succeed, an atomic upsert publishes the corpus and
removes obsolete chunks; failed embedding leaves the old corpus intact.

Questions use parameterized pgvector cosine search, a model filter, top-K
(default 3, maximum 5), and maximum distance (default 0.35). This is a demo
threshold, not a calibrated relevance guarantee. Empty retrieval skips the LLM
and says **No supporting policy was found**. Generation receives only the
question, retrieved evidence, and fixed instructions. There is no full-corpus,
live-order, model-memory, or conversation-history evidence fallback.

Instructions label both question/documents untrusted, prohibit prompt overrides
and actions, and supply no tools. Structured output must identify retrieved
source numbers and quote exact nonempty substrings. The service validates them
and attaches document/section/chunk references itself. Invented citations fail
closed; unsupported answers abstain with no citations. This proves provenance,
**not semantic correctness of every generated claim**. Prompt injection and
hallucination remain model risks. Vue renders plain escaped text, never HTML.

Live acceptance requires `configured: true`, source/section/quote comparison
against `policies/`, an unsupported-question abstention, and action/order-status
refusals. Fake-provider tests alone cannot establish live RAG quality.

## Privacy, cost, and limits

Use synthetic content only. Import sends policy text, filenames, and section
names to OpenAI; asking sends a question for embedding and retrieved evidence
for generation. Content/embeddings persist in the AI volume. This application
does not persist questions/answers or log their contents. `store: false` on
Responses is **not** a zero-provider-retention promise; review current provider
data controls. Never send credentials, private records, or real employee data.

Each uncached chunk costs one embedding request; each supported question costs
one embedding request and at most one generation request. Four answers may run
concurrently. Limits: question 1000 UTF-8 bytes, embedding input 1600 bytes,
generation request JSON 16000 bytes, provider response 1 MiB, answer 4096 bytes.
These bound input size without assuming an exact tokenizer count. Output uses
`AI_MAX_OUTPUT_TOKENS` (default 512, range 64–1024, including reasoning tokens).
Import has a two-minute deadline. There are no hidden provider retries.

Default deadlines are provider 6s, assistant 8s, gateway assistant RPC 10s,
HTTP write 15s, and browser 12s. Earlier parent deadlines win. Slow models may
time out; if increasing limits, preserve that nesting. This loopback demo has
no per-user quota or monetary cap. Set account budgets, monitor usage, and add
per-user assistant quotas and global provider cost limits before any public deployment. Local sign-in throttling does not cap assistant spending.

## Tests without paid calls

```powershell
go test -v -count=1 ./internal/assistant ./internal/gateway
```

Unit tests inject fake providers or a local HTTP fixture. The pgvector test
requires `AI_TEST_DATABASE_URL` for a separate pgvector test server with CREATEDB.
It creates/drops only its own generated database and checks migration/model
consistency, import idempotency, read-only SQL, real cosine retrieval, and real
gRPC citations against source files. Missing/unavailable infrastructure reports
an explicit skip; failed migrations/assertions fail. No test calls a real model.
CI provisions a separate pgvector service alongside the four existing database
tests and rejects unexpected integration skips. Actual hosted results still
require a GitHub run.

References: official OpenAI [embeddings API](https://developers.openai.com/api/reference/resources/embeddings/methods/create),
[structured outputs](https://developers.openai.com/api/docs/guides/structured-outputs),
[Responses storage](https://developers.openai.com/api/docs/guides/migrate-to-responses),
and [pgvector](https://github.com/pgvector/pgvector).
