package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	"github.com/beeper/ai-bridge/pkg/ai-stream"
)

func (r aiRunner) runLorem(ctx context.Context, w *aistream.Writer, cmd loremCommand) error {
	opts := cmd.Options
	rng := rngForOptions(opts.SeedSet, opts.Seed, r.runtime.now().UnixNano())
	steps := max(opts.Steps, 1)
	text := buildDemoVisibleText(cmd.Chars, rand.New(rand.NewSource(rng.Int63())))
	reasoning := buildLoremText(opts.ReasoningChars, rand.New(rand.NewSource(rng.Int63())))
	for step := range steps {
		if opts.Steps > 0 {
			w.StepStart(fmt.Sprintf("step-%d", step+1))
		}
		emitDecorations(w, opts, cmd.Chars, step, steps)
		if reasoning != "" {
			w.Thinking(sliceByStep(reasoning, steps, step))
		}
		for _, chunk := range chunkText(sliceByStep(text, steps, step), rng, opts.ChunkMin, opts.ChunkMax) {
			w.Text(chunk)
			if err := r.runtime.sleep(ctx, r.sampleDelay(rng, opts.DelayMin, opts.DelayMax)); err != nil {
				return err
			}
		}
		if opts.Steps > 0 {
			w.StepFinish(fmt.Sprintf("step-%d", step+1))
		}
	}
	finishWriter(w, opts)
	return nil
}

func (r aiRunner) runTools(ctx context.Context, w *aistream.Writer, cmd toolsCommand) error {
	opts := cmd.Options
	rng := rngForOptions(opts.SeedSet, opts.Seed, r.runtime.now().UnixNano())
	phaseCount := max(len(cmd.Tools)+1, max(opts.Steps, 1))
	text := buildDemoVisibleText(cmd.Chars, rand.New(rand.NewSource(rng.Int63())))
	reasoning := buildLoremText(opts.ReasoningChars, rand.New(rand.NewSource(rng.Int63())))
	for phase := range phaseCount {
		w.StepStart(fmt.Sprintf("phase-%d", phase+1))
		emitDecorations(w, opts, cmd.Chars, phase, phaseCount)
		if reasoning != "" {
			w.Thinking(sliceByStep(reasoning, phaseCount, phase))
		}
		for _, chunk := range chunkText(sliceByStep(text, phaseCount, phase), rng, opts.ChunkMin, opts.ChunkMax) {
			w.Text(chunk)
		}
		if phase < len(cmd.Tools) {
			if err := r.runToolSpec(ctx, w, cmd.Tools[phase], rng, opts); err != nil {
				if errors.Is(err, errApprovalRequested) {
					w.StepFinish(fmt.Sprintf("phase-%d", phase+1))
				}
				return err
			}
		}
		w.StepFinish(fmt.Sprintf("phase-%d", phase+1))
	}
	finishWriter(w, opts)
	return nil
}

