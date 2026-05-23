package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/beeper/dummybridge/pkg/ag-ui"
	"github.com/beeper/dummybridge/pkg/ai-stream"
	"go.mau.fi/util/shlex"
)

var (
	errApprovalRequested = errors.New("approval requested")
	errApprovalDenied    = errors.New("approval denied")
)

const (
	defaultChunkMin       = 24
	defaultChunkMax       = 96
	maxDemoChars          = 96 * 1024
	maxDemoReasoningChars = 8192
	maxDemoToolSpecs      = 16
	maxDemoSteps          = 32
	maxDemoCollections    = 16
	maxDemoRandomActions  = 64
	maxDemoChaosRuns      = 16
	maxDemoChaosActions   = 64
	maxDemoDuration       = 5 * time.Minute
	maxDemoDelay          = 30 * time.Second
	maxDemoChunkChars     = 512
	maxDemoStagger        = 30 * time.Second
)

const (
	randomActionText          = "text"
	randomActionThinking      = "thinking"
	randomActionStep          = "step"
	randomActionTool          = "tool"
	randomActionToolFail      = "tool_fail"
	randomActionToolDeny      = "tool_deny"
	randomActionToolApproval  = "tool_approval"
	randomActionSource        = "source"
	randomActionDocument      = "document"
	randomActionFile          = "file"
	randomActionMetadata      = "metadata"
	randomActionData          = "data"
	randomActionDataTransient = "data_transient"
)

type commonCommandOptions struct {
	ReasoningChars    int
	Steps             int
	Sources           int
	Documents         int
	Files             int
	Meta              bool
	DataName          string
	DataTransientName string
	DelayMin          time.Duration
	DelayMax          time.Duration
	ChunkMin          int
	ChunkMax          int
	FinishReason      string
	Abort             bool
	Error             bool
	Seed              int64
	SeedSet           bool
}

type loremCommand struct {
	Chars   int
	Options commonCommandOptions
}

type toolSpec struct {
	Name          string
	Tags          []string
	Fail          bool
	Approval      bool
	Deny          bool
	Delta         bool
	InputError    bool
	Preliminary   bool
	Provider      bool
	SequenceIndex int
}

type toolsCommand struct {
	Chars   int
	Tools   []toolSpec
	Options commonCommandOptions
}

type sharedStreamOptions struct {
	Profile       string
	Seed          int64
	SeedSet       bool
	AllowAbort    bool
	AllowError    bool
	AllowApproval bool
}

type randomCommand struct {
	Duration time.Duration
	Actions  int
	DelayMin time.Duration
	DelayMax time.Duration
	sharedStreamOptions
}

type randomActionOption struct {
	name   string
	weight int
}

type chaosCommand struct {
	Runs       int
	Duration   time.Duration
	StaggerMin time.Duration
	StaggerMax time.Duration
	MaxActions int
	sharedStreamOptions
}

type parsedCommand struct {
	Name   string
	Lorem  *loremCommand
	Tools  *toolsCommand
	Random *randomCommand
	Chaos  *chaosCommand
}

type aiRuntime struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

type aiRunner struct {
	runtime   aiRuntime
	approvals map[string]agui.ToolApprovalResponse
}

type aiRunPlan struct {
	Run   *aistream.Run
	Delay time.Duration
	// EffectiveCommand is the canonical command form used to deterministically
	// replay this run during approval continuation. For random/chaos sub-runs
	// (where the seed was derived implicitly) this includes the resolved
	// --seed=N so the continuation reproduces the same action sequence.
	EffectiveCommand string
}

func virtualAIRuntime(now time.Time) aiRuntime {
	current := now
	return aiRuntime{
		now: func() time.Time {
			return current
		},
		sleep: func(ctx context.Context, delay time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if delay > 0 {
				current = current.Add(delay)
			}
			return nil
		},
	}
}

func buildAIRun(ctx context.Context, runID, threadID, input string, now time.Time) (*aistream.Run, error) {
	plans, err := buildAIRunPlans(ctx, runID, threadID, input, now, "ai", "AI")
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("no AI runs built")
	}
	return plans[0].Run, nil
}

