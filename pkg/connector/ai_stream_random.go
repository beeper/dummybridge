package connector

import "math/rand"

func buildRandomActionOptions(cmd randomCommand) ([]randomActionOption, int) {
	options := []randomActionOption{
		{randomActionText, 6},
		{randomActionThinking, 4},
		{randomActionStep, 2},
		{randomActionTool, 3},
		{randomActionToolFail, 1},
		{randomActionSource, 2},
		{randomActionDocument, 2},
		{randomActionFile, 2},
		{randomActionMetadata, 2},
		{randomActionData, 1},
		{randomActionDataTransient, 1},
	}
	if cmd.AllowApproval && cmd.Profile != "balanced" {
		options = append(options, randomActionOption{randomActionToolApproval, 2})
	}
	switch cmd.Profile {
	case "tools":
		options = append(options,
			randomActionOption{randomActionTool, 6},
			randomActionOption{randomActionToolFail, 4},
			randomActionOption{randomActionToolDeny, 3},
		)
		if cmd.AllowApproval {
			options = append(options, randomActionOption{randomActionToolApproval, 4})
		}
	case "artifacts":
		options = append(options,
			randomActionOption{randomActionSource, 4},
			randomActionOption{randomActionDocument, 4},
			randomActionOption{randomActionFile, 4},
			randomActionOption{randomActionMetadata, 3},
			randomActionOption{randomActionData, 3},
			randomActionOption{randomActionDataTransient, 3},
		)
	case "errors":
		options = append(options,
			randomActionOption{randomActionToolFail, 7},
			randomActionOption{randomActionToolDeny, 5},
			randomActionOption{randomActionTool, 2},
		)
		if cmd.AllowApproval {
			options = append(options, randomActionOption{randomActionToolApproval, 4})
		}
	}
	total := 0
	for _, option := range options {
		total += option.weight
	}
	return options, total
}

func pickWeighted(options []randomActionOption, total int, rng *rand.Rand) string {
	if total <= 0 || len(options) == 0 {
		return randomActionText
	}
	pick := rng.Intn(total)
	for _, option := range options {
		if pick < option.weight {
			return option.name
		}
		pick -= option.weight
	}
	return randomActionText
}

func chooseRandomTerminal(cmd randomCommand, rng *rand.Rand) string {
	if cmd.Terminal != "" {
		return cmd.Terminal
	}
	options := []string{"finish"}
	if cmd.AllowAbort {
		options = append(options, "abort")
	}
	if cmd.AllowError {
		options = append(options, "error")
	}
	return options[rng.Intn(len(options))]
}

func randomToolName(rng *rand.Rand) string {
	names := []string{"search", "fetch", "summarize", "calendar", "shell", "files", "preview"}
	return names[rng.Intn(len(names))]
}