func (r aiRunner) runRandom(ctx context.Context, w *aistream.Writer, cmd randomCommand) error {
	seed := cmd.Seed
	if !cmd.SeedSet {
		seed = r.runtime.now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))
	started := r.runtime.now()
	var deadline time.Time
	if cmd.Duration > 0 {
		deadline = started.Add(cmd.Duration)
	}
	stepOpen := false
	stepName := ""
	actionOptions, actionWeightTotal := buildRandomActionOptions(cmd)
	if cmd.Chars > 0 {
		text := buildDemoVisibleText(cmd.Chars, rand.New(rand.NewSource(rng.Int63())))
		for _, chunk := range chunkText(text, rng, defaultChunkMin, defaultChunkMax) {
			w.Text(chunk)
			if err := r.runtime.sleep(ctx, r.sampleDelay(rng, cmd.DelayMin, cmd.DelayMax)); err != nil {
				return err
			}
		}
	}
	approvalRequested := false
	handleTool := func(spec toolSpec) error {
		if err := r.runToolSpec(ctx, w, spec, rng, defaultCommonOptions()); err != nil {
			if spec.Approval {
				approvalRequested = true
			}
			if errors.Is(err, errApprovalRequested) && stepOpen {
				w.StepFinish(stepName)
				stepOpen = false
				stepName = ""
			}
			return err
		}
		if spec.Approval {
			approvalRequested = true
		}
		return nil
	}
	for action := range cmd.Actions {
		if !deadline.IsZero() && !r.runtime.now().Before(deadline) {
			break
		}
		if action > 0 {
			delay := r.sampleDelay(rng, cmd.DelayMin, cmd.DelayMax)
			if !deadline.IsZero() && r.runtime.now().Add(delay).After(deadline) {
				delay = deadline.Sub(r.runtime.now())
			}
			if err := r.runtime.sleep(ctx, delay); err != nil {
				return err
			}
			if !deadline.IsZero() && !r.runtime.now().Before(deadline) {
				break
			}
		}
		switch pickWeighted(actionOptions, actionWeightTotal, rng) {
		case randomActionText:
			text := "\n\n" + buildDemoVisibleText(40+rng.Intn(160), rand.New(rand.NewSource(rng.Int63())))
			for _, chunk := range chunkText(text, rng, defaultChunkMin, defaultChunkMax) {
				w.Text(chunk)
			}
		case randomActionThinking:
			w.Thinking(buildLoremText(30+rng.Intn(120), rand.New(rand.NewSource(rng.Int63()))))
		case randomActionStep:
			if stepOpen {
				w.StepFinish(stepName)
				stepOpen = false
				stepName = ""
			} else {
				stepName = fmt.Sprintf("random-step-%d", action+1)
				w.StepStart(stepName)
				stepOpen = true
			}
		case randomActionTool:
			if cmd.AllowApproval && cmd.Profile == "balanced" && action >= 10 && !approvalRequested {
				if err := handleTool(toolSpec{Name: randomToolName(rng), Approval: true, SequenceIndex: action + 1}); err != nil {
					return err
				}
				continue
			}
			if err := handleTool(toolSpec{Name: randomToolName(rng), SequenceIndex: action + 1}); err != nil {
				return err
			}
		case randomActionToolFail:
			if err := handleTool(toolSpec{Name: randomToolName(rng), Fail: true, SequenceIndex: action + 1}); err != nil {
				return err
			}
		case randomActionToolDeny:
			if err := handleTool(toolSpec{Name: randomToolName(rng), Deny: true, SequenceIndex: action + 1}); err != nil {
				return err
			}
		case randomActionToolApproval:
			if err := handleTool(toolSpec{Name: randomToolName(rng), Approval: true, SequenceIndex: action + 1}); err != nil {
				return err
			}
		case randomActionSource:
			sourceID := fmt.Sprintf("random-source-%d", action+1)
			w.Custom("com.beeper.source", map[string]any{"sourceId": sourceID, "url": fmt.Sprintf("https://dummybridge.local/random/source/%d", action+1), "title": fmt.Sprintf("Random Source %d", action+1)})
		case randomActionDocument:
			w.Custom("com.beeper.document", map[string]any{"id": fmt.Sprintf("random-doc-%d", action+1), "title": fmt.Sprintf("Random Document %d", action+1), "mediaType": "text/plain"})
		case randomActionFile:
			w.Custom("com.beeper.file", map[string]any{"url": fmt.Sprintf("mxc://dummybridge/random-file-%d", action+1), "mediaType": "application/octet-stream"})
		case randomActionMetadata:
			w.StateDelta(statePatch(map[string]any{"command": "stream", "seed": seed, "action": action + 1, "profile": cmd.Profile}))
		case randomActionData:
			w.Custom("com.beeper.data", map[string]any{"name": "random", "value": map[string]any{"action": action + 1, "seed": seed}})
		case randomActionDataTransient:
			w.Custom("com.beeper.data.transient", map[string]any{"name": "random", "value": map[string]any{"action": action + 1, "seed": seed}})
		}
	}
	if stepOpen {
		w.StepFinish(stepName)
	}
	terminal := chooseRandomTerminal(cmd, rng)
	switch terminal {
	case "abort":
		w.Abort("DummyBridge random mode aborted")
	case "error":
		w.Error("DummyBridge random mode failed")
	case agui.FinishReasonLength, agui.FinishReasonToolCalls, agui.FinishReasonContentFilter, agui.FinishReasonOther:
		w.Finish(terminal)
	default:
		w.Finish(agui.FinishReasonStop)
	}
	return nil
}

