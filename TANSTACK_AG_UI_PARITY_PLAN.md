# TanStack/AG-UI Parity Implementation Brief

## Summary

Build dummybridge AI around current TanStack AG-UI primitives in both directions:

- dummybridge emits AG-UI stream events and accepts TanStack-shaped approval responses.
- Desktop consumes multi-event encrypted AG-UI streams and hides carrier events from the normal timeline.
- Shared Go packages define the primitive contract instead of preserving old AI SDK or agentremote decisions.

Do not install dependencies, update dependencies, or modify lockfiles unless the user explicitly approves that exact dependency change. Before adding `@tanstack/ai-react-ui` or changing the Desktop lockfile, verify whether it is already present in the current Desktop checkout; if it is not, ask first.

## Plan Of Record

This is the intended behavior to preserve while finishing the implementation:

1. The normal AG-UI stream is an ordered delta log. Envelopes have `seq`, never `seqTotal`, carrier totals, or final event counts.
2. Every run has one visible Matrix anchor message. Normal stream carriers and finalization carriers are hidden transport events that merge into that anchor.
3. The final supported-client state is complete AG-UI state: text, thinking, tool calls/results, approval state, sources/files/data/state, terminal status, usage, model, and run metadata.
4. Finalization sends hidden carriers first, then the compact final Matrix edit last. The final edit stops streaming and carries Matrix-native preview HTML; it does not need to carry the full generated text or full parts array.
5. Over-budget final state uses a base `MESSAGES_SNAPSHOT` plus `CUSTOM name="com.beeper.ai.final-parts"` continuations. Continuations contain only relation data and omitted parts, not repeated full metadata.
6. PAS/Desktop must process hidden stream/finalization carriers before treating the final edit as the point where streaming stops. From the renderer's perspective, there is always one final AI message.
7. Approval prompts remain separate visible Matrix messages for actionability, while semantic approval state is also represented in AG-UI so supported clients can show it inline as a tool-call state.
8. Unsupported clients are not the primary target, but the final Matrix edit must still be a coherent bounded Matrix HTML preview for timeline, search, and notifications.

Do not reintroduce these rejected approaches:

- No `seqTotal`, carrier totals, or final event totals on normal streaming envelopes.
- No visible carrier bubbles as a fallback for unsupported or failed merge behavior.
- No final full-text Matrix edit for long runs. The final edit is a bounded preview plus compact metadata.
- No broad `as any` or whole-object assertions at the Desktop TanStack render boundary.
- No duplicate Beeper-only UI message model where TanStack types already describe the part contract.
- No package-manager install/update/lockfile change without explicit approval.

## Implementation Status

As of this checkout, the plan should be read as a completion/audit checklist rather than a blank design doc.

Done in dummybridge:

- New `pkg/ag-ui`, `pkg/ai-stream`, `pkg/ai-stream/matrix`, and `pkg/ai-stream/bridgev2` packages exist.
- The old provisional `pkg/aichats` package has been removed.
- Normal stream envelopes use ordered `seq` and do not carry `seqTotal`.
- `MESSAGES_SNAPSHOT` finalization can split into a metadata-preserving base snapshot plus `com.beeper.ai.final-parts` continuations.
- Large text/thinking final parts split at UTF-8 boundaries and are reassembled by the supported client model.
- Carrier replay for built runs is contiguous; synthetic timestamps no longer add random delays between already-built carrier sends.
- Final anchor edits use mautrix Markdown rendering for Matrix HTML preview content.
- Approval response carriers are queued before the final metadata edit for the anchor.

Done in the related Desktop checkout:

- Carrier-only encrypted events with non-empty `*.deltas` are routed as hidden stream updates instead of normal timeline upserts.
- `com.beeper.ai.final-parts` continuations merge into the existing TanStack-shaped UI message by `messageId`, `runId`, and `partOffset`.
- The AI renderer path uses a typed high-level adapter at the TanStack render boundary instead of asserting the whole message as `any`.
- `src/renderer/ai/ui-message.ts` uses typed builders/guards for TanStack and Beeper custom parts instead of broad `MutableUIPart`/record-level assertions.

Completion status:

- The non-visual plan gates are implemented and verified by the evidence below.
- Full Desktop typecheck is still red, but the failures are outside the touched AI/PAS files and are listed below.
- Visual testing remains explicitly excluded from the current completion target.

Completion gates:

