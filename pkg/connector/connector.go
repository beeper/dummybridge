package connector

import (
	"context"

	"go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/database"
)

type DummyConnector struct {
	br *bridgev2.Bridge
}

var _ bridgev2.NetworkConnector = (*DummyConnector)(nil)

func (dc *DummyConnector) Init(bridge *bridgev2.Bridge) {
	dc.br = bridge
	bridge.Commands.(*commands.Processor).AddHandlers(AllCommands...)
}

func (dc *DummyConnector) Start(ctx context.Context) error {
	return nil
}

func (dc *DummyConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{}
}

func (dc *DummyConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:      "Dummy",
		NetworkURL:       "https://beeper.com",
		NetworkIcon:      "mxc://beeper.com/f6ec13a4953757f04c1714b43d1c8ec451e0bab1",
		NetworkID:        "dummy",
		BeeperBridgeType: "beeper.com/dummy",
	}
}

func (dc *DummyConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{}
}

func (dc *DummyConnector) GetConfig() (example string, data any, upgrader configupgrade.Upgrader) {
	return "", nil, configupgrade.NoopUpgrader
}

func (dc *DummyConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	login.Client = &DummyClient{
		UserLogin: login,
	}
	return nil
}

func (dc *DummyConnector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{{
		Name:        "Password",
		Description: "Log in with a password",
		ID:          "password",
	}}
}

func (dc *DummyConnector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	return &DummyLogin{User: user}, nil
}