func (r aiRunner) runToolSpec(ctx context.Context, w *aistream.Writer, spec toolSpec, rng *rand.Rand, opts commonCommandOptions) error {
	toolCallID := fmt.Sprintf("dummy-tool-%d-%s", spec.SequenceIndex, sanitizeToolName(spec.Name))
	input := toolRequestInput(spec)
	approvalID := approvalIDForRun(w.Run.RunID, toolCallID)
	var approval *agui.ToolApproval
	if spec.Approval {
		approval = &agui.ToolApproval{ID: approvalID, NeedsApproval: true}
	}
	displayMetadata := toolDisplayMetadata(spec.Name)
	w.ToolStartWithMetadata(toolCallID, spec.Name, spec.SequenceIndex-1, approval, displayMetadata)
	annotateProviderRawEvent(w, spec, "tool_call_start")
	if spec.InputError {
		if encodedInput := jsonToolInput(input); encodedInput != "" {
			w.ToolArgs(toolCallID, encodedInput, nil)
			annotateProviderRawEvent(w, spec, "tool_call_args")
		}
		w.ToolError(toolCallID, spec.Name, input, "input-error")
		annotateProviderRawEvent(w, spec, "tool_call_error")
		return nil
	}
	if spec.Delta {
		if encodedInput := jsonToolInput(input); encodedInput != "" {
			for _, chunk := range chunkText(encodedInput, rng, opts.ChunkMin, opts.ChunkMax) {
				w.ToolArgs(toolCallID, chunk, nil)
				annotateProviderRawEvent(w, spec, "tool_call_args")
				if err := r.runtime.sleep(ctx, r.sampleDelay(rng, opts.DelayMin, opts.DelayMax)); err != nil {
					return err
				}
			}
		}
	} else {
		if encodedInput := jsonToolInput(input); encodedInput != "" {
			w.ToolArgs(toolCallID, encodedInput, encodedInput)
			annotateProviderRawEvent(w, spec, "tool_call_args")
		}
	}
	if spec.Preliminary {
		w.ToolResult(toolCallID, fmt.Sprintf(`{"state":%q}`, agui.ToolResultStateStreaming), agui.ToolResultStateStreaming)
		annotateProviderRawEvent(w, spec, "tool_call_result")
	}
	switch {
	case spec.Approval:
		if response, ok := r.approvals[approvalID]; ok {
			if response.ID == "" {
				response.ID = approvalID
			}
			w.ToolApprovalResponded(toolCallID, spec.Name, input, response)
			annotateProviderRawEvent(w, spec, "approval_responded")
			if !response.Approved {
				return errApprovalDenied
			}
			return nil
		}
		w.ToolApprovalInputComplete(toolCallID, spec.Name, input)
		annotateProviderRawEvent(w, spec, "tool_call_input_complete")
		w.ToolApprovalRequestedWithMetadata(toolCallID, spec.Name, input, *approval, displayMetadata)
		annotateProviderRawEvent(w, spec, "approval_requested")
		return errApprovalRequested
	case spec.Deny:
		w.ToolDenied(toolCallID, spec.Name, input, approvalID, "denied")
		annotateProviderRawEvent(w, spec, "tool_call_denied")
	case spec.Fail:
		w.ToolError(toolCallID, spec.Name, input, "DummyBridge synthetic tool failure")
		annotateProviderRawEvent(w, spec, "tool_call_error")
	default:
		w.ToolEnd(toolCallID, spec.Name, input, nil)
		annotateProviderRawEvent(w, spec, "tool_call_end")
	}
	return nil
}

func toolRequestInput(spec toolSpec) any {
	return nil
}