- Unit/focused tests pass for dummybridge and touched Desktop AI/PAS paths.
- Full Desktop typecheck either passes or every failure is documented as unrelated to touched AI/PAS files.
- Live staging smoke proves over-64KB output produces one visible AI anchor and hidden carriers only.
- Live staging smoke proves approve and deny both finalize through hidden response carriers before the final anchor edit.
- Replay/backfill, redaction/delete, and missing-gap behavior have either automated tests or an explicit live/manual verification note.
- `rg` source scan proves runtime source does not emit `seqTotal`.

Current verification snapshot, 2026-05-19:

- `go test -mod=readonly ./...` in dummybridge passes.
- Desktop focused AI/PAS tests pass:
  - `bun run test --run src/common/ai-common.test.ts src/renderer/ai/ui-message.test.ts src/renderer/ai/stream-ordering.test.ts src/renderer/stores/AIChatsStore.test.ts src/pas-server/beeper/EventSyncContext.test.ts src/pas-server/beeper/connect/ws-event-mapper.test.ts src/pas-server/beeper/connect/ws-events-server.test.ts`
- Full Desktop typecheck still fails in unrelated files outside the touched AI/PAS paths:
  - `BrandLink.stories.tsx`
  - `ComposeMessage/TextArea/TextArea.tsx`
  - `DetachedAccountsOnboarding.tsx`
  - `electron-ipc.ts`
  - `measureInteractionNextPaint.ts`
  - `QuickRepliesPrefsSubView.tsx`
- Runtime source scans find no total-count fields:
  - `rg -n 'seqTotal|carrierTotal|finalEventTotal' pkg cmd --glob '!**/*_test.go'`
  - `rg -n 'seqTotal|carrierTotal|finalEventTotal' src/common src/renderer/ai src/renderer/stores src/pas-server/beeper --glob '!**/*.test.ts' --glob '!**/*.test.tsx'`
- Desktop AI render/store source scan finds no broad assertions in the touched AI paths:
  - `rg -n '\] as any|as MutableUIPart|MutableUIPart|ToolUIPartRecord|console\.log\(' src/renderer/ai src/renderer/stores/AIChatsStore.ts`
- Live staging over-64KB smoke passed after lowering the raw carrier budget to 40KB: one visible AI anchor, hidden carriers, no visible `com.beeper.ai.final-parts` leakage, and Matrix HTML preview on the final edit.
- Live staging approval approve and deny pass: prompt remains separate, selected user reaction remains, bridge option reactions are redacted, response carriers are queued, and final anchor edit preserves the existing preview instead of reverting to `...`.
- Live staging random/chaos smoke passes for hidden-carrier behavior: no carrier bubbles appeared in Desktop API output, approval prompts stayed separate, and final edits did not regress completed approval anchors.
- Replay/backfill has unit coverage through batched `updates` extraction and `AIChatsStore` replay into an existing anchor.
- Deleted/redacted carrier and missing-gap behavior have unit coverage: carrier deletion marks the anchor failed while keeping carrier events hidden, and unresolved sequence gaps now fail via timer without requiring another stream event.

## Baseline And Existing Entry Points

The original plan replaced a provisional `pkg/aichats` package plus AI handling in `pkg/connector/client.go`. In this checkout, `pkg/aichats` should stay deleted; the active implementation is the new `pkg/ag-ui` and `pkg/ai-stream` stack plus the connector integration points.

Behavior baseline to preserve or improve:

- AI DM resolution uses the `ai`/`AI` ghost and AI portals with the `ai-` prefix.
- The bridge sends one visible anchor/placeholder event, streams `com.beeper.llm.deltas`, then edits the anchor with final compact metadata and Matrix preview content.
- Stream deltas must be real AG-UI events, not AG-UI-like compatibility shapes.
- Approval requests are separate Matrix events with `com.beeper.ai.approval` metadata and reaction options.
- Approval reaction handling keeps the user's selected emoji and removes bridge-posted placeholder/non-selected options.
- Text streaming must send incremental deltas and must not resend full accumulated text on every delta.

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

Legacy dummybridge AI implementation that should not be restored:

- `/Users/batuhan/Projects/labs/dummybridge/pkg/aichats/agui.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/aichats/matrix.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/aichats/agui_test.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/connector/client.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/connector/connector.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/connector/login.go`
- `/Users/batuhan/Projects/labs/dummybridge/pkg/connector/example-config.yaml`

