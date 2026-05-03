package main

import (
	"maunium.net/go/mautrix/bridgev2/matrix/mxmain"

	dummybridge "github.com/beeper/dummybridge/pkg/connector"
)

// Information to find out exactly which commit the bridge was built from.
// These are filled at build time with the -X linker flag.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	m := mxmain.BridgeMain{
		Name:        "dummybridge",
		Description: "DummyBridge demo bridge built with shared AI Chats primitives.",
		URL:         "https://github.com/beeper/dummybridge",
		Version:     "0.1.0",
		Connector:   dummybridge.NewConnector(),
	}
	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
