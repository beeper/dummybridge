package aistream

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beeper/dummybridge/pkg/ag-ui"
)

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
	seq := startSeq
	for _, original := range run.Events {
		for _, part := range splitEventForBudget(original, budget) {
			env, err := BuildEnvelope(run, seq, part, targetEventID)
			if err != nil {
				return nil, err
			}
			single := CarrierContent([]Envelope{env})
			if JSONSize(single) > budget {
				return nil, fmt.Errorf("stream envelope %d exceeds %d byte budget", seq, budget)
			}
			candidate := append(append([]Envelope{}, current.Envelopes...), env)
			if len(current.Envelopes) > 0 && JSONSize(CarrierContent(candidate)) > budget {
				carriers = append(carriers, current)
				current = Carrier{}
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

func eventTimestampMillis(evt agui.Event) int64 {
	switch value := evt["timestamp"].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	default:
		return 0
	}
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
	if JSONSize(evt) <= budget {
		return []agui.Event{sanitizeRawEvent(evt, budget)}
	}
	if evt["type"] == agui.EventMessagesSnapshot {
		return splitMessagesSnapshotForBudget(evt, budget)
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
		base := agui.CloneEvent(evt)
		messageWithoutParts := message
		messageWithoutParts.Parts = nil
		base["messages"] = []agui.UIMessage{messageWithoutParts}
		var current []agui.MessagePart
		flush := func() {
			if len(current) == 0 {
				return
			}
			cp := agui.CloneEvent(evt)
			msg := message
			msg.Parts = append([]agui.MessagePart{}, current...)
			cp["messages"] = []agui.UIMessage{msg}
			out = append(out, sanitizeRawEvent(cp, budget))
			current = nil
		}
		for _, part := range message.Parts {
			candidate := append(append([]agui.MessagePart{}, current...), part)
			cp := agui.CloneEvent(evt)
			msg := message
			msg.Parts = candidate
			cp["messages"] = []agui.UIMessage{msg}
			if len(current) > 0 && JSONSize(cp) > budget {
				flush()
			}
			current = append(current, part)
		}
		flush()
	}
	if len(out) == 0 {
		return []agui.Event{sanitizeRawEvent(evt, budget)}
	}
	return out
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
	if err != nil || len(raw) > 2048 {
		cp["rawEvent"] = string(raw[:min(len(raw), 2048)])
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
