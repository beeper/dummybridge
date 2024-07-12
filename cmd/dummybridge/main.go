package main

import (
	"maunium.net/go/mautrix/bridgev2/matrix/mxmain"

	"github.com/beeper/dummybridge/pkg/connector"
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
		Description: "An echo bridge for testing",
		URL:         "https://github.com/beeper/dummybridge",
		Version:     "0.0.1",
		Connector:   &connector.DummyConnector{},
	}
	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
