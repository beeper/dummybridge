package connector

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	"go.mau.fi/util/shlex"
)

func parseCommand(input string) (*parsedCommand, error) {
	tokens, err := shlex.Split(input)
	if err != nil {
		return nil, fmt.Errorf("invalid command syntax: %w", err)
	}
	if len(tokens) == 0 {
		return &parsedCommand{Name: "help"}, nil
	}
	switch strings.ToLower(tokens[0]) {
	case "help", "/help", "!help", "dummybridge":
		return &parsedCommand{Name: "help"}, nil
	case "stream-tools":
		cmd, err := parseToolsCommand(tokens[1:])
		return &parsedCommand{Name: "stream-tools", Tools: cmd}, err
	case "stream":
		cmd, err := parseStreamCommand(tokens[1:])
		return &parsedCommand{Name: "stream", Random: cmd}, err
	default:
		return nil, fmt.Errorf("unknown AI demo command %q", tokens[0])
	}
}

func helpText() string {
	return strings.Join([]string{
		"DummyBridge demo commands:",
		"help",
		"stream [seconds] [--runs=N] [--profile=balanced|tools|errors|artifacts] [--seed=N] [--chars=N] [--terminal=stop|length|abort|error] [--delay-ms=min:max] [--stagger-ms=min:max] [--actions=N] [--no-approval] [--allow-abort] [--allow-error]",
		"stream-tools <chars> <tool[#fail|#approval|#deny|#delta|#inputerror|#prelim|#provider]>... [common options]",
		"Notes: stream enables approval requests by default; approval-tagged tools emit a separate Matrix approval event with reaction options.",
	}, "\n")
}

func defaultCommonOptions() commonCommandOptions {
	return commonCommandOptions{
		DelayMin:     30 * time.Millisecond,
		DelayMax:     150 * time.Millisecond,
		ChunkMin:     defaultChunkMin,
		ChunkMax:     defaultChunkMax,
		FinishReason: agui.FinishReasonStop,
	}
}

func parseLoremCommand(tokens []string) (*loremCommand, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("text stream requires a character count")
	}
	count, err := parsePositiveInt(tokens[0], "character count")
	if err != nil {
		return nil, err
	}
	if err := validateMaxIntValue(count, maxDemoChars, "character count"); err != nil {
		return nil, err
	}
	opts, err := parseCommonOptions(tokens[1:])
	if err != nil {
		return nil, err
	}
	return &loremCommand{Chars: count, Options: opts}, nil
}

func parseToolsCommand(tokens []string) (*toolsCommand, error) {
	if len(tokens) < 2 {
		return nil, fmt.Errorf("stream-tools requires a character count and at least one tool")
	}
	count, err := parsePositiveInt(tokens[0], "character count")
	if err != nil {
		return nil, err
	}
	if err := validateMaxIntValue(count, maxDemoChars, "character count"); err != nil {
		return nil, err
	}
	var toolTokens, optTokens []string
	for _, token := range tokens[1:] {
		if strings.HasPrefix(token, "--") {
			optTokens = append(optTokens, token)
		} else {
			toolTokens = append(toolTokens, token)
		}
	}
	if len(toolTokens) == 0 {
		return nil, fmt.Errorf("stream-tools requires at least one tool spec")
	}
	if err := validateMaxIntValue(len(toolTokens), maxDemoToolSpecs, "tool spec count"); err != nil {
		return nil, err
	}
	opts, err := parseCommonOptions(optTokens)
	if err != nil {
		return nil, err
	}
	tools := make([]toolSpec, 0, len(toolTokens))
	for idx, token := range toolTokens {
		spec, err := parseToolSpec(token, idx)
		if err != nil {
			return nil, err
		}
		tools = append(tools, spec)
	}
	return &toolsCommand{Chars: count, Tools: tools, Options: opts}, nil
}

func parseRandomCommand(tokens []string) (*randomCommand, error) {
	cmd := &randomCommand{
		Duration:            20 * time.Second,
		Actions:             20,
		DelayMin:            350 * time.Millisecond,
		DelayMax:            1150 * time.Millisecond,
		Runs:                1,
		StaggerMin:          150 * time.Millisecond,
		StaggerMax:          900 * time.Millisecond,
		sharedStreamOptions: sharedStreamOptions{Profile: "balanced"},
	}
	return parseStreamLikeCommand(tokens, cmd, false)
}

func parseStreamCommand(tokens []string) (*randomCommand, error) {
	cmd := &randomCommand{
		Duration:            20 * time.Second,
		DelayMin:            350 * time.Millisecond,
		DelayMax:            1150 * time.Millisecond,
		Runs:                1,
		StaggerMin:          150 * time.Millisecond,
		StaggerMax:          900 * time.Millisecond,
		sharedStreamOptions: sharedStreamOptions{Profile: "balanced", AllowApproval: true},
	}
	return parseStreamLikeCommand(tokens, cmd, true)
}

