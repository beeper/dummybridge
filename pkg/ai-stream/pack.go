package aistream

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/beeper/dummybridge/pkg/ag-ui"
)

func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

type Envelope struct {
	ThreadID    string     `json:"threadId"`
	RunID       string     `json:"runId"`
	MessageID   string     `json:"messageId"`
	Seq         int        `json:"seq"`
	Part        agui.Event `json:"part"`
	TargetEvent string     `json:"target_event,omitempty"`
	RelatesTo   Relation   `json:"m.relates_to,omitempty"`
	AgentID     string     `json:"agent_id,omitempty"`
}

type Relation struct {
	Type    string `json:"rel_type"`
	EventID string `json:"event_id"`
}

type Carrier struct {
	Envelopes []Envelope
}

func BuildEnvelope(run Run, seq int, part agui.Event, targetEventID string) (Envelope, error) {
	if seq <= 0 {
		return Envelope{}, fmt.Errorf("stream envelope: seq must be > 0")
	}
	if err := agui.ValidateEvent(part); err != nil {
		return Envelope{}, err
	}
	targetEventID = strings.TrimSpace(targetEventID)
	if targetEventID == "" {
		return Envelope{}, fmt.Errorf("stream envelope: missing target event id")
	}
	return Envelope{
		ThreadID:    run.ThreadID,
		RunID:       run.RunID,
		MessageID:   run.MessageID,
		Seq:         seq,
		Part:        part,
		TargetEvent: targetEventID,
		RelatesTo:   Relation{Type: "m.reference", EventID: targetEventID},
		AgentID:     run.AgentID,
	}, nil
}

func PackRun(run Run, targetEventID string, budget int) ([]Carrier, error) {
	return PackRunFromSeq(run, targetEventID, budget, 1)
}

func PackRunFromSeq(run Run, targetEventID string, budget int, startSeq int) ([]Carrier, error) {
	if budget <= 0 {
		budget = CarrierBudgetBytes
	}
	if startSeq <= 0 {
		startSeq = 1
	}
	if err := run.Validate(); err != nil {
		return nil, err
	}
	var carriers []Carrier
	var current Carrier
	currentSize := 0
	emptyCarrierOverhead := JSONSize(CarrierContent([]Envelope{}))
	seq := startSeq
	for _, original := range run.Events {
		for _, part := range splitEventForBudget(original, budget) {
			env, err := BuildEnvelope(run, seq, part, targetEventID)
			if err != nil {
				return nil, err
			}
			envSize := JSONSize(env)
			if emptyCarrierOverhead+envSize > budget {
				return nil, fmt.Errorf("stream envelope %d exceeds %d byte budget", seq, budget)
			}
			// +1 for the comma separator between envelopes in the JSON array.
			addedSize := envSize
			if len(current.Envelopes) > 0 {
				addedSize++
			}
			if len(current.Envelopes) > 0 && currentSize+addedSize > budget {
				carriers = append(carriers, current)
				current = Carrier{}
				currentSize = 0
				addedSize = envSize
			}
			if len(current.Envelopes) == 0 {
				currentSize = emptyCarrierOverhead + envSize
			} else {
				currentSize += addedSize
			}
			current.Envelopes = append(current.Envelopes, env)
			seq++
		}
	}
	if len(current.Envelopes) > 0 {
		carriers = append(carriers, current)
	}
	return carriers, nil
}

func NextSeq(carriers []Carrier) int {
	next := 1
	for _, carrier := range carriers {
		for _, env := range carrier.Envelopes {
			if env.Seq >= next {
				next = env.Seq + 1
			}
		}
	}
	return next
}

func CarrierContent(envelopes []Envelope) map[string]any {
	return map[string]any{BeeperAIStreamDeltas: envelopes}
}

func ReconstructText(carriers []Carrier) string {
	var out strings.Builder
	for _, carrier := range carriers {
		for _, env := range carrier.Envelopes {
			if env.Part["type"] == agui.EventTextMessageContent {
				delta, _ := env.Part["delta"].(string)
				out.WriteString(delta)
			}
		}
	}
	return out.String()
}

func splitEventForBudget(evt agui.Event, budget int) []agui.Event {
	if evt["type"] == agui.EventMessagesSnapshot {
		return splitMessagesSnapshotForBudget(evt, budget)
	}
	if JSONSize(evt) <= budget {
		return []agui.Event{sanitizeRawEvent(evt, budget)}
	}
	if evt["type"] != agui.EventTextMessageContent {
		return []agui.Event{sanitizeRawEvent(evt, budget)}
	}
	delta, _ := evt["delta"].(string)
	if delta == "" {
		return []agui.Event{sanitizeRawEvent(evt, budget)}
	}
	maxDelta := budget / 2
	if maxDelta < 1024 {
		maxDelta = 1024
	}
	var out []agui.Event
	for _, chunk := range SplitTextUTF8(delta, maxDelta) {
		cp := agui.CloneEvent(evt)
		cp["delta"] = chunk
		out = append(out, sanitizeRawEvent(cp, budget))
	}
	return out
}

