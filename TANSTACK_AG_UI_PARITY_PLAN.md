# TanStack/AG-UI Parity Implementation Brief

## Summary

Build dummybridge AI around current TanStack AG-UI primitives in both directions:

- dummybridge emits AG-UI stream events and accepts TanStack-shaped approval responses.
- Desktop consumes multi-event encrypted AG-UI streams and hides carrier events from the normal timeline.
- Shared Go packages define the primitive contract instead of preserving old AI SDK or agentremote decisions.

Do not install dependencies or modify lockfiles unless the user explicitly approves that dependency change. `@tanstack/ai-react-ui` is approved for the Desktop rendering work in this plan.

## Current State

The dummybridge repo currently has a provisional `pkg/aichats` package and AI handling in `pkg/connector/client.go`.

Known current behavior:

- AI DM resolution uses the `ai`/`AI` ghost and AI portals with the `ai-` prefix.
- The bridge sends one visible placeholder event, streams `com.beeper.llm.deltas`, then edits the placeholder with final content.
- Current deltas are AG-UI-like but incomplete.
- Current approval requests are separate Matrix events with `com.beeper.ai.approval` metadata and reaction options.
- Current approval reaction handling should keep the user's selected emoji and remove the bridge-posted placeholder/non-selected options, but this needs robust implementation and tests.
- Current text streaming has started moving away from full accumulated content on each delta, but the final design must enforce that.

Desktop already has partial AI stream support in these areas:

- `src/common/ai-common.ts`: `BeeperAIMessage`, `BeeperAGUIEvent`, approval constants, and type guards.
- `src/common/types/beeper.ts`: stream content types with `.deltas` and `updates`.
- `src/pas-server/beeper/EventSyncContext.ts`: maps `com.beeper.ai`, `com.beeper.stream`, per-message profile, edits, and hidden AI notices.
- `src/pas-server/beeper/BeeperClient.ts`: processes stream events into `STATE_SYNC message stream`.
- `src/renderer/stores/AIChatsStore.ts`: extracts `.deltas`, orders by `seq`, applies AG-UI events, tracks approvals, and merges stream state.
- `src/renderer/ai/ui-message.ts`: applies AG-UI events into current Desktop UI message parts.

The implementation should update those Desktop paths instead of inventing a second client stream path.

Implementation rules:

- Keep the code simple, clean, and direct.
- Prefer less LOC, less indirection, and fewer abstractions.
- Fold or flatten abstractions that do not carry real behavior.
- Do not add fake layers, simple wrappers, barrel exports, duplicated logic, or duplicated types.
- Smaller files are fine only when they represent real concerns.
- Optimize for one coherent system per concern, not multiple parallel ways to do the same thing.
- Current AI code was generated and never released, so no backward compatibility or legacy compatibility is required.
- Delete provisional schemas, routes, event shapes, migrations, aliases, and helper layers if they only exist for history or compatibility.
- Prefer deleting code over preserving it.
- Prefer collapsing duplicate entrypoints over keeping aliases.
- Product intention matters more than the current code shape.
- If product intent is ambiguous, explicitly call out the question instead of encoding both options.

Compatibility policy:

- No compatibility policy is required for the provisional/current dummybridge AI event shapes.
- Desktop and dummybridge should converge on one new TanStack/AG-UI shape.
- Delete old reader/writer paths instead of accepting old names such as `REASONING_MESSAGE_*` or older tool-call fields unless they are required by current TanStack AG-UI docs.

## Full Paths To Inspect

Primary dummybridge checkout:

- `/Users/batuhan/Projects/labs/dummybridge`
- `/Users/batuhan/Projects/labs/dummybridge/TANSTACK_AG_UI_PARITY_PLAN.md`
- `/Users/batuhan/Projects/labs/dummybridge/README.md`
- `/Users/batuhan/Projects/labs/dummybridge/go.mod`
- `/Users/batuhan/Projects/labs/dummybridge/go.sum`
- `/Users/batuhan/Projects/labs/dummybridge/config-agui.yaml`
- `/Users/batuhan/Projects/labs/dummybridge/config-qa-agui.yaml`

Current dummybridge AI implementation to replace:

- `/Users/batuhan/Projects/labs/dummybridge/pkg/aichats/agui.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/aichats/matrix.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/aichats/agui_test.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/connector/client.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/connector/connector.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/connector/login.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/connector/example-config.yaml`

New dummybridge package targets:

- `/Users/batuhan/Projects/labs/dummybridge/pkg/ag-ui`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/ai-stream`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/ai-stream/matrix`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/ai-stream/bridgev2`

Archived AI dummybridge reference:

- `/Users/batuhan/Projects/labs/ai-bridge-archived`
- `/Users/batuhan/Projects/labs/ai-bridge-archived/bridges/dummybridge/runtime.go`
- `/Users/batuhan/Projects/labs/ai-bridge-archived/bridges/dummybridge/runtime_test.go`
- `/Users/batuhan/Projects/labs/ai-bridge-archived/sdk/writer.go`
- `/Users/batuhan/Projects/labs/ai-bridge-archived/approval_flow.go`

Local TanStack AI reference checkout:

- `/Users/batuhan/Projects/labs/upstream/tanstack-ai`
- `/Users/batuhan/Projects/labs/upstream/tanstack-ai/packages/typescript/ai-react-ui/src/text-part.tsx`
- `/Users/batuhan/Projects/labs/upstream/tanstack-ai/packages/typescript/ai-react-ui/src/chat-message.tsx`
- `/Users/batuhan/Projects/labs/upstream/tanstack-ai/packages/typescript/ai-react-ui/package.json`
- `/Users/batuhan/Projects/labs/upstream/tanstack-ai/packages/typescript/ai-client/src/types.ts`
- `/Users/batuhan/Projects/labs/upstream/tanstack-ai/packages/typescript/ai-client/src/chat-client.ts`
- `/Users/batuhan/Projects/labs/upstream/tanstack-ai/packages/typescript/ai-client/src/connection-adapters.ts`
- `/Users/batuhan/Projects/labs/upstream/tanstack-ai/packages/typescript/ai-event-client/src/index.ts`
- `/Users/batuhan/Projects/labs/upstream/tanstack-ai/packages/typescript/ai/src/types.ts`
- `/Users/batuhan/Projects/labs/upstream/tanstack-ai/packages/typescript/ai/src/utilities/chat-params.ts`

Desktop checkout and AI consumer paths:

- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/common/ai-common.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/common/ai-common.test.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/common/types/beeper.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/pas-server/beeper/EventSyncContext.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/pas-server/beeper/BeeperClient.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/pas-server/beeper/connect/ws-event-mapper.test.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/pas-server/beeper/connect/ws-events-server.test.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/renderer/stores/AIChatsStore.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/renderer/ai/ui-message.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/renderer/ai/ui-message.test.ts`
- `/Users/batuhan/Projects/texts/beeper-workspace/beeper/beeper/desktop/src/renderer/ai/ai-message-view.ts`

Local tooling for live smoke tests:

- `/Users/batuhan/Projects/texts/bridge-manager/bbctl`
- `/Users/batuhan/Projects/labs/desktop-api-cli/packages/cli`

Local runtime artifacts that may be useful for debugging, but should not be treated as source:

- `/Users/batuhan/Projects/labs/dummybridge/logs`
- `/Users/batuhan/Projects/labs/dummybridge/sh-dummybridge-agui.db`
- `/Users/batuhan/Projects/labs/dummybridge/sh-dummybridge-agui.db-shm`
- `/Users/batuhan/Projects/labs/dummybridge/sh-dummybridge-agui.db-wal`
- `/Users/batuhan/Projects/labs/dummybridge/sh-dummybridge-qa-agui.db`

## TanStack/AG-UI Contract

Use TanStack primitives as the source of truth:

- `StreamChunk = AGUIEvent`; do not preserve legacy non-AG-UI chunk formats.
- Support every current AG-UI lifecycle event explicitly:
  - `RUN_STARTED`
  - `RUN_FINISHED`
  - `RUN_ERROR`
  - `TEXT_MESSAGE_START`
  - `TEXT_MESSAGE_CONTENT`
  - `TEXT_MESSAGE_END`
  - `TOOL_CALL_START`
  - `TOOL_CALL_ARGS`
  - `TOOL_CALL_END`
  - `TOOL_CALL_RESULT`
  - `STEP_STARTED`
  - `STEP_FINISHED`
  - `STATE_SNAPSHOT`
  - `STATE_DELTA`
  - `MESSAGES_SNAPSHOT`
  - `CUSTOM`
- Support bidirectional AG-UI run input: `threadId`, `runId`, `state`, `messages`, `tools`, `context`, `forwardedProps`, and legacy `data` mirror.
- Model `UIMessage` as `{ id, role, parts, createdAt? }`, preserving ordered parts.
- Use TanStack part shapes:
  - Text part: `{ type: "text", content }`
  - Thinking part: `{ type: "thinking", content }`
  - Tool call part: `{ type: "tool-call", id, name, arguments, state, approval?, output? }`
  - Tool result part: `{ type: "tool-result", toolCallId, content, state, error? }`
- Use TanStack tool states:
  - `awaiting-input`
  - `input-streaming`
  - `input-complete`
  - `approval-requested`
  - `approval-responded`
- Use TanStack tool result states:
  - `streaming`
  - `complete`
  - `error`
- Treat AG-UI `REASONING_START`, `REASONING_MESSAGE_START`, `REASONING_MESSAGE_CONTENT`, `REASONING_MESSAGE_END`, and `REASONING_END` as the canonical thinking/reasoning stream for new output.
- Keep `STEP_STARTED` / `STEP_FINISHED` as step lifecycle events using AG-UI `stepName`, not deprecated `stepId`, and not as a substitute for reasoning content.
- Fully support AG-UI `STATE_SNAPSHOT`, `STATE_DELTA`, and `MESSAGES_SNAPSHOT` events.
- Every emitted AG-UI event must include `timestamp`.
- Support optional AG-UI `rawEvent` on every event, with the bounded/truncation policy below.
- Support `TOOL_CALL_START.index` for parallel tool calls.
- Support partial JSON argument streaming through `TOOL_CALL_ARGS`; consumers should preserve partial input while parsing best-effort and finalize on `TOOL_CALL_END`.
- Support `TOOL_CALL_END` both with and without a result payload.
- Support AG-UI `TOOL_CALL_RESULT` for separate tool-result parts instead of Beeper custom tool-result events.
- Support multiple assistant `messageId`s per run. Do not assume a run has exactly one assistant text message.

