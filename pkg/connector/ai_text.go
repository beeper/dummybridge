package connector

import (
	"fmt"
	"math/rand"
	"strings"
)

var loremSentenceCorpus = []string{
	"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
	"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
	"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.",
	"Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur.",
	"Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.",
	"Integer nec odio praesent libero sed cursus ante dapibus diam.",
	"Nulla quis sem at nibh elementum imperdiet duis sagittis ipsum.",
	"Praesent mauris fusce nec tellus sed augue semper porta.",
	"Mauris massa vestibulum lacinia arcu eget nulla.",
	"Class aptent taciti sociosqu ad litora torquent per conubia nostra.",
	"In consectetur orci eu erat varius, vitae facilisis lorem blandit.",
	"Curabitur ullamcorper ultricies nisi nam eget dui etiam rhoncus.",
}

var demoMarkdownLabels = []string{"release notes", "ops runbook", "incident log", "design memo", "qa checklist", "support brief"}
var demoMarkdownURLs = []string{
	"https://dummybridge.local/docs/streaming",
	"https://dummybridge.local/docs/markdown",
	"https://dummybridge.local/runbooks/runs",
	"https://dummybridge.local/notes/demo-output",
}
var demoMarkdownEmphasis = []string{"high-signal", "operator-visible", "tool-safe", "incremental", "review-ready"}
var demoMarkdownListItems = []string{
	"Confirm the seeded output changes shape between runs.",
	"Surface enough formatting to stress the renderer.",
	"Keep deltas readable while chunks arrive out of phase.",
	"Preserve stable output for deterministic test fixtures.",
	"Expose links, tables, and code blocks without extra flags.",
}
var demoMarkdownQuoteCorpus = []string{
	"Streaming output should feel alive, not like the same paragraph repeated forever.",
	"Richer markdown gives the client something realistic to render while the run is still open.",
}
var demoMarkdownCodeSnippets = []string{
	"const preview = chunks.filter(Boolean).join(\"\");",
	"writer.textDelta(\"| status | value |\\n| --- | --- |\\n\");",
	"if (seeded) { return renderMarkdownBlocks(); }",
}
var demoMarkdownTableHeaders = [][]string{{"Metric", "Value", "Notes"}, {"Phase", "Owner", "Status"}, {"Artifact", "State", "Latency"}}
var demoMarkdownTableRows = [][]string{
	{"stream", "warming", "steady deltas"},
	{"renderer", "active", "accepts markdown"},
	{"tool call", "complete", "output persisted"},
	{"search step", "queued", "awaiting sources"},
	{"summary", "ready", "links attached"},
}

type demoSegmentSpec struct {
	weight int
	minLen int
	build  func(*rand.Rand, int) string
}

func buildLoremText(chars int, rng *rand.Rand) string {
	if chars <= 0 {
		return ""
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(int64(chars)))
	}
	var sb strings.Builder
	sb.Grow(chars + 128)
	lastIndex := -1
	for sb.Len() < chars+64 {
		index := rng.Intn(len(loremSentenceCorpus))
		if len(loremSentenceCorpus) > 1 && index == lastIndex {
			index = (index + 1 + rng.Intn(len(loremSentenceCorpus)-1)) % len(loremSentenceCorpus)
		}
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(loremSentenceCorpus[index])
		lastIndex = index
	}
	return trimText(sb.String(), chars)
}

