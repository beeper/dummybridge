package connector

import (
	"context"
	"errors"
	"time"

	"github.com/beeper/ai-bridge/pkg/ai-stream"
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
	Duration   time.Duration
	Actions    int
	Chars      int
	DelayMin   time.Duration
	DelayMax   time.Duration
	Terminal   string
	Runs       int
	StaggerMin time.Duration
	StaggerMax time.Duration
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
	approvals map[string]aistream.ToolApprovalResponse
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
