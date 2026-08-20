package connector

import (
	"encoding/json"
	"testing"

	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

// The clients key off these exact strings. A typo here would render every generated placeholder
// as an ordinary image and silently skip the resolve flow, so pin the wire shape.
func TestUnresolvedMediaMarkerWireShape(t *testing.T) {
	base := &event.MessageEventContent{
		MsgType: event.MsgImage,
		Body:    "caption",
		URL:     "mxc://example.org/preview",
	}
	raw, err := json.Marshal(&unresolvedMediaContent{Kind: "permanent", Base: filterUnresolvedMediaContent(base)})
	if err != nil {
		t.Fatalf("failed to marshal marker: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("failed to unmarshal marker: %v", err)
	}
	if _, ok := decoded["kind"]; !ok {
		t.Errorf("marker is missing 'kind', got %s", raw)
	}
	if _, ok := decoded["fi.mau.instagram.base_part"]; !ok {
		t.Errorf("marker is missing 'fi.mau.instagram.base_part', got %s", raw)
	}

	// ResolveMedia rejects a marker without a base part, so it must always be populated.
	var roundTripped unresolvedMediaContent
	if err := json.Unmarshal(raw, &roundTripped); err != nil {
		t.Fatalf("failed to round-trip marker: %v", err)
	}
	if roundTripped.Base == nil {
		t.Fatal("base part did not survive the round trip")
	}
	if roundTripped.Base.URL != base.URL {
		t.Errorf("preview URL changed: got %q, want %q", roundTripped.Base.URL, base.URL)
	}
}

// The resolve behaviour is recovered from the message ID rather than from anything the client
// sends, so the encoding has to round-trip for every supported mode.
func TestUnresolvedModeRoundTripsThroughMessageID(t *testing.T) {
	for _, mode := range unresolvedModes {
		id := unresolvedMessageID(mode)
		if got := unresolvedModeFromMessageID(id); got != mode {
			t.Errorf("mode %q became %q via ID %q", mode, got, id)
		}
	}
}

// Messages the command did not generate must not be treated as resolvable.
func TestUnresolvedModeFromForeignMessageID(t *testing.T) {
	for _, id := range []networkid.MessageID{"", "abc123", "unresolved", "resolved-image-1"} {
		if got := unresolvedModeFromMessageID(id); got != "" {
			t.Errorf("expected no mode for ID %q, got %q", id, got)
		}
	}
}
