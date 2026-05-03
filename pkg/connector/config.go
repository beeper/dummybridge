package dummybridge

import (
	_ "embed"

	"go.mau.fi/util/configupgrade"
)

const ProviderDummyBridge = "dummybridge"

//go:embed example-config.yaml
var exampleNetworkConfig string

type Config struct {
	Bridge      BridgeConfig      `yaml:"bridge"`
	DummyBridge DummyBridgeConfig `yaml:"dummybridge"`
}

type BridgeConfig struct {
	CommandPrefix string `yaml:"command_prefix"`
}

type DummyBridgeConfig struct {
	Enabled *bool `yaml:"enabled"`
}

func upgradeConfig(_ configupgrade.Helper) {}
