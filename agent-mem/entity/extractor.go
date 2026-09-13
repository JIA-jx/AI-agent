package entity

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"agentmem/llm"
	"agentmem/observability"
	"agentmem/types"
)

// Extractor 实体关系抽取器，优先 LLM，失败回退正则
type Extractor struct {
	cmpl   llm.Completer
	logger observability.Logger
	re     *regexp.Regexp
}

func NewExtractor(cmpl llm.Completer, logger observability.Logger) *Extractor {
	re, _ := regexp.Compile(`[A-Z][a-z]+(?:[ ][A-Z][a-z]+)*`)
	return &Extractor{cmpl: cmpl, logger: logger, re: re}
}

// Extract 从文本抽取实体关系图
func (e *Extractor) Extract(ctx context.Context, text string) (*types.Graph, error) {
	if e.cmpl != nil {
		system, user := llm.EntityExtractionPrompt(text)
		out, err := e.cmpl.Complete(ctx, system, user)
		if err != nil {
			e.logger.Warnf("llm entity extraction failed: %v", err)
		} else if out != "" {
			if g := parseGraph(out); g != nil {
				return g, nil
			}
		}
	}
	return e.extractByRegex(text), nil
}

func parseGraph(out string) *types.Graph {
	raw := extractJSON(out)
	if raw == "" {
		return nil
	}

	var g types.Graph
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil
	}

	return &g
}

// extractByRegex 正则回退：抽取首字母大写的词组作为候选实体
func (e *Extractor) extractByRegex(text string) *types.Graph {
	g := &types.Graph{}
	if e.re == nil {
		return g
	}

	seen := make(map[string]bool)
	for _, name := range e.re.FindAllString(text, -1) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}

		seen[name] = true
		g.Entities = append(g.Entities, *types.NewEntity(name, "other"))
	}
	return g
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```JSON")
		s = strings.TrimPrefix(s, "```")
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = strings.TrimSpace(s[:idx])
		}
	}
	if start := strings.IndexAny(s, "[{"); start >= 0 {
		if end := strings.LastIndexAny(s, "]}"); end > start {
			return s[start : end+1]
		}
	}
	return ""
}
