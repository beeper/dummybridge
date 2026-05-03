package dummybridge

import (
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/beeper/ai-chats/pkg/shared/aihelpers"
)

const (
	dummyAgentIdentifierPrimary = "dummybridge"
	dummyAgentIdentifierShort   = "dummy"
	dummyAgentName              = "DummyBridge"
)

var dummyAgentUserID = networkid.UserID(dummyAgentIdentifierPrimary)

func dummySDKAgent() *aihelpers.Agent {
	return &aihelpers.Agent{
		ID:          string(dummyAgentUserID),
		Name:        dummyAgentName,
		Description: "Synthetic demo agent for streaming, turns, tools, and approvals.",
		Identifiers: []string{
			dummyAgentIdentifierPrimary,
			dummyAgentIdentifierShort,
		},
		Capabilities: aihelpers.BaseAgentCapabilities(),
	}
}