Active dummybridge package targets:

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
- Do not put total counts such as `seqTotal`, carrier count, or final event count on normal stream envelopes. The streaming layer is an ordered event stream, not a pre-counted file transfer.
- If `target_event` is unavailable during early processing, temporarily key by `{threadId, runId}` and promote to `{target_event, runId}` when the anchor message is known.
- Desktop buffers out-of-order deltas within existing ordering limits.
- Duplicate or stale `seq` values are ignored or rejected consistently.

Size budget:

- Treat 64KB as the external ceiling.
- Use a hard carrier budget of 40KB for serialized Matrix content. Live staging E2EE sends showed that 58KB raw carrier content can become 66-79KB encrypted Matrix event content, so the budget must leave room for megolm/base64/wrapper overhead.
- The packer must measure serialized JSON byte size before adding an envelope to a carrier.
- If a single text delta would exceed the carrier budget, split it at UTF-8 rune boundaries.
- If a non-text event cannot fit inside the carrier budget, return a validation error rather than sending it.
- `rawEvent` must be optional, bounded, and safe to omit. If including `rawEvent` would push a carrier over budget, truncate it or drop it before packing rather than bloating the event.
- Truncated raw provider data must be marked, e.g. `rawEventTruncated: true`, so debugging does not confuse partial raw data with complete provider payloads.

Preview/body algorithm:

- The first visible message is the canonical message for the run.
- Put as much useful early visible preview as practical into the first message while preserving required metadata and staying under the carrier budget.
- All run-level metadata that should survive as the message identity, such as model, usage, thread/run/message IDs, terminal state, and approval summary, belongs on the first visible message or its compact final metadata.
- Later carrier messages should be hidden and merged by compatible clients into the first visible message.
- Later carrier bodies should be empty or minimal and put payload in `.deltas`.
- Compatible clients must reconstruct from ordered deltas and merge content/parts into the first message, not display carriers as separate runs.
- Do not rewrite full accumulated content on every delta.

Finalization:

- The run accumulator is only for finalization, preview generation, and tests.
- Normal stream chunks remain unaware of final chunk totals. Completion is determined by ordered AG-UI terminal/finalization events plus the final edit ordering, not by `seqTotal`.
- Finalization must emit the complete final AG-UI UI state for supported clients, including text, thinking, tool calls, tool results, approval state, sources/files/data/state, terminal status, usage, and model/run metadata.
- Finalization state may be split across hidden carrier events to stay under the serialized Matrix carrier budget.
- The final Matrix edit is sent only after all normal stream carriers and finalization carriers have been queued. It marks the anchor finalized and carries compact metadata plus Matrix-native preview HTML, not the full parts array.
- Do not require a final Matrix edit containing the full generated body or full AG-UI parts for over-64KB runs.
- The client is responsible for merging the hidden stream/finalization carriers into the anchor message.

Final snapshot splitting algorithm:

- Build one final AG-UI `UIMessage` in render order.
- Compact adjacent same-kind text fragments before packing final state when doing so does not lose detail. For example, five adjacent text-only chunks should become one final text part.
- Preserve semantic boundaries. Do not merge text across thinking, tool-call, tool-result, approval, source/file/data, or state parts.
- Start with a base `MESSAGES_SNAPSHOT` event containing the message identity and metadata:
  - `id`
  - `role`
  - `metadata`
  - `parts`
- The base event should include as many user-visible parts as fit under budget, in display order. Prioritize visible content over bulky diagnostics.
- If the next part would exceed budget, omit it from the base event and move it to a continuation event instead of duplicating metadata.
- Continuation events use a Beeper-owned AG-UI custom event: `CUSTOM` with `name: "com.beeper.ai.final-parts"`.
- Continuation event payload contains only relation/merge data and parts:

```json
{
  "messageId": "message-id",
  "runId": "run-id",
  "threadId": "thread-id",
  "partOffset": 3,
  "parts": []
}
```

- `partOffset` is the zero-based part index in the final message and is used for deterministic append/validation. Continuations must not repeat full message metadata.
- Desktop merges by applying the base snapshot, then inserting/appending continuation `parts` at `partOffset`. If the continuation part has the same semantic part identity as the part at that offset and only extends a split `content` field, concatenate the content instead of creating a second visible part.
- Split at the highest semantic level possible: carrier -> AG-UI event -> UIMessage parts -> large string fields.
- If a single text or thinking part is too large, split only its `content` at UTF-8 rune boundaries and use the same `partOffset` for the continuation slices so they concatenate back into one part.
- Do not split tool call, tool result, source/file/data, approval, or structured state objects unless there is an explicit field-level reassembly schema. Drop or truncate raw/debug/provider metadata before considering structured splitting.
- If one non-splittable structured part cannot fit under budget after raw/debug/provider metadata is removed, fail packing with a validation error instead of emitting an unmergeable partial object.
- Finalization carriers are sent before the final Matrix edit. The final edit must not race ahead of the final-parts carriers.