func splitMessagesSnapshotForBudget(evt agui.Event, budget int) []agui.Event {
	rawMessages, ok := evt["messages"].([]agui.UIMessage)
	if !ok || len(rawMessages) == 0 {
		return []agui.Event{sanitizeRawEvent(evt, budget)}
	}
	var out []agui.Event
	for _, message := range rawMessages {
		out = append(out, splitFinalMessageSnapshot(evt, message, budget)...)
	}
	if len(out) == 0 {
		return []agui.Event{sanitizeRawEvent(evt, budget)}
	}
	return out
}

func splitFinalMessageSnapshot(evt agui.Event, message agui.UIMessage, budget int) []agui.Event {
	base := agui.CloneEvent(evt)
	baseMessage := message
	baseMessage.Parts = nil
	base["messages"] = []agui.UIMessage{baseMessage}

	var out []agui.Event
	baseFlushed := false
	flushBase := func() {
		if baseFlushed {
			return
		}
		out = append(out, sanitizeRawEvent(base, budget))
		baseFlushed = true
	}
	appendToBase := func(part agui.MessagePart) bool {
		if baseFlushed {
			return false
		}
		nextMessage := baseMessage
		nextMessage.Parts = append(append([]agui.MessagePart{}, baseMessage.Parts...), part)
		candidate := agui.CloneEvent(base)
		candidate["messages"] = []agui.UIMessage{nextMessage}
		if JSONSize(candidate) > budget {
			return false
		}
		baseMessage = nextMessage
		base["messages"] = []agui.UIMessage{baseMessage}
		return true
	}

	var continuationParts []agui.MessagePart
	continuationOffset := 0
	flushContinuation := func() {
		if len(continuationParts) == 0 {
			return
		}
		out = append(out, finalPartsEvent(evt, message.ID, message.Metadata, continuationOffset, continuationParts))
		continuationParts = nil
	}
	addContinuation := func(partOffset int, part agui.MessagePart) {
		if len(continuationParts) > 0 && partOffset != continuationOffset+len(continuationParts) {
			flushContinuation()
		}
		if len(continuationParts) == 0 {
			continuationOffset = partOffset
		}
		candidateParts := append(append([]agui.MessagePart{}, continuationParts...), part)
		candidate := finalPartsEvent(evt, message.ID, message.Metadata, continuationOffset, candidateParts)
		if len(continuationParts) > 0 && JSONSize(candidate) > budget {
			flushContinuation()
			continuationOffset = partOffset
		}
		continuationParts = append(continuationParts, part)
	}

	for partOffset, part := range message.Parts {
		for pieceIndex, piece := range splitFinalPartForBudget(part, budget) {
			if pieceIndex == 0 && appendToBase(piece) {
				continue
			}
			flushBase()
			addContinuation(partOffset, piece)
		}
	}
	flushBase()
	flushContinuation()
	return out
}

func finalPartsEvent(base agui.Event, messageID string, metadata map[string]any, partOffset int, parts []agui.MessagePart) agui.Event {
	evt := agui.CloneEvent(base)
	evt["type"] = agui.EventCustom
	evt["name"] = FinalPartsCustomName
	delete(evt, "messages")
	runID, _ := metadata["runId"].(string)
	evt["value"] = map[string]any{
		"messageId":  messageID,
		"runId":      runID,
		"partOffset": partOffset,
		"parts":      append([]agui.MessagePart{}, parts...),
	}
	return evt
}

func splitFinalPartForBudget(part agui.MessagePart, budget int) []agui.MessagePart {
	partType, _ := part["type"].(string)
	if partType != "text" && partType != "thinking" {
		return []agui.MessagePart{part}
	}
	content, _ := part["content"].(string)
	if content == "" || JSONSize(part) <= budget/2 {
		return []agui.MessagePart{part}
	}
	maxContent := budget / 3
	if maxContent < 1024 {
		maxContent = 1024
	}
	chunks := SplitTextUTF8(content, maxContent)
	out := make([]agui.MessagePart, 0, len(chunks))
	for _, chunk := range chunks {
		cp := cloneMessagePart(part)
		cp["content"] = chunk
		out = append(out, cp)
	}
	return out
}

func cloneMessagePart(part agui.MessagePart) agui.MessagePart {
	cp := make(agui.MessagePart, len(part))
	for key, value := range part {
		cp[key] = value
	}
	return cp
}

func sanitizeRawEvent(evt agui.Event, budget int) agui.Event {
	cp := agui.CloneEvent(evt)
	if _, ok := cp["rawEvent"]; !ok {
		return cp
	}
	if JSONSize(cp) <= budget {
		return cp
	}
	raw, err := json.Marshal(cp["rawEvent"])
	if err != nil {
		delete(cp, "rawEvent")
		cp["rawEventTruncated"] = true
	} else if len(raw) > 2048 {
		cp["rawEvent"] = truncateUTF8(string(raw), 2048)
		cp["rawEventTruncated"] = true
	}
	if JSONSize(cp) > budget {
		delete(cp, "rawEvent")
		cp["rawEventTruncated"] = true
	}
	return cp
}

func StreamTxnID(runID string, seq int) string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Sprintf("ai_stream_%d", seq)
	}
	return fmt.Sprintf("ai_stream_%s_%d", runID, seq)
}