func toolDisplayMetadata(name string) map[string]any {
	type ToolProviderMetadata struct {
		ID          string `json:"id,omitempty"`
		DisplayName string `json:"displayName,omitempty"`
		IconURL     string `json:"iconUrl,omitempty"`
	}
	type ToolDisplayMetadata struct {
		DisplayName string                `json:"displayName,omitempty"`
		Description string                `json:"description,omitempty"`
		IconURL     string                `json:"iconUrl,omitempty"`
		Provider    *ToolProviderMetadata `json:"provider,omitempty"`
	}

	metadata := ToolDisplayMetadata{}
	switch strings.ToLower(name) {
	case "calendar.get_events", "google_calendar.get_events", "google-calendar.get-events":
		metadata.DisplayName = "List Calendar Events"
		metadata.Provider = &ToolProviderMetadata{
			ID:          "google-calendar",
			DisplayName: "Google Calendar",
		}
	case "linear.list_issues", "linear.list-issues", "list_issues", "list-issues":
		metadata.DisplayName = "List Issues"
		metadata.Provider = &ToolProviderMetadata{
			ID:          "linear",
			DisplayName: "Linear",
		}
	case "shell":
		metadata.DisplayName = "Run Command"
	case "fetch":
		metadata.DisplayName = "Fetch Web"
	}
	return compactJSONMap(metadata)
}

func compactJSONMap(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func approvalIDForRun(runID, toolCallID string) string {
	return "approval-" + runID + "-" + toolCallID
}

func annotateProviderRawEvent(w *aistream.Writer, spec toolSpec, stage string) {
	if !spec.Provider || w == nil || w.Run == nil || len(w.Run.Events) == 0 {
		return
	}
	w.Run.Events[len(w.Run.Events)-1]["rawEvent"] = map[string]any{
		"provider": "dummybridge",
		"stage":    stage,
		"tool":     spec.Name,
		"sequence": spec.SequenceIndex,
		"tags":     spec.Tags,
	}
}

func jsonToolInput(input any) string {
	if input == nil {
		return ""
	}
	if inputMap, ok := input.(map[string]any); ok && len(inputMap) == 0 {
		return ""
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return string(raw)
}

func finishWriter(w *aistream.Writer, opts commonCommandOptions) {
	switch {
	case opts.Abort:
		w.Abort("DummyBridge synthetic abort")
	case opts.Error:
		w.Error("DummyBridge synthetic error")
	default:
		w.Finish(opts.FinishReason)
	}
}

func emitDecorations(w *aistream.Writer, opts commonCommandOptions, chars, step, steps int) {
	if opts.Meta {
		seed := opts.Seed
		if !opts.SeedSet {
			seed = int64(chars)
		}
		w.StateDelta(statePatch(map[string]any{"command": "demo", "seed": seed, "step": step + 1}))
	}
	for i := range splitCount(opts.Sources, steps, step) {
		sourceID := fmt.Sprintf("demo-source-%d-%d", step+1, i+1)
		w.Custom("com.beeper.source", map[string]any{"sourceId": sourceID, "url": fmt.Sprintf("https://dummybridge.local/source/%d-%d", step+1, i+1), "title": fmt.Sprintf("Demo Source %d.%d", step+1, i+1)})
	}
	for i := range splitCount(opts.Documents, steps, step) {
		w.Custom("com.beeper.document", map[string]any{"id": fmt.Sprintf("demo-doc-%d-%d", step+1, i+1), "title": fmt.Sprintf("Demo Document %d.%d", step+1, i+1), "mediaType": "text/plain"})
	}
	for i := range splitCount(opts.Files, steps, step) {
		w.Custom("com.beeper.file", map[string]any{"url": fmt.Sprintf("mxc://dummybridge/demo-file-%d-%d", step+1, i+1), "mediaType": "application/octet-stream"})
	}
	if step == 0 && opts.DataName != "" {
		w.Custom("com.beeper.data", map[string]any{"name": opts.DataName, "value": map[string]any{"mode": "persistent", "stage": step + 1}})
	}
	if step == 0 && opts.DataTransientName != "" {
		w.Custom("com.beeper.data.transient", map[string]any{"name": opts.DataTransientName, "value": map[string]any{"mode": "transient", "stage": step + 1}})
	}
}

func statePatch(values map[string]any) []map[string]any {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	patch := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		patch = append(patch, map[string]any{
			"op":    "add",
			"path":  "/" + key,
			"value": values[key],
		})
	}
	return patch
}

func (r aiRunner) sampleDelay(rng *rand.Rand, minDelay, maxDelay time.Duration) time.Duration {
	if maxDelay <= minDelay {
		return minDelay
	}
	return minDelay + time.Duration(rng.Int63n(int64(maxDelay-minDelay)+1))
}
