package connector

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	"github.com/beeper/ai-bridge/pkg/ai-stream"
)

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

func buildAIRunFromCommandWithApprovals(ctx context.Context, runID, threadID string, now time.Time, cmd *parsedCommand, agentID, agentName string, approvals map[string]aistream.ToolApprovalResponse) (*aistream.Run, error) {
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
		writer.Interrupt()
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
	var delay time.Duration
	for i := range cmd.Runs {
		if i > 0 {
			delay += runner.sampleDelay(rng, cmd.StaggerMin, cmd.StaggerMax)
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
		parsed := &parsedCommand{Name: "stream", Random: &randomCmd}
		run, err := buildAIRunFromCommandWithApprovals(ctx, runID, threadID, now.Add(delay), parsed, agentID, agentName, nil)
		if err != nil {
			return nil, err
		}
		plans = append(plans, aiRunPlan{
			Run:              run,
			Delay:            delay,
			EffectiveCommand: streamSubRunCommand(randomCmd),
		})
	}
	return plans, nil
}

func buildAIStreamRunPlans(ctx context.Context, baseRunID, threadID string, now time.Time, cmd randomCommand, agentID, agentName string) ([]aiRunPlan, error) {
	seed := cmd.Seed
	if !cmd.SeedSet {
		seed = now.UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))
	plans := make([]aiRunPlan, 0, cmd.Runs)
	runner := aiRunner{runtime: virtualAIRuntime(now)}
	var delay time.Duration
	for i := range cmd.Runs {
		if i > 0 {
			delay += runner.sampleDelay(rng, cmd.StaggerMin, cmd.StaggerMax)
		}
		child := cmd
		child.Runs = 1
		child.Seed = seed + int64(i+1)*97
		child.SeedSet = true
		parsed := &parsedCommand{Name: "stream", Random: &child}
		run, err := buildAIRunFromCommandWithApprovals(ctx, fmt.Sprintf("%s-%d", baseRunID, i+1), threadID, now.Add(delay), parsed, agentID, agentName, nil)
		if err != nil {
			return nil, err
		}
		plans = append(plans, aiRunPlan{
			Run:              run,
			Delay:            delay,
			EffectiveCommand: streamSubRunCommand(child),
		})
	}
	return plans, nil
}

func streamSubRunCommand(cmd randomCommand) string {
	parts := []string{
		"stream",
		strconv.Itoa(int(cmd.Duration / time.Second)),
		"--actions=" + strconv.Itoa(cmd.Actions),
		"--delay-ms=" + strconv.Itoa(int(cmd.DelayMin/time.Millisecond)) + ":" + strconv.Itoa(int(cmd.DelayMax/time.Millisecond)),
		"--profile=" + cmd.Profile,
		"--seed=" + strconv.FormatInt(cmd.Seed, 10),
	}
	if cmd.Chars > 0 {
		parts = append(parts, "--chars="+strconv.Itoa(cmd.Chars))
	}
	if cmd.Terminal != "" {
		parts = append(parts, "--terminal="+cmd.Terminal)
	}
	if !cmd.AllowApproval {
		parts = append(parts, "--no-approval")
	}
	if cmd.AllowAbort {
		parts = append(parts, "--allow-abort")
	}
	if cmd.AllowError {
		parts = append(parts, "--allow-error")
	}
	return strings.Join(parts, " ")
}