func buildDemoVisibleText(chars int, rng *rand.Rand) string {
	if chars <= 0 {
		return ""
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(int64(chars)))
	}
	segments := []demoSegmentSpec{
		{weight: 5, minLen: 48, build: func(rng *rand.Rand, remaining int) string {
			return buildLoremText(max(48, min(168, remaining+48)), rand.New(rand.NewSource(rng.Int63())))
		}},
		{weight: 4, minLen: 96, build: func(rng *rand.Rand, _ int) string {
			return fmt.Sprintf("%s Review the [%s](%s) entry for **%s** output and _staged_ formatting transitions.",
				buildLoremText(72+rng.Intn(48), rand.New(rand.NewSource(rng.Int63()))),
				demoMarkdownLabels[rng.Intn(len(demoMarkdownLabels))],
				demoMarkdownURLs[rng.Intn(len(demoMarkdownURLs))],
				demoMarkdownEmphasis[rng.Intn(len(demoMarkdownEmphasis))])
		}},
		{weight: 3, minLen: 96, build: func(rng *rand.Rand, _ int) string {
			var lines []string
			for i := 0; i < 2+rng.Intn(3); i++ {
				prefix := "-"
				if rng.Intn(4) == 0 {
					prefix = "- [x]"
				}
				lines = append(lines, fmt.Sprintf("%s %s", prefix, demoMarkdownListItems[(rng.Intn(len(demoMarkdownListItems))+i)%len(demoMarkdownListItems)]))
			}
			return strings.Join(lines, "\n")
		}},
		{weight: 2, minLen: 72, build: func(rng *rand.Rand, _ int) string {
			return fmt.Sprintf("> %s\n>\n> %s", demoMarkdownQuoteCorpus[rng.Intn(len(demoMarkdownQuoteCorpus))], buildLoremText(48+rng.Intn(36), rand.New(rand.NewSource(rng.Int63()))))
		}},
		{weight: 2, minLen: 72, build: func(rng *rand.Rand, _ int) string {
			return fmt.Sprintf("Use `%s` for incremental patches.\n\n```js\n%s\n```", sanitizeToolName(demoMarkdownLabels[rng.Intn(len(demoMarkdownLabels))]), demoMarkdownCodeSnippets[rng.Intn(len(demoMarkdownCodeSnippets))])
		}},
		{weight: 2, minLen: 180, build: func(rng *rand.Rand, _ int) string {
			header := demoMarkdownTableHeaders[rng.Intn(len(demoMarkdownTableHeaders))]
			lines := []string{fmt.Sprintf("| %s |", strings.Join(header, " | ")), "| --- | --- | --- |"}
			for i := 0; i < 2+rng.Intn(2); i++ {
				lines = append(lines, fmt.Sprintf("| %s |", strings.Join(demoMarkdownTableRows[(rng.Intn(len(demoMarkdownTableRows))+i)%len(demoMarkdownTableRows)], " | ")))
			}
			return strings.Join(lines, "\n")
		}},
	}
	var blocks []string
	total := 0
	for total < chars {
		block := chooseDemoSegment(segments, rng, chars-total)
		blocks = append(blocks, block)
		total += len(block) + 2
	}
	return trimVisibleText(strings.Join(blocks, "\n\n"), chars)
}

func chooseDemoSegment(specs []demoSegmentSpec, rng *rand.Rand, remaining int) string {
	var candidates []demoSegmentSpec
	total := 0
	for _, spec := range specs {
		if remaining > 0 && remaining < spec.minLen/2 {
			continue
		}
		candidates = append(candidates, spec)
		total += spec.weight
	}
	if len(candidates) == 0 {
		candidates = specs
		for _, spec := range candidates {
			total += spec.weight
		}
	}
	target := rng.Intn(total)
	for _, spec := range candidates {
		target -= spec.weight
		if target < 0 {
			return spec.build(rng, remaining)
		}
	}
	return candidates[0].build(rng, remaining)
}

func trimVisibleText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	blocks := strings.Split(text, "\n\n")
	var kept []string
	total := 0
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		next := total + len(block)
		if len(kept) > 0 {
			next += 2
		}
		if next > limit && len(kept) > 0 {
			break
		}
		kept = append(kept, block)
		total = next
	}
	if len(kept) > 0 {
		return strings.Join(kept, "\n\n")
	}
	return trimText(text, limit)
}

func trimText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	minCutoff := max(1, (limit*3)/4)
	for i := min(limit, len(text)); i >= minCutoff; i-- {
		switch text[i-1] {
		case '.', '!', '?':
			return strings.TrimSpace(text[:i])
		}
	}
	for i := min(limit, len(text)); i >= minCutoff; i-- {
		if text[i-1] == ' ' {
			return strings.Trim(strings.TrimSpace(text[:i]), ".,;:")
		}
	}
	return strings.Trim(strings.TrimSpace(text[:limit]), ".,;:")
}