Relevant docs:

- AG-UI event definitions: <https://tanstack.com/ai/latest/docs/protocol/chunk-definitions>
- Streaming: <https://tanstack.com/ai/latest/docs/chat/streaming>
- Tool states and parts: <https://tanstack.com/ai/latest/docs/reference/type-aliases/ToolCallState>
- UIMessage: <https://tanstack.com/ai/latest/docs/reference/interfaces/UIMessage>
- Bidirectional AG-UI compliance: <https://tanstack.com/blog/ag-ui-compliance>
- Local source tags currently include `@tanstack/ai@0.18.0`, `@tanstack/ai-client@0.10.0`, and `@tanstack/ai-event-client@0.3.2`; prefer the local checkout above for exact type names during implementation.

## Package Layout

Create `pkg/ag-ui/` with Go package name `agui`.

Responsibilities:

- Standalone AG-UI event and UI message types.
- `RunAgentInput` and bidirectional request types.
- Tool, tool result, approval, text, thinking, step, custom, run, and error event builders.
- Validation helpers that reject invalid event ordering, missing IDs, bad states, invalid tool approval shapes, and oversized individual deltas.
- No Matrix, bridgev2, Desktop, or dummybridge-specific dependencies.

Create `pkg/ai-stream/` with Go package name `aistream`.

Responsibilities:

- Run writer for ordered AG-UI event emission.
- Accumulation used only for finalization, preview generation, and test reconstruction.
- Stream envelope and chunk packing helpers.
- Approval resolver primitives.
- Terminal/finalization helpers.
- Spec enforcement, but not transport ownership.

Add adapter layers:

- `pkg/ai-stream/matrix`: Matrix content helpers using mautrix event types, stream carrier content, approval prompt content, reaction option serialization.
- `pkg/ai-stream/bridgev2`: bridgev2 queue/send/redaction adapter. This layer may import bridgev2 and database types.

Delete `pkg/aichats` once the new packages fully replace it. Do not keep it as an unused compatibility package.

## Archived Dummybridge Parity

Use `../ai-bridge-archived/bridges/dummybridge/runtime.go` and `runtime_test.go` as the feature checklist, not as an architecture to copy.

Commands:

- `help`
- `/help`
- `!help`
- `dummybridge help`
- `stream-lorem <chars> [common options]`
- `stream-tools <chars> <tool[#fail|#approval|#deny|#delta|#inputerror|#prelim|#provider]>... [common options]`
- `stream-random [seconds] [--actions=N] [--profile=balanced|tools|artifacts|terminals] [--seed=N] [--delay-ms=min:max] [--allow-abort] [--allow-error] [--allow-approval]`
- `stream-chaos [runs] [seconds] [--profile=balanced|tools|artifacts|terminals] [--seed=N] [--stagger-ms=min:max] [--max-actions=N] [--allow-abort] [--allow-error] [--allow-approval]`

The help aliases are intentional product/demo affordances and should remain unless there is a later product decision to reduce command aliases.

Common options:

- `--reasoning=N`
- `--steps=N`
- `--sources=N`
- `--documents=N`
- `--files=N`
- `--meta`
- `--data=name`
- `--data-transient=name`
- `--delay-ms=min:max`
- `--chunk-chars=min:max`
- `--seed=N`
- `--finish=stop|length|tool-calls|content-filter|other`
- `--abort`
- `--error`

Tool tags:

- `#fail`
- `#approval`
- `#deny`
- `#delta`
- `#inputerror`
- `#prelim`
- `#provider`

Behavior to preserve:

- `stream-lorem` emits markdown-rich visible text, optional thinking/reasoning, optional steps, optional sources/documents/files/data, and a final run state.
- `stream-tools` emits text, thinking, tool input streaming, input errors, approval requests, approval denials, tool output streaming, final output, and tool failures.
- `stream-random` emits weighted random actions with deterministic seed support and profiles.
- `stream-chaos` starts multiple staggered runs and runs random streams per run.
- Persistent data survives final snapshots; transient data does not.
- Markdown generation must include realistic links, lists, quotes, code blocks, and tables.
- Terminal states include normal finish, error, and abort.

Limits should start from the archived limits, except the explicit over-64KB streaming tests require larger generated output support:

- Archived default chunk range: 24 to 96 characters.
- Archived maximum chunk size option: 512 characters.
- Archived maximum random actions: 64.
- Archived maximum chaos runs: 16.
- Archived maximum chaos actions: 64.
- Archived maximum demo duration: 5 minutes.
- Archived maximum delay/stagger: 30 seconds.
- Increase text generation limits enough to test at least 70KiB output. The transport must handle this by splitting carrier events, not by sending oversized Matrix events.

## Streaming Transport