Final Matrix preview:

- Finalized messages must have Matrix-native preview content on the anchor edit:
  - `body`: bounded plain text preview
  - `format`: `org.matrix.custom.html`
  - `formatted_body`: Matrix HTML generated by mautrix's Markdown renderer
- Unsupported clients are not a primary target, but the final edit should still be a coherent Matrix message preview for timeline/search/notifications.
- The full supported-client AI state comes from hidden carriers, not from the final edit body.

Transport ordering:

- For built dummybridge runs, send carrier events contiguously once the anchor Matrix event ID is known. Do not sleep between carriers based on synthetic generation timestamps.
- Demo/random delays may affect when runs are started or what timestamps are embedded in AG-UI events, but they must not delay replaying an already-built carrier sequence before finalization.
- Queue order for one run must be: anchor -> hidden normal carriers -> visible approval prompts/reaction options when applicable -> hidden approval response carriers when resolved -> hidden finalization carriers -> final anchor edit.
- If finalization carriers and final edit arrive in the same sync batch, PAS/Desktop must process carrier stream entries before using the edit to stop streaming.

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
- State events must still obey the carrier budget and multi-carrier splitting rules.
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

- Use `@tanstack/ai-react-ui` from the current Desktop checkout when it is already present.
- If it is absent, ask before adding it, running a package-manager install/update, or changing any manifest/lockfile.
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
- Type the state at the highest correct level. The renderer should accept a TanStack-shaped `UIMessage`/renderable message type and should not require whole-message `as any` assertions.
- Use narrow builders/guards for Beeper custom part variants instead of broad `MutableUIPart` assertions. If a part is not expressible as a TanStack part, keep the extension isolated behind a typed Beeper custom-part union and convert at the render boundary.
- Do not use assertions to bypass missing required fields. If TanStack requires a field, either populate it from AG-UI state or keep the part out of the TanStack render path until it has a real representation.
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
- Matrix reaction choices are transport metadata and must not be embedded into AG-UI events as the source of truth for reactions.
- Approval prompt events should relate to the first visible anchor message and include `threadId`, `runId`, `messageId`, `toolCallId`, and approval ID.
- Matrix approval event stores `com.beeper.ai.approval` with tool call ID, tool name, `threadId`, `runId`, `messageId`, expiration if any, and reaction options.
- Approval prompts are separate visible Matrix messages for actionability and reaction handling.
- Supported clients may render the same approval inline as a tool-call variant on the anchor message. To support that, duplicate semantic approval state into the AG-UI stream while keeping Matrix prompt/reaction metadata on the prompt event.
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

## Decisions And Remaining Behavior

Settled decisions:

- `pkg/ag-ui` is the Go source of truth for AG-UI concepts. Other Go packages import it instead of redefining parallel event, message, tool, or approval types.
- Desktop uses TanStack types directly for AG-UI/UI message concepts wherever possible. Desktop-local types describe Beeper transport and persistence, not a second AI message model.
- Long runs never require a final full-text Matrix edit. The final edit stores compact identity/terminal metadata and Matrix HTML preview; supported clients reconstruct complete UI state from hidden carriers.
- Final AG-UI state is complete and may be split into hidden finalization carriers. The final anchor edit remains compact and must not embed the full parts/chunks array.
- The final split format is base `MESSAGES_SNAPSHOT` plus `com.beeper.ai.final-parts` continuations with relation data and omitted parts only.
- Normal stream chunks do not include `seqTotal` or any total-count field.
- First visible message metadata owns non-part run metadata: IDs, model, usage, finish/terminal state, approval summary, and source/file/data descriptors that are metadata. It does not store streamed text chunks, thinking chunks, tool args, tool results, or full parts.
- Use mautrix in `pkg/ai-stream/matrix`. Keep bridgev2-specific queue/database/redaction behavior outside the pure AG-UI package.

Behavior still requiring implementation or verification:

- Dropped or invalid carriers: Desktop should mark the anchor incomplete/failed and keep carriers hidden. Do not show carrier messages as fallback bubbles.
- Missing `seq` gaps: timeout must stop infinite buffering, fail or mark incomplete on the anchor, and keep later stray carrier events hidden.
- Carrier delete/redaction: recompute from remaining carriers when possible; otherwise mark incomplete/failed.
- Replay/backfill: reconstruct the same visible AI run from persisted anchor plus carriers as live streaming.
- Approval idempotency: first valid approval resolution should win. Later reactions/programmatic responses should not re-run the tool and may be cleaned up as stale.
- Allow-always: support the field generically in approval options/responses, but dummybridge should not persist cross-run allow-always state until there is a real product storage target.
- TanStack drift: before future dependency upgrades, re-open current TanStack docs/source and update this contract deliberately. Do not silently adapt by assertions.

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
- Verify normal stream envelopes do not contain finalization totals such as `seqTotal`.
- Verify no per-delta accumulated full text.
- Verify final accumulator is only used at finalization.
- Verify UTF-8 splitting.
- Verify carrier packer respects the serialized JSON carrier budget.
- Verify stream reconstruction from carriers.
- Verify finalization carriers split a complete final UI message into a base snapshot plus continuation parts without repeating metadata.
- Verify finalization continuations merge deterministically by `messageId`, `runId`, and `partOffset`.
- Verify oversized text/thinking final parts split at UTF-8 boundaries and reassemble exactly.
- Verify oversized raw/debug/provider metadata is truncated or omitted before splitting structured tool/data parts.
- Verify duplicate/stale/out-of-order `seq` behavior.
- Verify missing `seq` gap timeout marks the anchor incomplete/failed.
- Verify carrier delete/redaction recomputes or marks the anchor incomplete/failed.
- Verify approval reaction resolver keeps the selected value and identifies removals.

Over-64KB tests:

- Generate at least 70KiB of output.
- Assert every carrier's serialized content is at or below the carrier budget.
- Assert at least two carrier events are emitted.
- Assert later carriers have no preview body or only minimal body.
- Assert reconstruction from deltas exactly equals generated output.
- Assert no final full-body edit is required to display the complete stream.
- Assert final snapshot state is complete even when split across finalization carriers.
- Assert final edit contains Matrix `formatted_body` generated by mautrix Markdown rendering and does not contain the full parts array.

Desktop tests:

- PAS extracts `.deltas` from decrypted carrier events.
- Carrier-only events are hidden and do not render as chat bubbles.
- Single-update and batched `updates` formats still work.
- Multi-carrier stream merges into the visible anchor message.
- Finalization base snapshot plus `com.beeper.ai.final-parts` continuations merge into one final `UIMessage`.
- Final edit arriving after carriers finalizes the existing anchor without creating a second message or flickering back to preview-only content.
- If final edit and stream/finalization carriers arrive in one sync batch, carriers are applied before streaming is stopped.
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
- In Desktop, if `@tanstack/ai-react-ui` is not already present: ask before running any package-manager install/update command or changing any lockfile.
- In Desktop: run the existing focused test commands for touched files. At minimum cover `ai-common`, `ui-message`, `AIChatsStore`, `EventSyncContext`, and stream mapper tests.
- In Desktop: run typecheck after the focused tests. If the full repo typecheck is already failing for unrelated reasons, record the unrelated failures and separately prove touched AI files are type-clean.

Verification status to track in the PR or completion note:

- Dummybridge unit tests: command, date, result.
- Desktop focused tests: command, date, result.
- Desktop typecheck: command, date, result, and whether failures touch AI files.
- Source scan: prove no runtime source emits `seqTotal`.
- Over-64KB live smoke: one visible AI anchor, hidden carriers, final Matrix HTML preview, complete reconstructed supported-client state.
- Approval live smoke: visible approval prompt, selected reaction preserved, stale bridge option reactions removed, final anchor edit after response carriers.
- Random/chaos live smoke: no carrier bubbles, no stuck streaming state, no flicker to preview-only content after final edit.
- Replay/backfill smoke: persisted history reconstructs the same visible run after restart/reload.
- Redaction/gap smoke or unit coverage: carriers stay hidden and anchor becomes recomputed or incomplete/failed.

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
- Finalized over-64KB runs still have one visible anchor message, complete supported-client AG-UI state, and bounded Matrix HTML preview on the final edit.
- Approvals work from Matrix reactions and programmatic/TanStack-shaped responses.
- The selected approval emoji is kept and non-selected placeholder options are removed.
