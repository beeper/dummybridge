package connector

import (
	"context"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

type DummyLogin struct {
	User *bridgev2.User
}

func (dl *DummyLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       "com.beeper.dummy.password",
		Instructions: "",
		UserInputParams: &bridgev2.LoginUserInputParams{
			Fields: []bridgev2.LoginInputDataField{
				{
					Type: bridgev2.LoginInputFieldTypeUsername,
					ID:   "username",
					Name: "username, anything goes and it's used as the ID",
				},
				{
					Type: bridgev2.LoginInputFieldTypePassword,
					ID:   "password",
					Name: "password, anything goes",
				},
			},
		},
	}, nil
}

func (dl *DummyLogin) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	ul, err := dl.User.NewLogin(ctx, &database.UserLogin{
		ID: networkid.UserLoginID(input["username"]),
	}, &bridgev2.NewLoginParams{
		LoadUserLogin: func(ctx context.Context, ul *bridgev2.UserLogin) error {
			ul.Client = &DummyClient{
				UserLogin: ul,
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "com.beeper.dummy.complete",
		Instructions: "Successfully logged in with whatever you provided",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: ul.ID,
			UserLogin:   ul,
		},
	}, nil
}

func (dl *DummyLogin) Cancel() {}