Every AI run starts with one visible Matrix anchor event.

Anchor event requirements:

- `msgtype: m.text`
- AI per-message profile for the AI ghost
- Minimal `com.beeper.ai`
- Stable AG-UI `threadId`
- Stable AG-UI `runId`
- Stable AG-UI `messageId`
- Useful preview text in `body`
- `com.beeper.stream` descriptor when using the Beeper stream publisher

ID model:

- Use AG-UI IDs for semantic identity.
- `threadId` is the conversation/thread identity. For dummybridge this should map to the Beeper thread/portal/room identity used by Desktop.
- `runId` is the assistant execution identity. Do not add a separate Beeper execution ID unless a future AG-UI version requires it.
- `messageId` is the AG-UI assistant UI message identity. It should map to the first visible/anchor message, not to every carrier.
- Matrix event IDs are transport identities. Use the anchor Matrix event ID as `target_event` / `m.relates_to.event_id` for carriers.
- The Beeper stream descriptor is identified by `(room_id, event_id, type)` and does not expose a separate stream ID. Do not invent `streamId`; use `target_event` plus `runId` for merging.

Carrier events:

- Are sent through bridgev2 remote events so E2EE works normally.
- Must never be raw Matrix sends.
- Are `m.room.message` events with `msgtype: m.text` for bridgev2 and client compatibility.
- Contain `com.beeper.llm.deltas`.
- Carry ordered AG-UI envelopes.
- Are hidden from normal chat rendering by Desktop after deltas are extracted.
- Use empty or minimal body text after the initial visible preview; they must not appear as chat bubbles in Desktop.

Envelope shape:

- `threadId`
- `runId`
- `messageId`
- `seq`
- `part`
- `target_event` or `m.relates_to.event_id`
- optional `agent_id`

Ordering and merge key:

- `seq` is strictly increasing per `{target_event, runId}`.
- If `target_event` is unavailable during early processing, temporarily key by `{threadId, runId}` and promote to `{target_event, runId}` when the anchor message is known.
- Desktop buffers out-of-order deltas within existing ordering limits.
- Duplicate or stale `seq` values are ignored or rejected consistently.

Size budget:

- Treat 64KB as the external ceiling.
- Use a hard carrier budget of 58KB for serialized Matrix content to leave buffer for encryption overhead, wrappers, event metadata, and implementation variance.
- The packer must measure serialized JSON byte size before adding an envelope to a carrier.
- If a single text delta would exceed the 58KB carrier budget, split it at UTF-8 rune boundaries.
- If a non-text event cannot fit inside the 58KB budget, return a validation error rather than sending it.
- `rawEvent` must be optional, bounded, and safe to omit. If including `rawEvent` would push a carrier over budget, truncate it or drop it before packing rather than bloating the event.
- Truncated raw provider data must be marked, e.g. `rawEventTruncated: true`, so debugging does not confuse partial raw data with complete provider payloads.

Preview/body algorithm:

- The first visible message is the canonical message for the run.
- Put as much useful early visible preview as practical into the first message while preserving required metadata and staying under the 58KB budget.
- All run-level metadata that should survive as the message identity, such as model, usage, thread/run/message IDs, terminal state, and approval summary, belongs on the first visible message or its compact final metadata.
- Later carrier messages should be hidden and merged by compatible clients into the first visible message.
- Later carrier bodies should be empty or minimal and put payload in `.deltas`.
- Compatible clients must reconstruct from ordered deltas and merge content/parts into the first message, not display carriers as separate runs.
- Do not rewrite full accumulated content on every delta.

Finalization:

- The run accumulator is only for finalization, preview generation, and tests.
- Finalization emits compact terminal metadata and a compact final UI state when needed.
- Do not require a final Matrix edit containing the full generated body for over-64KB runs.
- The client is responsible for merging the stream.

Replay/backfill:

- Desktop must be able to reconstruct a run from persisted anchor plus persisted carrier messages, not only from live stream events.
- Replay must use the same merge key and ordering rules as live streaming.
- Backfilled carrier events should remain hidden after extraction.

Redaction/delete behavior:

- If a carrier is deleted/redacted, Desktop should recompute the visible anchor from remaining carrier events when possible.
- If recomputation leaves a sequence gap or invalid stream, mark the anchor message incomplete/failed.
- Approval prompt deletion/redaction should not delete or corrupt the AI run; it only removes that visible prompt.

Ordering gap timeout:

- Do not buffer missing `seq` gaps forever.
- If a gap remains unresolved past the configured timeout, mark the first visible anchor message incomplete/failed and keep carrier messages hidden.
- Late arrivals after failure should not create separate visible carrier messages.

AG-UI state events:

- Fully support `STATE_SNAPSHOT`, `STATE_DELTA`, and `MESSAGES_SNAPSHOT` as first-class AG-UI events.
- `STATE_SNAPSHOT` replaces the current run/application state view for the AI run.
- `STATE_DELTA` applies an incremental patch/update to that state.
- `MESSAGES_SNAPSHOT` carries a complete AG-UI `UIMessage[]` snapshot.
- Desktop must preserve and expose this state for AI rendering/devtools instead of dropping it.
- State events are allowed to affect rendered state when the renderer intentionally consumes them.
- State events must still obey the 58KB carrier budget and multi-carrier splitting rules.
- Do not duplicate the normal streaming path: text should still prefer text events, tool calls should still prefer tool events, and state events should be used when AG-UI state synchronization is the right primitive.