func buildAIRunPlans(ctx context.Context, runID, threadID, input string, now time.Time, agentID, agentName string) ([]aiRunPlan, error) {
	cmd, err := parseCommand(input)
	if err != nil {
		run := aistream.NewRun(runID, threadID, aistream.DefaultModel, agentID, agentName, now)
		writer := aistream.NewWriter(run, func() time.Time { return now })
		writer.Start()
		writer.Text(err.Error() + "\n\n" + helpText())
		writer.Finish(agui.FinishReasonStop)
		return []aiRunPlan{{Run: run, EffectiveCommand: input}}, nil
	}
	if cmd != nil && cmd.Chaos != nil {
		return buildAIChaosRunPlans(ctx, runID, threadID, now, *cmd.Chaos, agentID, agentName)
	}
	resolveCommandSeed(cmd, now)
	run, err := buildAIRunFromCommand(ctx, runID, threadID, now, cmd, agentID, agentName)
	if err != nil {
		return nil, err
	}
	return []aiRunPlan{{Run: run, EffectiveCommand: canonicalCommand(input, cmd)}}, nil
}

// resolveCommandSeed fills in an implicit seed for commands that derive their
// random behavior from the current time, so the continuation can replay the
// exact same sequence.
func resolveCommandSeed(cmd *parsedCommand, now time.Time) {
	if cmd == nil {
		return
	}
	switch {
	case cmd.Lorem != nil && !cmd.Lorem.Options.SeedSet:
		cmd.Lorem.Options.Seed = now.UnixNano()
		cmd.Lorem.Options.SeedSet = true
	case cmd.Tools != nil && !cmd.Tools.Options.SeedSet:
		cmd.Tools.Options.Seed = now.UnixNano()
		cmd.Tools.Options.SeedSet = true
	case cmd.Random != nil && !cmd.Random.SeedSet:
		cmd.Random.Seed = now.UnixNano()
		cmd.Random.SeedSet = true
	}
}

// canonicalCommand returns a command string that, when re-parsed, reproduces
// the same run as cmd. If the original input already encoded all randomness
// inputs (e.g. an explicit --seed), it is returned as-is.
func canonicalCommand(input string, cmd *parsedCommand) string {
	if cmd == nil {
		return input
	}
	switch {
	case cmd.Lorem != nil:
		return ensureSeedFlag(input, cmd.Lorem.Options.Seed, cmd.Lorem.Options.SeedSet)
	case cmd.Tools != nil:
		return ensureSeedFlag(input, cmd.Tools.Options.Seed, cmd.Tools.Options.SeedSet)
	case cmd.Random != nil:
		return ensureSeedFlag(input, cmd.Random.Seed, cmd.Random.SeedSet)
	}
	return input
}

func ensureSeedFlag(input string, seed int64, seedSet bool) string {
	if !seedSet || hasSeedFlag(input) {
		return input
	}
	return strings.TrimRight(input, " ") + " --seed=" + strconv.FormatInt(seed, 10)
}

func hasSeedFlag(input string) bool {
	for _, token := range strings.Fields(input) {
		if strings.HasPrefix(token, "--seed=") || token == "--seed" {
			return true
		}
	}
	return false
}

func buildAIRunFromCommand(ctx context.Context, runID, threadID string, now time.Time, cmd *parsedCommand, agentID, agentName string) (*aistream.Run, error) {
	return buildAIRunFromCommandWithApprovals(ctx, runID, threadID, now, cmd, agentID, agentName, nil)
}

func buildAIRunFromCommandWithApprovals(ctx context.Context, runID, threadID string, now time.Time, cmd *parsedCommand, agentID, agentName string, approvals map[string]agui.ToolApprovalResponse) (*aistream.Run, error) {
	runtime := virtualAIRuntime(now)
	run := aistream.NewRun(runID, threadID, aistream.DefaultModel, agentID, agentName, now)
	writer := aistream.NewWriter(run, runtime.now)
	writer.Start()

	runner := aiRunner{runtime: runtime, approvals: approvals}
	var err error
	switch {
	case cmd == nil || cmd.Name == "help":
		writer.Text(helpText())
		writer.Finish(agui.FinishReasonStop)
	case cmd.Lorem != nil:
		err = runner.runLorem(ctx, writer, *cmd.Lorem)
	case cmd.Tools != nil:
		err = runner.runTools(ctx, writer, *cmd.Tools)
	case cmd.Random != nil:
		err = runner.runRandom(ctx, writer, *cmd.Random)
	}
	if errors.Is(err, errApprovalRequested) {
		err = nil
	}
	if err != nil {
		writer.Error(err.Error())
	} else if err = agui.ValidateEventSequence(run.Events); err != nil {
		writer.Error(err.Error())
	}
	return run, nil
}

