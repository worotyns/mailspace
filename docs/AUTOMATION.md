# Space automation (JEV)

JEV here is Mailspace's built-in proposal/validation/apply contract, not a plugin system. Its TypeSafe Jev adapter uses the actual OpenRouter Decisions API; ordinary structured-output models have a separate chat-completions path. A model suggests zero or more existing Space IDs for one message. A deterministic gate checks ownership, account generation, Space revisions, settings revision and manual overrides before any assignment. Models cannot send mail, create permissions, run tools or execute rules. Classification is optional and costs provider tokens when enabled.

## Configure the operator adapter

Set `.env` using [CONFIGURATION.md](CONFIGURATION.md):

```dotenv
MAILSPACE_JEV_PROVIDER=openrouter
MAILSPACE_JEV_MODEL=typesafe/jev-1.13
MAILSPACE_JEV_API_TOKEN=your-private-token
MAILSPACE_JEV_KEY_FILE=
MAILSPACE_JEV_ENDPOINT=
```

Replace the token placeholder. `typesafe/jev-1.13` is a documented pinned model ID; choose an exact supported catalog ID. The namespace `typesafe-ai/jev-1.3` is incorrect for OpenRouter. Ordinary chat models instead need strict JSON-schema support. The requested routing policy is retained for both protocols. Set a provider spending limit. Alternatively leave API_TOKEN blank and configure a private, app-readable KEY_FILE, mode 0600. These are API credentials, never OpenPGP keys. Restart/recreate the service after changes: `docker compose up -d --build`.

For a local compatible service set PROVIDER=local and ENDPOINT to the complete `/v1/chat/completions` URL. Numeric loopback HTTP is allowed; another host/container needs verified HTTPS. Container loopback does not reach a different container. Local adapters do not imply privacy from the server operator. No automatic model download, proxy inheritance, redirect or weakened-privacy fallback is enabled.

Blank PROVIDER creates no classifier client or worker. A configured adapter alone sends no user mail: per-user mode defaults to Off. Changing the displayed provider/model/endpoint invalidates previous consent until the user approves the new destination.

## Describe Spaces and opt in

Create Spaces with specific relevance briefs: for example Payments (payment providers, settlement and publisher commercial terms), Commerce (e-commerce, loyalty and partner programmes), Personal (home, utilities, cinema and appointments). Briefs are sent with classification inputs; keep unrelated private details out of them. One message may match several Spaces; membership never changes the underlying IMAP mail or original reply thread.

Open **Space automation**:

| Mode | Behavior |
|---|---|
| Off | No provider requests. |
| Manual | Only explicit preview/selected-excerpt requests; no background classification. |
| Review | Background proposals await human approval. |
| Auto | Valid proposals at confidence >= 0.8 apply automatically; lower confidence remains for review. Confidence is a model claim, not proof. |

Choose **Metadata** (sender, recipients and subject) or **Ordinary text** (metadata plus up to 4 KiB UTF-8 body text). Every request also includes Space IDs, names and up to 512 bytes per relevance brief. Encrypted/unsupported/pending and your own messages are excluded from automatic processing. Manually organized messages are not automatically reassigned. Ordinary plaintext may include quoted sensitive text; review payload consent accordingly.

The worker checks the bounded recent mail cache once per minute, up to three new calls per owner per cycle. Limits: six new attempts per user per rolling minute, 5,000 retained runs per user, latest 50 displayed; at most 32 enabled owners scanned per cycle. This is a personal-installation alpha, not a bulk historical classifier. Existing/error attempts for an unchanged input are reused without rebilling; there is no automatic retry loop. A settings/Space revision change creates new inputs and can incur new calls.

## Preview, trace, replay and apply

**Classify preview (dry-run)** on a message records a proposal without assigning it. Review the trace in Space automation, then **Apply** to accept. Approval is a manual override against future automatic assignment. **Replay** revalidates stored references and current revisions: no model call, no reassignment. Changed source/settings/definitions/manual choices reject stale results. Applied decisions are idempotent. Automatic assignments replace only prior model Space memberships, preserving manual memberships and labels.

Traces retain owner/account/message references, IDs/revisions, provider consent binding, proposal IDs/confidence and state/timestamp. They contain no message subjects, bodies, Space briefs, prompts, raw provider responses or raw provider errors. A trace still reveals sensitive relationship metadata; protect database/backups. Proposals are untrusted and constrained to current owned Space IDs. No rationale containing email text is requested or stored.

## Explicit encrypted-message exception