func parseStreamLikeCommand(tokens []string, cmd *randomCommand, deriveActions bool) (*randomCommand, error) {
	rest := tokens
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		seconds, err := parsePositiveInt(rest[0], "duration")
		if err != nil {
			return nil, err
		}
		if err := validateMaxIntValue(seconds, int(maxDemoDuration/time.Second), "duration seconds"); err != nil {
			return nil, err
		}
		cmd.Duration = time.Duration(seconds) * time.Second
		rest = rest[1:]
	}
	if deriveActions && cmd.Actions == 0 {
		cmd.Actions = max(3, min(maxDemoRandomActions, int(cmd.Duration/time.Second)*2))
	}
	for _, token := range rest {
		key, value, hasValue := parseOptionToken(token)
		switch key {
		case "actions":
			n, err := parseValidatedInt(value, hasValue, token, "actions", maxDemoRandomActions, false)
			if err != nil {
				return nil, err
			}
			cmd.Actions = n
		case "chars":
			n, err := parseValidatedInt(value, hasValue, token, "character count", maxDemoChars, false)
			if err != nil {
				return nil, err
			}
			cmd.Chars = n
		case "delay-ms":
			minDelay, maxDelay, err := parseDurationRangeMS(value, hasValue, token)
			if err != nil {
				return nil, err
			}
			cmd.DelayMin, cmd.DelayMax = minDelay, maxDelay
		case "terminal":
			if !hasValue {
				return nil, fmt.Errorf("%s requires a value", token)
			}
			switch strings.ToLower(value) {
			case "stop", "finish":
				cmd.Terminal = "finish"
			case "abort", "error":
				cmd.Terminal = strings.ToLower(value)
			case "length", "tool-calls", "content-filter", "other":
				cmd.Terminal = agui.NormalizeFinishReason(value)
			default:
				return nil, fmt.Errorf("unknown terminal %q", value)
			}
		case "runs":
			n, err := parseValidatedInt(value, hasValue, token, "run count", maxDemoChaosRuns, false)
			if err != nil {
				return nil, err
			}
			cmd.Runs = n
		case "stagger-ms":
			minDelay, maxDelay, err := parseDurationRange(value, hasValue, token, "stagger-ms", maxDemoStagger)
			if err != nil {
				return nil, err
			}
			cmd.StaggerMin, cmd.StaggerMax = minDelay, maxDelay
		case "no-approval":
			cmd.AllowApproval = false
		default:
			handled, err := parseSharedStreamOption(key, value, hasValue, token, &cmd.sharedStreamOptions)
			if err != nil || !handled {
				if err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("unknown stream option %q", token)
			}
		}
	}
	return cmd, nil
}

func parseChaosCommand(tokens []string) (*chaosCommand, error) {
	cmd := &chaosCommand{
		Runs:                3,
		Duration:            10 * time.Second,
		StaggerMin:          150 * time.Millisecond,
		StaggerMax:          900 * time.Millisecond,
		MaxActions:          10,
		sharedStreamOptions: sharedStreamOptions{Profile: "balanced"},
	}
	rest := tokens
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		n, err := parsePositiveInt(rest[0], "run count")
		if err != nil {
			return nil, err
		}
		if err := validateMaxIntValue(n, maxDemoChaosRuns, "run count"); err != nil {
			return nil, err
		}
		cmd.Runs = n
		rest = rest[1:]
	}
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		seconds, err := parsePositiveInt(rest[0], "duration")
		if err != nil {
			return nil, err
		}
		if err := validateMaxIntValue(seconds, int(maxDemoDuration/time.Second), "duration seconds"); err != nil {
			return nil, err
		}
		cmd.Duration = time.Duration(seconds) * time.Second
		rest = rest[1:]
	}
	for _, token := range rest {
		key, value, hasValue := parseOptionToken(token)
		switch key {
		case "stagger-ms":
			minDelay, maxDelay, err := parseDurationRange(value, hasValue, token, "stagger-ms", maxDemoStagger)
			if err != nil {
				return nil, err
			}
			cmd.StaggerMin, cmd.StaggerMax = minDelay, maxDelay
		case "max-actions":
			n, err := parseValidatedInt(value, hasValue, token, "max-actions", maxDemoChaosActions, false)
			if err != nil {
				return nil, err
			}
			cmd.MaxActions = n
		default:
			handled, err := parseSharedStreamOption(key, value, hasValue, token, &cmd.sharedStreamOptions)
			if err != nil || !handled {
				if err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("unknown chaos option %q", token)
			}
		}
	}
	return cmd, nil
}