Run errors:

- `RUN_ERROR` may be run-scoped or session/thread-scoped.
- If `RUN_ERROR.runId` is present, fail only that run.
- If `RUN_ERROR.runId` is absent, fail active runs in that thread/session.
- Desktop should surface the failure on the first visible anchor message for each affected run and keep carrier messages hidden.

First message metadata schema:

- The first visible message must contain enough metadata for a compatible client to render run chrome, status, model info, usage, approvals, and non-part attachments without reading carrier event metadata.
- Do not put streamed UI parts, text chunks, thinking chunks, tool argument chunks, or tool output chunks in this metadata.
- AG-UI/TanStack-mirrored fields must use TanStack naming and value shapes: `threadId`, `runId`, `messageId`, `finishReason`, `promptTokens`, `completionTokens`, and `totalTokens`.
- Beeper-only fields should be grouped clearly under Beeper-owned names instead of changing AG-UI concepts.
- Suggested `com.beeper.ai.metadata` shape:

```json
{
  "schema": "com.beeper.ai.run.v1",
  "protocol": "ag-ui",
  "threadId": "thread-id",
  "runId": "run-id",
  "messageId": "message-id",
  "agent": {
    "id": "ai",
    "displayName": "AI"
  },
  "model": "dummybridge/ag-ui",
  "usage": {
    "promptTokens": 0,
    "completionTokens": 0,
    "totalTokens": 0
  },
  "usageDetails": {
    "reasoningTokens": 0,
    "cachedInputTokens": 0
  },
  "status": {
    "state": "streaming",
    "finishReason": "stop",
    "terminal": null,
    "error": null
  },
  "approvals": [
    {
      "id": "approval-id",
      "toolCallId": "tool-call-id",
      "state": "requested",
      "always": false,
      "reason": ""
    }
  ],
  "artifacts": {
    "sources": [],
    "documents": [],
    "files": []
  },
  "data": {},
  "preview": {
    "text": "bounded visible preview",
    "truncated": true
  }
}
```

- `model` is the AG-UI model identifier string. Do not add `modelInfo`; display/provider details should be derived from the model registry, agent profile, or bridge/network metadata instead of duplicated on every message.
- `usage` mirrors AG-UI `RUN_FINISHED.usage`. Extra usage fields belong in `usageDetails`.
- `finishReason` should use TanStack/AG-UI values: `stop`, `length`, `content_filter`, `tool_calls`, or `null`. Command aliases may accept hyphenated input, but emitted metadata should use AG-UI values.
- `usage` is token/usage metadata only. Do not add dollar cost fields unless the product explicitly decides to expose pricing.
- `artifacts` and `data` are for descriptors needed to render run-level UI outside the streamed parts. If an item is naturally a UI part, it should stay in the stream instead of being duplicated here.
- Final compact metadata may update `status`, `usage`, `approvals`, `artifacts`, `data`, and `preview`, but still must not embed full chunks/parts.

## Desktop Work

Update Desktop as part of parity because the new transport deliberately splits one stream across multiple Matrix events.

Dependency:

- Add `@tanstack/ai-react-ui` to the Desktop app and use it for AI message rendering.
- Do not hand-roll a parallel markdown renderer when TanStack's UI package already provides one.
- `@tanstack/ai-react-ui` `TextPart` renders Markdown with `react-markdown`, GFM tables/strikethrough via `remark-gfm`, sanitized HTML via `rehype-sanitize`, and code highlighting via `rehype-highlight`.
- Keep Beeper-specific shell/layout/actions in Desktop, but delegate TanStack text/thinking/tool/result part rendering to TanStack UI components or thin render props around them.

TanStack ownership in Desktop:

- Desktop already depends on `@tanstack/ai` and `@tanstack/ai-client`; use those packages instead of duplicating their concepts.
- Import `UIMessage`, `MessagePart`, `TextPart`, `ThinkingPart`, `ToolCallPart`, `ToolResultPart`, `ToolCallState`, and `ToolResultState` from TanStack packages.
- Use TanStack `StreamProcessor`/stream utilities where practical for applying AG-UI chunks into UI messages instead of maintaining a parallel Desktop-only stream reducer.
- Use TanStack `parsePartialJSON`/partial JSON utilities for streaming tool args instead of maintaining a separate parser.
- Use `@tanstack/ai-react-ui` for `ChatMessage`/part rendering, with Beeper render props only for product-specific chrome, approvals, and bridge actions.
- Delete or collapse Desktop-only normalized AI types that duplicate TanStack structures, such as separate text/reasoning/tool-call models, once the TanStack path can feed the UI directly.
- Keep Desktop-local types only for Beeper transport/persistence: Matrix event IDs, `target_event`, carrier visibility, `com.beeper.stream`, `com.beeper.ai.metadata`, and approval prompt Matrix metadata.

Intentional AG-UI boundaries:

- AG-UI owns semantic events, UI message parts, tool states, run input, and stream processing.
- Beeper owns transport: encrypted Matrix events, carrier hiding, target event mapping, replay from persisted Matrix history, and approval reaction cleanup.
- `com.beeper.ai.metadata` is Beeper message metadata and must not become a second UI message schema. It may store non-part run metadata, but streamed parts/chunks remain AG-UI.
- `target_event` is a Beeper transport pointer, not an AG-UI field. Keep it in the carrier envelope and Desktop stream routing, not in TanStack `UIMessage`.

PAS sync:

- In `src/pas-server/beeper/EventSyncContext.ts`, detect decrypted `m.room.message` events that contain stream delta content keys ending in `.deltas` or batched `updates`.
- Extract stream deltas from encrypted carrier events after decryption.
- Emit `STATE_SYNC message stream` updates instead of normal message upserts for carrier-only events.
- Mark carrier timeline events hidden after extraction so they do not show as chat bubbles.
- Keep the visible anchor event and approval prompt event as normal messages.
- Preserve `com.beeper.ai`, `com.beeper.stream`, and per-message profile behavior for anchor/final messages.

Beeper client stream routing:

- In `src/pas-server/beeper/BeeperClient.ts`, keep using the existing stream event path, but ensure multi-carrier events preserve `room_id`, carrier `event_id`, `target_event`, `threadId`, `runId`, `messageId`, and `seq`.

Common types:

- In `src/common/types/beeper.ts`, extend stream types to include AG-UI `threadId`, `runId`, and `messageId`, plus Beeper transport `target_event`.
- Keep support for both single-update `.deltas` and batched replay `updates`.

Renderer store:

- In `src/renderer/stores/AIChatsStore.ts`, merge by `{target_event, runId}` rather than only target message/run.
- Map carrier target events back to the visible anchor message.
- Continue buffering out-of-order `seq`.
- Treat stream carriers as dirtying and extending the first visible AI message only, never as separate visible messages.
- Merge streamed content and ordered UI parts into the first visible message's renderer state.
- Hide carrier messages after extracting deltas.
- Track approval prompts by approval ID and target tool call.
- Support multiple assistant messages/parts per run by indexing on AG-UI `messageId`, not assuming one text part per run.
- Support parallel tool calls by distinct tool call IDs and optional `index`.
- Preserve streamed partial tool arguments while parsing partial JSON best-effort; replace with finalized arguments when `TOOL_CALL_END` arrives.
- Accept tool output either on `TOOL_CALL_END.result` or as AG-UI `TOOL_CALL_RESULT`.
- Apply run-scoped versus thread-scoped `RUN_ERROR` behavior as described above.
- Reconstruct runs from persisted anchor plus carrier history during replay/backfill using the same code path as live streaming.

UI message application:

- In `src/renderer/ai/ui-message.ts`, apply AG-UI events into TanStack-shaped parts.
- Preserve ordered parts instead of collapsing everything by type.
- Render the resulting TanStack `UIMessage` with `@tanstack/ai-react-ui` instead of converting it into a separate Beeper-only part model.
- Support compatibility input for current events while preferring new output shapes:
  - text
  - thinking/step
  - tool-call
  - tool-result
  - state snapshot/state delta/messages snapshot
  - source-url/source-document/file/custom data
- Map approval states to TanStack states:
  - `approval-requested`
  - `approval-responded`
  - result `complete`
  - result `error`

Message types:

- AI visible messages and approval prompts should use message types that render as bubbles where intended.
- Stream carrier events are `m.room.message`/`m.text` for compatibility, but should not render as bubbles after Desktop extraction.
- Avoid `m.notice` for visible AI chat content in AI-network rooms because Desktop hides AI `m.notice` events.

## Approvals

Approval requests remain separate visible Matrix events with reaction options.

Generic reaction option shape:

```go
type ReactionOption[T any] struct {
	ID       string
	Label    string
	Values   []string
	Value    T
}
```

`Values` is the complete set of strings that should match this option. Entries may be literal emoji (`👍`), symbolic reaction keys (`approval.allow_once`), short names (`allow`), or bridge-specific aliases. The helper owns normalization and matching; callers should pass strings and not branch on whether a value is an emoji or a key.

Tool approval response shape:

```go
type ToolApprovalResponse struct {
	ID       string
	Approved bool
	Always   bool
	Reason   string
	Fields   map[string]any
	Metadata map[string]any
}
```

`Always` supports allow-always style options without making that concept Matrix-specific. `Fields` is for flexible provider/bridge-specific approval data that should survive resolution but not force new top-level schema every time.

Rules:

- AG-UI stream emits a tool-call state transition to `approval-requested`.
- The tool-call part includes `approval: { id, needsApproval: true }`.
- Matrix reaction choices are transport metadata and must not be embedded into AG-UI events.
- Approval prompt events should relate to the first visible anchor message and include `threadId`, `runId`, `messageId`, `toolCallId`, and approval ID.
- Matrix approval event stores `com.beeper.ai.approval` with tool call ID, tool name, `threadId`, `runId`, `messageId`, expiration if any, and reaction options.
- On user reaction, the bridge resolves the option to a `ToolApprovalResponse`.
- After resolution, emit AG-UI state `approval-responded`.
- If approved, continue execution and emit tool result `complete` or `error`.
- If denied, emit a `tool-result` with `state: "error"` and structured reason `denied`; do not pretend the tool executed.
- Approval options should support flexible fields, including allow-once, allow-always, deny, reason, and provider/bridge-specific metadata.
- Keep the user's selected Matrix reaction event exactly as the visible user choice, regardless of whether it matched by emoji or symbolic key.
- Remove bridge-posted placeholder option reactions and non-selected option reactions.
- The cleanup helper should return the selected option, selected reaction event ID if known, and a list of bridge-posted reaction event IDs to remove. Actual Matrix redaction/deletion remains the bridge adapter's job.
- Programmatic approval and Matrix reaction approval must share the same resolver and produce the same stream events.

Custom events:

- Support AG-UI `CUSTOM` events.
- Use built-in/custom names from TanStack when they exist, such as `approval-requested`.
- Beeper-specific custom events must use a clear namespace such as `com.beeper.*`.
- Do not add random one-off custom names when an AG-UI lifecycle, tool, state, or message event already models the behavior.

## Open Decisions With Recommended Defaults

These are the remaining decisions that affect product behavior or implementation shape. Use the recommended default unless the answer to the question changes the product intent.

1. Source of truth for AG-UI schemas
   - Recommended: `pkg/ag-ui` is the only Go source of truth for AG-UI concepts. Other packages import it instead of redefining parallel event, message, tool, or approval types.
   - Decision: Desktop must use TanStack types directly for AG-UI/UI message concepts. Local Desktop types should only describe Beeper transport envelopes and app-specific metadata.

2. Final persisted state for long runs
   - Recommended: never require a final full-text edit. The first visible message stores compact identity/terminal metadata and Desktop reconstructs long content from carriers.
   - Decision: compact final metadata should include everything needed to render the run except streamed parts/chunks. Do not store full parts/chunks in final metadata for large runs.

3. Metadata contract on the first visible message
   - Decision: first message owns all non-part run metadata: IDs, model, usage, finish/terminal state, approval summary, source/file/data descriptors that are metadata, and any archived `aichats` metadata that is not the streamed UI parts/chunks themselves. Do not include dollar cost fields unless there is a separate product decision.

4. Dropped or invalid carriers
   - Recommended: Desktop marks the first visible AI message failed and keeps carriers hidden. Do not expose carrier messages as fallback bubbles.
   - Direct question: Should Desktop show a recoverable "stream incomplete" state, or a hard failed generation state?

5. Approval idempotency
   - Recommended: first valid approval resolution wins. Later Matrix reactions or programmatic responses are ignored, do not re-run the tool, and may be cleaned up as stale choices.
   - Direct question: Should a user be allowed to change approval before the tool starts executing, or is first valid reaction always final?

6. Allow-always behavior
   - Recommended: support it generically in approval fields and reaction options, but dummybridge should only persist/use it if there is a clear storage target.
   - Direct question: Should dummybridge actually remember allow-always across runs, or only emit the field to prove the UI/transport supports it?

7. Package boundaries
   - Recommended: embrace mautrix in `pkg/ai-stream/matrix`; keep bridgev2-specific database/queue/redaction in `pkg/ai-stream/bridgev2`; keep `pkg/ag-ui` pure.
   - Direct question: Should `pkg/ai-stream/matrix` return mautrix `event.MessageEventContent` directly everywhere, or expose a small content struct plus conversion helpers?

8. TanStack docs freshness
   - Recommended: before implementation starts, re-open current TanStack AI docs and update the contract section if state names or part shapes changed.
   - Direct question: Should implementation pin to the docs current at implementation start, or should tests tolerate small TanStack naming changes?

## Tests

Dummybridge Go tests:

- Port archived parser tests.
- Verify help aliases.
- Verify command guide includes all commands.
- Verify conflicting terminal options are rejected.
- Verify invalid random profile is rejected.
- Verify oversized option inputs are rejected.
- Verify markdown-rich text generation is deterministic by seed and varied across calls.
- Verify table/link/list/code/quote markdown signals.
- Verify `stream-lorem` emits thinking, steps, text, sources, documents, files, persistent data, and excludes transient data from final snapshot.
- Verify `stream-tools` covers success, failure, approval, denial, delta input, input error, preliminary output, and provider-executed tools.
- Verify random streams finish and respect duration.
- Verify chaos streams start multiple runs with stagger and max-actions.
- Verify error and abort terminal states.

`pkg/ag-ui` tests:

- Validate all current AG-UI lifecycle event builders: `RUN_STARTED`, `RUN_FINISHED`, `RUN_ERROR`, `TEXT_MESSAGE_START`, `TEXT_MESSAGE_CONTENT`, `TEXT_MESSAGE_END`, `TOOL_CALL_START`, `TOOL_CALL_ARGS`, `TOOL_CALL_END`, `TOOL_CALL_RESULT`, `STEP_STARTED`, `STEP_FINISHED`, `STATE_SNAPSHOT`, `STATE_DELTA`, `MESSAGES_SNAPSHOT`, and `CUSTOM`.
- Validate event builders and required IDs.
- Validate `RunAgentInput`.
- Validate `UIMessage` ordered part shape.
- Validate tool-call and tool-result states against TanStack values.
- Validate approval request and response shapes.
- Validate step/thinking events.
- Validate `STATE_SNAPSHOT`, `STATE_DELTA`, and `MESSAGES_SNAPSHOT` event shapes.
- Validate every emitted event has `timestamp`.
- Validate `rawEvent` is optional and bounded/truncated/omitted before exceeding carrier limits.
- Validate `TOOL_CALL_START.index`.
- Validate partial JSON `TOOL_CALL_ARGS`.
- Validate `TOOL_CALL_END` with and without result.
- Validate `TOOL_CALL_RESULT` creates/updates TanStack `tool-result` parts.
- Validate run-scoped and thread/session-scoped `RUN_ERROR`.
- Validate multiple assistant `messageId`s per run.
- Reject legacy/non-AG-UI chunk shapes.

`pkg/ai-stream` tests:

- Verify ordered run writer output.
- Verify no per-delta accumulated full text.
- Verify final accumulator is only used at finalization.
- Verify UTF-8 splitting.
- Verify carrier packer respects the 58KB serialized JSON budget.
- Verify stream reconstruction from carriers.
- Verify duplicate/stale/out-of-order `seq` behavior.
- Verify missing `seq` gap timeout marks the anchor incomplete/failed.
- Verify carrier delete/redaction recomputes or marks the anchor incomplete/failed.
- Verify approval reaction resolver keeps the selected value and identifies removals.

Over-64KB tests:

- Generate at least 70KiB of output.
- Assert every carrier's serialized content is at or below 58KB.
- Assert at least two carrier events are emitted.
- Assert later carriers have no preview body or only minimal body.
- Assert reconstruction from deltas exactly equals generated output.
- Assert no final full-body edit is required to display the complete stream.

Desktop tests:

- PAS extracts `.deltas` from decrypted carrier events.
- Carrier-only events are hidden and do not render as chat bubbles.
- Single-update and batched `updates` formats still work.
- Multi-carrier stream merges into the visible anchor message.
- Out-of-order `seq` buffering works.
- Duplicate/stale `seq` handling works.
- TanStack-shaped text/thinking/tool/result parts render through the AI message view.
- State snapshot, state delta, and messages snapshot events are preserved and exposed to rendering/devtools.
- Approval prompt indexing works from both visible prompt metadata and stream state.
- Approval response transitions resolve approval state.
- Parallel tool calls render/merge by distinct tool call IDs and optional indexes.
- Partial JSON tool args remain visible while streaming and finalize cleanly.
- `TOOL_CALL_END.result` renders as a completed tool result.
- `RUN_ERROR` with `runId` fails only that run; `RUN_ERROR` without `runId` fails active runs in the thread.
- Multiple assistant `messageId`s in one run render in order.
- Replay/backfill reconstructs the same visible run from persisted anchor plus carrier history as live streaming.
- Deleted/redacted carriers keep carrier bubbles hidden and mark/recompute the anchor correctly.
- Ordering gaps time out instead of buffering forever.
- Over-64KB carrier sequence reconstructs into one AI message.

Commands to run:

- In dummybridge: `go test -mod=readonly ./...`
- In Desktop after adding `@tanstack/ai-react-ui`: run the package manager install/update command explicitly approved for that dependency and commit the resulting manifest/lockfile changes with the Desktop implementation.
- In Desktop: run the existing focused test commands for touched files. At minimum cover `ai-common`, `ui-message`, `AIChatsStore`, `EventSyncContext`, and stream mapper tests.

## Live Smoke Testing

Use bridgev2 and Desktop API, not raw Matrix sends, for end-to-end checks.

Recommended smoke cases:

- Create/login a QA account using the established `qatest+<digits>@beeper.com` pattern and fixed OTP only if a fresh account is needed.
- Create or reuse an AI DM through bridge-manager/Desktop API.
- Send `help` and confirm the command guide appears as a normal AI bubble.
- Send `stream-lorem 70000 --chunk-chars=512 --seed=7` and confirm Desktop shows one streaming AI message, not many carrier bubbles.
- Send `stream-tools 200 shell#approval --seed=3` and confirm the approval prompt appears separately with reaction options.
- React approve and confirm the selected emoji remains while other bridge options disappear and the tool completes.
- React deny and confirm the tool is cancelled/denied and does not execute.
- Send `stream-random 5 --actions=8 --allow-approval --seed=9`.
- Send `stream-chaos 3 5 --max-actions=5 --seed=11`.

Acceptance criteria:

- All carrier events are encrypted in E2EE rooms.
- No plaintext raw Matrix sends are used.
- Visible AI output uses bubble-rendering message types.
- Carrier events do not show as separate bubbles.
- Streaming remains incremental.
- Over-64KB output reconstructs correctly.
- Approvals work from Matrix reactions and programmatic/TanStack-shaped responses.
- The selected approval emoji is kept and non-selected placeholder options are removed.