An encrypted message is never decrypted or exported by the worker. Open its secure reader, decrypt locally, then choose **Classify selection into Spaces**. Select or type an excerpt (no full-message fallback), review the exact text and displayed provider/model, and approve that single request. Manual/Review/Auto must be enabled separately; global opt-in alone never authorizes a decrypted export.

Only the approved excerpt (1–4,096 UTF-8 bytes) plus Space definitions is relayed through the backend in memory, using the operator API token. Keys, passphrases, attachments, remaining plaintext and original headers are not submitted. No excerpt or content hash is retained by Mailspace. The request times out after 25 seconds; cancel/close before sending prevents export, while an already transmitted request cannot be recalled from the provider. Provider/operator infrastructure may have its own retention: do not enable proxy/body logging. OpenRouter requests specify [no data collection and ZDR routing](https://openrouter.ai/docs/guides/routing/provider-selection); this is not an independent provider privacy audit.

The result is always a dry-run, even in Auto. Apply it explicitly in Space automation. Replay does not re-export the excerpt. Lock/close/page hide clears the client selection UI. This narrowly scoped exception does not enable generic decrypted export to AI, search, rules or webhooks. See [ADR-0021](adr/0021.md).

## Troubleshooting

Unavailable adapter: check provider/model, exactly one token source and file ownership/0600. Errors are deliberately generic; do not turn on body/token logging. A busy, rate-limited or capacity-limited request can also fail safely. Invalid model JSON, invented IDs and unavailable privacy routes are rejected. If settings/Space/mail-account revisions changed, obtain a new preview; never bypass the gate. CI does not use a paid key. An explicitly opted-in synthetic live check is available through MAILSPACE_LIVE_JEV_KEY_FILE; it makes exactly two requests with no retries.

## TypeSafe Jev protocol

Jev is a decision model, not a generative chat model. For `typesafe/jev-*` (excluding Jev Router), Mailspace sends `state` plus one independent Noul relevance question per Space to fixed `https://openrouter.ai/api/alpha/decisions`. A message can match several Spaces. No chat prompts, response-format schema, tools or model-generated JSON are used on that path. Ordinary models retain the separate chat-completions adapter. No automatic substitution/fallback between models or protocols occurs.

For each Space, a probability >= 0.5 proposes membership. The proposal's confidence is the minimum certainty across all Space decisions (`p` for proposed membership, `1-p` otherwise), so one uncertain Space keeps the whole proposal in review below the 0.8 auto threshold. This is a conservative routing score, not Jev's separate Choice confidence or proof of calibration for your mail. Missing/extra answers, wrong types and invalid/out-of-range probabilities reject the proposal. Provider/model/protocol changes require new per-user consent. Prefer pinned model IDs to aliases that can change upstream.

Contract sources: [OpenRouter Jev guide](https://openrouter.ai/docs/guides/community/jev), [Decisions API](https://openrouter.ai/docs/api/api-reference/alphadecisions/submit-a-decisions-request). Requests retain no-collection/ZDR/parameter support requirements and explicitly disable provider fallback for Decisions. Availability with an actual paid account remains untested locally; rejected privacy routes do not silently weaken policy.

Incoming Unsorted is now the default triage queue; when all incoming cached messages have a Space, it shows inbox zero. All activity and Spaces retain the messages. Use **Preview Space suggestions** on an eligible message in any cached historical view. Even manually organized cached mail can be previewed explicitly, while automatic reassignment remains blocked by its manual epoch. Preview never starts a history import and requires separate approval to apply.

## Deterministic local rules

The independent [local rules](RULES.md) module needs no provider or API key. Saving/editing disables a rule; explicit enable covers cached and future eligible mail. It matches headers only and preserves manual organization. Rules do not enable Jev, inspect encrypted content or mutate remote mail. Jev may separately propose additional Spaces under its own saved consent.

## Interpreting failed checks

Each message uses one provider request containing every owned Space definition (up to 32); Jev evaluates each Space independently, so several can match. A batch checks up to three eligible messages. An unchanged stored result may be reused without a new request.

The UI distinguishes an already-running check, the local limit of six new attempts per user per minute, an outdated decision after source/Space/settings/manual changes, ineligible input, and provider failure. Failed attempts count toward the minute limit. Wait for the running check or minute window before trying again; outdated results need a fresh preview. Provider errors can mean a network failure, rejected HTTP request or invalid response, and do not prove credentials are wrong. There is no automatic retry or plaintext fallback. Raw provider errors and message content are never displayed as diagnostics.