func buildAIChaosRunPlans(ctx context.Context, baseRunID, threadID string, now time.Time, cmd chaosCommand, agentID, agentName string) ([]aiRunPlan, error) {
	seed := cmd.Seed
	if !cmd.SeedSet {
		seed = now.UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))
	runner := aiRunner{runtime: virtualAIRuntime(now)}
	actions := max(3, min(cmd.MaxActions, int(cmd.Duration/time.Second)))
	plans := make([]aiRunPlan, 0, cmd.Runs)
	for i := range cmd.Runs {
		var delay time.Duration
		if i > 0 {
			delay = runner.sampleDelay(rng, cmd.StaggerMin, cmd.StaggerMax)
		}
		runID := fmt.Sprintf("%s-%d", baseRunID, i+1)
		randomCmd := randomCommand{
			Duration: cmd.Duration,
			Actions:  actions,
			DelayMin: 180 * time.Millisecond,
			DelayMax: 900 * time.Millisecond,
			sharedStreamOptions: sharedStreamOptions{
				Profile:       cmd.Profile,
				Seed:          seed + int64(i+1)*97,
				SeedSet:       true,
				AllowAbort:    cmd.AllowAbort,
				AllowError:    cmd.AllowError,
				AllowApproval: cmd.AllowApproval,
			},
		}
		parsed := &parsedCommand{Name: "stream-random", Random: &randomCmd}
		run, err := buildAIRunFromCommand(ctx, runID, threadID, now.Add(delay), parsed, agentID, agentName)
		if err != nil {
			return nil, err
		}
		plans = append(plans, aiRunPlan{
			Run:              run,
			Delay:            delay,
			EffectiveCommand: chaosSubRunCommand(randomCmd),
		})
	}
	return plans, nil
}

// chaosSubRunCommand renders a stream-random command equivalent to the sub-run
// derived from a stream-chaos invocation. Used as the canonical command stored
// in the approval context so a chaos approval can be replayed deterministically.
func chaosSubRunCommand(cmd randomCommand) string {
	parts := []string{
		"stream-random",
		strconv.Itoa(int(cmd.Duration / time.Second)),
		"--actions=" + strconv.Itoa(cmd.Actions),
		"--delay-ms=" + strconv.Itoa(int(cmd.DelayMin/time.Millisecond)) + ":" + strconv.Itoa(int(cmd.DelayMax/time.Millisecond)),
		"--profile=" + cmd.Profile,
		"--seed=" + strconv.FormatInt(cmd.Seed, 10),
	}
	if cmd.AllowAbort {
		parts = append(parts, "--allow-abort")
	}
	if cmd.AllowError {
		parts = append(parts, "--allow-error")
	}
	if cmd.AllowApproval {
		parts = append(parts, "--allow-approval")
	}
	return strings.Join(parts, " ")
}

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
	case "stream-lorem":
		cmd, err := parseLoremCommand(tokens[1:])
		return &parsedCommand{Name: "stream-lorem", Lorem: cmd}, err
	case "stream-tools":
		cmd, err := parseToolsCommand(tokens[1:])
		return &parsedCommand{Name: "stream-tools", Tools: cmd}, err
	case "stream-random":
		cmd, err := parseRandomCommand(tokens[1:])
		return &parsedCommand{Name: "stream-random", Random: cmd}, err
	case "stream-chaos":
		cmd, err := parseChaosCommand(tokens[1:])
		return &parsedCommand{Name: "stream-chaos", Chaos: cmd}, err
	default:
		return &parsedCommand{Name: "stream-lorem", Lorem: &loremCommand{
			Chars:   min(max(len(input)*4, 120), 1200),
			Options: defaultCommonOptions(),
		}}, nil
	}
}

