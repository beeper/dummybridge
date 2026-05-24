package connector

import (
	"context"
	"fmt"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	"github.com/beeper/ai-bridge/pkg/ai-stream"
)

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
	if cmd != nil && cmd.Random != nil && cmd.Random.Runs > 1 {
		return buildAIStreamRunPlans(ctx, runID, threadID, now, *cmd.Random, agentID, agentName)
	}
	run, err := buildAIRunFromCommandWithApprovals(ctx, runID, threadID, now, cmd, agentID, agentName, nil)
	if err != nil {
		return nil, err
	}
	return []aiRunPlan{{Run: run, EffectiveCommand: canonicalCommand(input, cmd)}}, nil
}
