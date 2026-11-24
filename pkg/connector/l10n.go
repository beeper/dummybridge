package connector

import (
	"go.mau.fi/util/l10n"
)

var L = l10n.NewNamespace("dummybridge")

var (
	msgFlowPasswordName              = L.Define("login.flow.password.name", "Password")
	msgFlowPasswordDescription       = L.Define("login.flow.password.description", "Log in with a password")
	msgFlowCookiesName               = L.Define("login.flow.cookies.name", "Cookies")
	msgFlowCookiesDescription        = L.Define("login.flow.cookies.description", "Log in with extracted cookies")
	msgFlowLocalStorageName          = L.Define("login.flow.localstorage.name", "Local storage")
	msgFlowLocalStorageDescription   = L.Define("login.flow.localstorage.description", "Log in with extracted local storage")
	msgFlowDisplayAndWaitName        = L.Define("login.flow.displayandwait.name", "Display and wait")
	msgFlowDisplayAndWaitDescription = L.Define("login.flow.displayandwait.description", "Log in through a remote server")
	msgFlowWebAuthnName              = L.Define("login.flow.webauthn.name", "WebAuthn")
	msgFlowWebAuthnDescription       = L.Define("login.flow.webauthn.description", "Log in with a passkey")

	msgFieldUsernameName = L.Define("login.field.username.name", "username, anything goes and it's used as the ID")
	msgFieldPasswordName = L.Define("login.field.password.name", "password, anything goes")

	msgStepDisplayAndWaitInstructions = L.Define("login.step.displayandwait.instructions", "Enter the code on https://random.mau.fi/dummy/pages/daw_submit.html")
	msgStepCompleteInstructions       = L.Define("login.step.complete.instructions", "Successfully logged in with whatever you provided")
)