func helpText() string {
	return strings.Join([]string{
		"DummyBridge demo commands:",
		"help",
		"stream-lorem <chars> [--reasoning=N] [--steps=N] [--sources=N] [--documents=N] [--files=N] [--meta] [--data=name] [--data-transient=name] [--delay-ms=min:max] [--chunk-chars=min:max] [--seed=N] [--finish=stop|length|tool-calls|content-filter|other] [--abort|--error]",
		"stream-tools <chars> <tool[#fail|#approval|#deny|#delta|#inputerror|#prelim|#provider]>... [common options]",
		"stream-random [seconds] [--actions=N] [--profile=balanced|tools|artifacts|terminals] [--seed=N] [--delay-ms=min:max] [--allow-abort] [--allow-error] [--allow-approval]",
		"stream-chaos [runs] [seconds] [--profile=balanced|tools|artifacts|terminals] [--seed=N] [--stagger-ms=min:max] [--max-actions=N] [--allow-abort] [--allow-error] [--allow-approval]",
		"Notes: approval-tagged tools emit a separate Matrix approval event with reaction options.",
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
		return nil, fmt.Errorf("stream-lorem requires a character count")
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
		sharedStreamOptions: sharedStreamOptions{Profile: "balanced"},
	}
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
	for _, token := range rest {
		key, value, hasValue := parseOptionToken(token)
		switch key {
		case "actions":
			n, err := parseValidatedInt(value, hasValue, token, "actions", maxDemoRandomActions, false)
			if err != nil {
				return nil, err
			}
			cmd.Actions = n
		case "delay-ms":
			minDelay, maxDelay, err := parseDurationRangeMS(value, hasValue, token)
			if err != nil {
				return nil, err
			}
			cmd.DelayMin, cmd.DelayMax = minDelay, maxDelay
		default:
			handled, err := parseSharedStreamOption(key, value, hasValue, token, &cmd.sharedStreamOptions)
			if err != nil || !handled {
				if err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("unknown random option %q", token)
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
		case "balanced", "tools", "artifacts", "terminals":
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
	case "allow-approval":
		opts.AllowApproval = true
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

func (r aiRunner) runLorem(ctx context.Context, w *aistream.Writer, cmd loremCommand) error {
	opts := cmd.Options
	rng := rngForOptions(opts.SeedSet, opts.Seed, r.runtime.now().UnixNano())
	steps := max(opts.Steps, 1)
	text := buildDemoVisibleText(cmd.Chars, rand.New(rand.NewSource(rng.Int63())))
	reasoning := buildLoremText(opts.ReasoningChars, rand.New(rand.NewSource(rng.Int63())))
	for step := range steps {
		if opts.Steps > 0 {
			w.StepStart(fmt.Sprintf("step-%d", step+1))
		}
		emitDecorations(w, opts, cmd.Chars, step, steps)
		if reasoning != "" {
			w.Thinking(sliceByStep(reasoning, steps, step))
		}
		for _, chunk := range chunkText(sliceByStep(text, steps, step), rng, opts.ChunkMin, opts.ChunkMax) {
			w.Text(chunk)
			if err := r.runtime.sleep(ctx, r.sampleDelay(rng, opts.DelayMin, opts.DelayMax)); err != nil {
				return err
			}
		}
		if opts.Steps > 0 {
			w.StepFinish(fmt.Sprintf("step-%d", step+1))
		}
	}
	finishWriter(w, opts)
	return nil
}

func (r aiRunner) runTools(ctx context.Context, w *aistream.Writer, cmd toolsCommand) error {
	opts := cmd.Options
	rng := rngForOptions(opts.SeedSet, opts.Seed, r.runtime.now().UnixNano())
	phaseCount := max(len(cmd.Tools)+1, max(opts.Steps, 1))
	text := buildDemoVisibleText(cmd.Chars, rand.New(rand.NewSource(rng.Int63())))
	reasoning := buildLoremText(opts.ReasoningChars, rand.New(rand.NewSource(rng.Int63())))
	for phase := range phaseCount {
		w.StepStart(fmt.Sprintf("phase-%d", phase+1))
		emitDecorations(w, opts, cmd.Chars, phase, phaseCount)
		if reasoning != "" {
			w.Thinking(sliceByStep(reasoning, phaseCount, phase))
		}
		for _, chunk := range chunkText(sliceByStep(text, phaseCount, phase), rng, opts.ChunkMin, opts.ChunkMax) {
			w.Text(chunk)
		}
		if phase < len(cmd.Tools) {
			if err := r.runToolSpec(ctx, w, cmd.Tools[phase], rng, opts); err != nil {
				if errors.Is(err, errApprovalRequested) {
					w.StepFinish(fmt.Sprintf("phase-%d", phase+1))
				}
				return err
			}
		}
		w.StepFinish(fmt.Sprintf("phase-%d", phase+1))
	}
	finishWriter(w, opts)
	return nil
}

func (r aiRunner) runRandom(ctx context.Context, w *aistream.Writer, cmd randomCommand) error {
	seed := cmd.Seed
	if !cmd.SeedSet {
		seed = r.runtime.now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))
	started := r.runtime.now()
	var deadline time.Time
	if cmd.Duration > 0 {
		deadline = started.Add(cmd.Duration)
	}
	stepOpen := false
	stepName := ""
	actionOptions, actionWeightTotal := buildRandomActionOptions(cmd)
	handleTool := func(spec toolSpec) error {
		if err := r.runToolSpec(ctx, w, spec, rng, defaultCommonOptions()); err != nil {
			if errors.Is(err, errApprovalRequested) && stepOpen {
				w.StepFinish(stepName)
				stepOpen = false
				stepName = ""
			}
			return err
		}
		return nil
	}
	for action := range cmd.Actions {
		if !deadline.IsZero() && !r.runtime.now().Before(deadline) {
			break
		}
		if action > 0 {
			delay := r.sampleDelay(rng, cmd.DelayMin, cmd.DelayMax)
			if !deadline.IsZero() && r.runtime.now().Add(delay).After(deadline) {
				delay = deadline.Sub(r.runtime.now())
			}
			if err := r.runtime.sleep(ctx, delay); err != nil {
				return err
			}
			if !deadline.IsZero() && !r.runtime.now().Before(deadline) {
				break
			}
		}
		switch pickWeighted(actionOptions, actionWeightTotal, rng) {
		case randomActionText:
			for _, chunk := range chunkText(buildDemoVisibleText(40+rng.Intn(160), rand.New(rand.NewSource(rng.Int63()))), rng, defaultChunkMin, defaultChunkMax) {
				w.Text(chunk)
			}
		case randomActionThinking:
			w.Thinking(buildLoremText(30+rng.Intn(120), rand.New(rand.NewSource(rng.Int63()))))
		case randomActionStep:
			if stepOpen {
				w.StepFinish(stepName)
				stepOpen = false
				stepName = ""
			} else {
				stepName = fmt.Sprintf("random-step-%d", action+1)
				w.StepStart(stepName)
				stepOpen = true
			}
		case randomActionTool:
			if err := handleTool(toolSpec{Name: randomToolName(rng), SequenceIndex: action + 1}); err != nil {
				return err
			}
		case randomActionToolFail:
			if err := handleTool(toolSpec{Name: randomToolName(rng), Fail: true, SequenceIndex: action + 1}); err != nil {
				return err
			}
		case randomActionToolDeny:
			if err := handleTool(toolSpec{Name: randomToolName(rng), Deny: true, SequenceIndex: action + 1}); err != nil {
				return err
			}
		case randomActionToolApproval:
			if err := handleTool(toolSpec{Name: randomToolName(rng), Approval: true, SequenceIndex: action + 1}); err != nil {
				return err
			}
		case randomActionSource:
			w.Custom("com.beeper.source", map[string]any{"url": fmt.Sprintf("https://dummybridge.local/random/source/%d", action+1), "title": fmt.Sprintf("Random Source %d", action+1)})
		case randomActionDocument:
			w.Custom("com.beeper.document", map[string]any{"id": fmt.Sprintf("random-doc-%d", action+1), "title": fmt.Sprintf("Random Document %d", action+1), "mediaType": "text/plain"})
		case randomActionFile:
			w.Custom("com.beeper.file", map[string]any{"url": fmt.Sprintf("mxc://dummybridge/random-file-%d", action+1), "mediaType": "application/octet-stream"})
		case randomActionMetadata:
			w.StateDelta(statePatch(map[string]any{"command": "stream-random", "seed": seed, "action": action + 1, "profile": cmd.Profile}))
		case randomActionData:
			w.Custom("com.beeper.data", map[string]any{"name": "random", "value": map[string]any{"action": action + 1, "seed": seed}})
		case randomActionDataTransient:
			w.Custom("com.beeper.data.transient", map[string]any{"name": "random", "value": map[string]any{"action": action + 1, "seed": seed}})
		}
	}
	if stepOpen {
		w.StepFinish(stepName)
	}
	switch chooseRandomTerminal(cmd, rng) {
	case "abort":
		w.Abort("DummyBridge random mode aborted")
	case "error":
		w.Error("DummyBridge random mode failed")
	default:
		w.Finish(agui.FinishReasonStop)
	}
	return nil
}

func (r aiRunner) runToolSpec(ctx context.Context, w *aistream.Writer, spec toolSpec, rng *rand.Rand, opts commonCommandOptions) error {
	toolCallID := fmt.Sprintf("dummy-tool-%d-%s", spec.SequenceIndex, sanitizeToolName(spec.Name))
	input := map[string]any{"tool": spec.Name, "sequence": spec.SequenceIndex, "tags": spec.Tags}
	approvalID := approvalIDForRun(w.Run.RunID, toolCallID)
	var approval *agui.ToolApproval
	if spec.Approval {
		approval = &agui.ToolApproval{ID: approvalID, NeedsApproval: true}
	}
	w.ToolStartWithMetadata(toolCallID, spec.Name, spec.SequenceIndex-1, approval, toolDisplayMetadata(spec.Name))
	annotateProviderRawEvent(w, spec, "tool_call_start")
	if spec.InputError {
		w.ToolArgs(toolCallID, jsonToolInput(input), nil)
		annotateProviderRawEvent(w, spec, "tool_call_args")
		w.ToolError(toolCallID, spec.Name, input, "input-error")
		annotateProviderRawEvent(w, spec, "tool_call_error")
		return nil
	}
	if spec.Delta {
		for _, chunk := range chunkText(fmt.Sprintf("{\"tool\":%q,\"sequence\":%d}", spec.Name, spec.SequenceIndex), rng, opts.ChunkMin, opts.ChunkMax) {
			w.ToolArgs(toolCallID, chunk, nil)
			annotateProviderRawEvent(w, spec, "tool_call_args")
			if err := r.runtime.sleep(ctx, r.sampleDelay(rng, opts.DelayMin, opts.DelayMax)); err != nil {
				return err
			}
		}
	} else {
		encodedInput := jsonToolInput(input)
		w.ToolArgs(toolCallID, encodedInput, encodedInput)
		annotateProviderRawEvent(w, spec, "tool_call_args")
	}
	if spec.Preliminary {
		w.ToolResult(toolCallID, fmt.Sprintf(`{"state":%q,"tool":%q}`, agui.ToolResultStateStreaming, spec.Name), agui.ToolResultStateStreaming)
		annotateProviderRawEvent(w, spec, "tool_call_result")
	}
	switch {
	case spec.Approval:
		if response, ok := r.approvals[approvalID]; ok {
			if response.ID == "" {
				response.ID = approvalID
			}
			w.ToolApprovalResponded(toolCallID, spec.Name, input, response)
			annotateProviderRawEvent(w, spec, "approval_responded")
			if !response.Approved {
				return errApprovalDenied
			}
			return nil
		}
		w.ToolApprovalInputComplete(toolCallID, spec.Name, input)
		annotateProviderRawEvent(w, spec, "tool_call_input_complete")
		w.ToolApprovalRequested(toolCallID, spec.Name, input, *approval)
		annotateProviderRawEvent(w, spec, "approval_requested")
		return errApprovalRequested
	case spec.Deny:
		w.ToolDenied(toolCallID, spec.Name, input, approvalID, "denied")
		annotateProviderRawEvent(w, spec, "tool_call_denied")
	case spec.Fail:
		w.ToolError(toolCallID, spec.Name, input, "DummyBridge synthetic tool failure")
		annotateProviderRawEvent(w, spec, "tool_call_error")
	default:
		w.ToolEnd(toolCallID, spec.Name, input, map[string]any{"status": "ok", "tool": spec.Name, "sequence": spec.SequenceIndex})
		annotateProviderRawEvent(w, spec, "tool_call_end")
	}
	return nil
}

func toolDisplayMetadata(name string) map[string]any {
	displayName := titleToolName(name)
	metadata := map[string]any{
		"displayName": displayName,
	}
	switch strings.ToLower(name) {
	case "calendar.get_events", "google_calendar.get_events", "google-calendar.get-events":
		metadata["displayName"] = "List Calendar Events"
		metadata["iconId"] = "3257-5951"
		metadata["provider"] = map[string]any{
			"id":          "google-calendar",
			"displayName": "Google Calendar",
			"iconId":      "3257-5951",
		}
	case "linear.list_issues", "linear.list-issues", "list_issues", "list-issues":
		metadata["displayName"] = "List Issues"
		metadata["iconId"] = "3257-5945"
		metadata["provider"] = map[string]any{
			"id":          "linear",
			"displayName": "Linear",
			"iconId":      "3257-5945",
		}
	case "shell":
		metadata["displayName"] = "Run Command"
		metadata["iconId"] = "3255-2310"
	case "fetch":
		metadata["displayName"] = "Fetch Web"
		metadata["iconId"] = "source-placeholder"
	}
	return metadata
}

func titleToolName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func approvalIDForRun(runID, toolCallID string) string {
	return "approval-" + runID + "-" + toolCallID
}

func annotateProviderRawEvent(w *aistream.Writer, spec toolSpec, stage string) {
	if !spec.Provider || w == nil || w.Run == nil || len(w.Run.Events) == 0 {
		return
	}
	w.Run.Events[len(w.Run.Events)-1]["rawEvent"] = map[string]any{
		"provider": "dummybridge",
		"stage":    stage,
		"tool":     spec.Name,
		"sequence": spec.SequenceIndex,
		"tags":     spec.Tags,
	}
}

func jsonToolInput(input map[string]any) string {
	raw, err := json.Marshal(input)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func finishWriter(w *aistream.Writer, opts commonCommandOptions) {
	switch {
	case opts.Abort:
		w.Abort("DummyBridge synthetic abort")
	case opts.Error:
		w.Error("DummyBridge synthetic error")
	default:
		w.Finish(opts.FinishReason)
	}
}

func emitDecorations(w *aistream.Writer, opts commonCommandOptions, chars, step, steps int) {
	if opts.Meta {
		seed := opts.Seed
		if !opts.SeedSet {
			seed = int64(chars)
		}
		w.StateDelta(statePatch(map[string]any{"command": "demo", "seed": seed, "step": step + 1}))
	}
	for i := range splitCount(opts.Sources, steps, step) {
		w.Custom("com.beeper.source", map[string]any{"url": fmt.Sprintf("https://dummybridge.local/source/%d-%d", step+1, i+1), "title": fmt.Sprintf("Demo Source %d.%d", step+1, i+1)})
	}
	for i := range splitCount(opts.Documents, steps, step) {
		w.Custom("com.beeper.document", map[string]any{"id": fmt.Sprintf("demo-doc-%d-%d", step+1, i+1), "title": fmt.Sprintf("Demo Document %d.%d", step+1, i+1), "mediaType": "text/plain"})
	}
	for i := range splitCount(opts.Files, steps, step) {
		w.Custom("com.beeper.file", map[string]any{"url": fmt.Sprintf("mxc://dummybridge/demo-file-%d-%d", step+1, i+1), "mediaType": "application/octet-stream"})
	}
	if step == 0 && opts.DataName != "" {
		w.Custom("com.beeper.data", map[string]any{"name": opts.DataName, "value": map[string]any{"mode": "persistent", "stage": step + 1}})
	}
	if step == 0 && opts.DataTransientName != "" {
		w.Custom("com.beeper.data.transient", map[string]any{"name": opts.DataTransientName, "value": map[string]any{"mode": "transient", "stage": step + 1}})
	}
}

func statePatch(values map[string]any) []map[string]any {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	patch := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		patch = append(patch, map[string]any{
			"op":    "add",
			"path":  "/" + key,
			"value": values[key],
		})
	}
	return patch
}

func buildRandomActionOptions(cmd randomCommand) ([]randomActionOption, int) {
	options := []randomActionOption{
		{randomActionText, 6},
		{randomActionThinking, 4},
		{randomActionStep, 2},
		{randomActionTool, 3},
		{randomActionToolFail, 2},
		{randomActionSource, 2},
		{randomActionDocument, 2},
		{randomActionFile, 2},
		{randomActionMetadata, 2},
		{randomActionData, 1},
		{randomActionDataTransient, 1},
	}
	if cmd.AllowApproval {
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
	case "terminals":
		options = append(options, randomActionOption{randomActionStep, 5})
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

func (r aiRunner) sampleDelay(rng *rand.Rand, minDelay, maxDelay time.Duration) time.Duration {
	if maxDelay <= minDelay {
		return minDelay
	}
	return minDelay + time.Duration(rng.Int63n(int64(maxDelay-minDelay)+1))
}

func parseOptionToken(token string) (string, string, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(token), "--")
	key, value, ok := strings.Cut(trimmed, "=")
	return strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value), ok
}

func parseValidatedInt(value string, hasValue bool, token, label string, maxValue int, allowZero bool) (int, error) {
	if !hasValue {
		return 0, fmt.Errorf("%s requires a value", token)
	}
	var n int
	var err error
	if allowZero {
		n, err = parseNonNegativeInt(value, label)
	} else {
		n, err = parsePositiveInt(value, label)
	}
	if err != nil {
		return 0, err
	}
	return n, validateMaxIntValue(n, maxValue, label)
}

func parsePositiveInt(raw, label string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid %s %q", label, raw)
	}
	return n, nil
}

func parseNonNegativeInt(raw, label string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid %s %q", label, raw)
	}
	return n, nil
}

func parseDurationRangeMS(value string, hasValue bool, token string) (time.Duration, time.Duration, error) {
	return parseDurationRange(value, hasValue, token, "delay-ms", maxDemoDelay)
}

func parseDurationRange(value string, hasValue bool, token, label string, maxValue time.Duration) (time.Duration, time.Duration, error) {
	minValue, maxRange, err := parseIntRangeOption(value, hasValue, token, label, int(maxValue/time.Millisecond))
	if err != nil {
		return 0, 0, err
	}
	return time.Duration(minValue) * time.Millisecond, time.Duration(maxRange) * time.Millisecond, nil
}

func parseIntRangeOption(value string, hasValue bool, token, label string, maxValue int) (int, int, error) {
	if !hasValue {
		return 0, 0, fmt.Errorf("%s requires a value", token)
	}
	minValue, maxRange, ok := strings.Cut(value, ":")
	if !ok {
		n, err := parseNonNegativeInt(value, label)
		if err != nil {
			return 0, 0, err
		}
		if err := validateMaxIntValue(n, maxValue, label); err != nil {
			return 0, 0, err
		}
		return n, n, nil
	}
	minInt, err := parseNonNegativeInt(minValue, label)
	if err != nil {
		return 0, 0, err
	}
	maxInt, err := parseNonNegativeInt(maxRange, label)
	if err != nil {
		return 0, 0, err
	}
	if maxInt < minInt {
		return 0, 0, fmt.Errorf("invalid %s range %q", label, value)
	}
	if err := validateMaxIntValue(maxInt, maxValue, label); err != nil {
		return 0, 0, err
	}
	return minInt, maxInt, nil
}

func validateMaxIntValue(value, maxValue int, label string) error {
	if value > maxValue {
		return fmt.Errorf("%s %d exceeds the maximum of %d", label, value, maxValue)
	}
	return nil
}

func rngForOptions(seedSet bool, seed, fallback int64) *rand.Rand {
	if !seedSet {
		seed = fallback
	}
	return rand.New(rand.NewSource(seed))
}

func chunkText(text string, rng *rand.Rand, minChunk, maxChunk int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if minChunk <= 0 {
		minChunk = defaultChunkMin
	}
	if maxChunk < minChunk {
		maxChunk = minChunk
	}
	var chunks []string
	for len(text) > 0 {
		size := minChunk
		if maxChunk > minChunk {
			size += rng.Intn(maxChunk - minChunk + 1)
		}
		if size > len(text) {
			size = len(text)
		}
		parts := aistream.SplitTextUTF8(text, size)
		chunk := parts[0]
		chunks = append(chunks, chunk)
		text = text[len(chunk):]
	}
	return chunks
}

func splitCount(total, parts, index int) int {
	if total <= 0 || parts <= 0 || index < 0 || index >= parts {
		return 0
	}
	base := total / parts
	remainder := total % parts
	if index < remainder {
		return base + 1
	}
	return base
}

func sliceByStep(text string, parts, index int) string {
	if parts <= 1 || text == "" {
		return text
	}
	start := 0
	for i := 0; i < index; i++ {
		start += splitCount(len(text), parts, i)
	}
	length := splitCount(len(text), parts, index)
	if start >= len(text) || length <= 0 {
		return ""
	}
	end := min(start+length, len(text))
	return text[start:end]
}

func sanitizeToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var out strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			out.WriteRune(r)
		}
	}
	if out.Len() == 0 {
		return "tool"
	}
	return out.String()
}