func parseCommonOptions(tokens []string) (commonCommandOptions, error) {
	opts := defaultCommonOptions()
	for _, token := range tokens {
		key, value, hasValue := parseOptionToken(token)
		switch key {
		case "reasoning":
			n, err := parseValidatedInt(value, hasValue, token, "reasoning", maxDemoReasoningChars, true)
			if err != nil {
				return opts, err
			}
			opts.ReasoningChars = n
		case "steps":
			n, err := parseValidatedInt(value, hasValue, token, "steps", maxDemoSteps, false)
			if err != nil {
				return opts, err
			}
			opts.Steps = n
		case "sources":
			n, err := parseValidatedInt(value, hasValue, token, "sources", maxDemoCollections, true)
			if err != nil {
				return opts, err
			}
			opts.Sources = n
		case "documents":
			n, err := parseValidatedInt(value, hasValue, token, "documents", maxDemoCollections, true)
			if err != nil {
				return opts, err
			}
			opts.Documents = n
		case "files":
			n, err := parseValidatedInt(value, hasValue, token, "files", maxDemoCollections, true)
			if err != nil {
				return opts, err
			}
			opts.Files = n
		case "meta":
			opts.Meta = true
		case "data":
			if !hasValue {
				return opts, fmt.Errorf("%s requires a value", token)
			}
			opts.DataName = value
		case "data-transient":
			if !hasValue {
				return opts, fmt.Errorf("%s requires a value", token)
			}
			opts.DataTransientName = value
		case "delay-ms":
			minDelay, maxDelay, err := parseDurationRangeMS(value, hasValue, token)
			if err != nil {
				return opts, err
			}
			opts.DelayMin, opts.DelayMax = minDelay, maxDelay
		case "chunk-chars":
			minChunk, maxChunk, err := parseIntRangeOption(value, hasValue, token, "chunk-chars", maxDemoChunkChars)
			if err != nil {
				return opts, err
			}
			opts.ChunkMin, opts.ChunkMax = minChunk, maxChunk
		case "seed":
			if !hasValue {
				return opts, fmt.Errorf("%s requires a value", token)
			}
			seed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return opts, fmt.Errorf("invalid seed %q", value)
			}
			opts.Seed, opts.SeedSet = seed, true
		case "finish":
			if !hasValue {
				return opts, fmt.Errorf("%s requires a value", token)
			}
			opts.FinishReason = agui.NormalizeFinishReason(value)
		case "abort":
			opts.Abort = true
		case "error":
			opts.Error = true
		default:
			return opts, fmt.Errorf("unknown option %q", token)
		}
	}
	if opts.Abort && opts.Error {
		return opts, fmt.Errorf("--abort and --error cannot be combined")
	}
	if (opts.Abort || opts.Error) && opts.FinishReason != agui.FinishReasonStop {
		return opts, fmt.Errorf("--finish cannot be combined with --abort or --error")
	}
	return opts, nil
}

func parseSharedStreamOption(key, value string, hasValue bool, token string, opts *sharedStreamOptions) (bool, error) {
	switch key {
	case "profile":
		if !hasValue {
			return false, fmt.Errorf("%s requires a value", token)
		}
		switch strings.ToLower(value) {
		case "balanced", "tools", "errors", "artifacts":
			opts.Profile = strings.ToLower(value)
		default:
			return false, fmt.Errorf("unknown profile %q", value)
		}
	case "seed":
		if !hasValue {
			return false, fmt.Errorf("%s requires a value", token)
		}
		seed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return false, fmt.Errorf("invalid seed %q", value)
		}
		opts.Seed, opts.SeedSet = seed, true
	case "allow-abort":
		opts.AllowAbort = true
	case "allow-error":
		opts.AllowError = true
	default:
		return false, nil
	}
	return true, nil
}

func parseToolSpec(raw string, idx int) (toolSpec, error) {
	parts := strings.Split(raw, "#")
	spec := toolSpec{Name: strings.TrimSpace(parts[0]), SequenceIndex: idx + 1}
	if spec.Name == "" {
		return spec, fmt.Errorf("tool spec %q is missing a tool name", raw)
	}
	for _, tag := range parts[1:] {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		spec.Tags = append(spec.Tags, tag)
		switch tag {
		case "fail":
			spec.Fail = true
		case "approval":
			spec.Approval = true
		case "deny":
			spec.Deny = true
		case "delta":
			spec.Delta = true
		case "inputerror":
			spec.InputError = true
		case "prelim":
			spec.Preliminary = true
		case "provider":
			spec.Provider = true
		default:
			return spec, fmt.Errorf("unknown tool tag %q in %q", tag, raw)
		}
	}
	finalStates := 0
	for _, enabled := range []bool{spec.Fail, spec.Approval, spec.Deny} {
		if enabled {
			finalStates++
		}
	}
	if finalStates > 1 {
		return spec, fmt.Errorf("tool spec %q has conflicting final state tags", raw)
	}
	return spec, nil
}
