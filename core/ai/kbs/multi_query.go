package kbs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"basemodel/logs"

	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type QueryExpander interface {
	Expand(ctx context.Context, query string) ([]string, error)
	Available() bool
}

const mqeSystemPrompt = `你是一个知识库检索优化助手。你的任务是将用户的原始问题改写为多个不同视角的子查询，以便从知识库中更全面地检索相关内容。

规则：
1. 保持每个子查询的独立性，从不同角度表达同一个信息需求
2. 可以包含：同义词替换、实体展开、上下位概念、细节补充、反向查询等
3. 数量控制在 3-5 个，返回 JSON 数组格式
4. 保留关键实体名、技术术语、专有名词原样不变
5. 直接返回 JSON，不要任何解释文字

示例输出（用户问："Redis 缓存穿透怎么解决"）：
["Redis 缓存穿透的解决方案有哪些", "布隆过滤器如何防止缓存穿透", "缓存空值是否能解决缓存穿透", "什么是缓存穿透现象及其处理方式"]`

type LLMQueryExpander struct {
	model    einoModel.BaseChatModel
	maxQuery int
}

func NewLLMQueryExpander(model einoModel.BaseChatModel, maxQuery int) *LLMQueryExpander {
	if maxQuery <= 0 {
		maxQuery = 4
	}
	return &LLMQueryExpander{model: model, maxQuery: maxQuery}
}

func (e *LLMQueryExpander) Available() bool { return e.model != nil }

func (e *LLMQueryExpander) Expand(ctx context.Context, query string) ([]string, error) {
	if !e.Available() || query == "" {
		return nil, fmt.Errorf("LLM expander not available or empty query")
	}

	userPrompt := fmt.Sprintf("请为以下问题生成 %d 个子查询，返回 JSON 数组：\n\n%s", e.maxQuery, query)

	resp, err := e.model.Generate(ctx, []*schema.Message{
		{Role: schema.System, Content: mqeSystemPrompt},
		{Role: schema.User, Content: userPrompt},
	})
	if err != nil {
		logs.Warnf("MQE: LLM generate failed, fallback to original query only: %v", err)
		return nil, err
	}

	content := strings.TrimSpace(resp.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var subQueries []string
	if err := json.Unmarshal([]byte(content), &subQueries); err != nil {
		logs.Warnf("MQE: JSON parse failed, trying regex fallback: %v", err)
		subQueries = extractQuotedStrings(content)
	}

	// 去重 + 过滤空串 + 截断
	seen := make(map[string]bool)
	var result []string
	seen[query] = true
	result = append(result, query)

	for _, q := range subQueries {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if seen[q] {
			continue
		}
		seen[q] = true
		result = append(result, q)

		if len(result) >= e.maxQuery+1 {
			// 原 query + maxQuery 个子查询
			break
		}
	}

	if len(result) <= 1 {
		logs.Infof("MQE: expansion returned no valid sub-queries, using original only")
		return []string{query}, nil
	}

	logs.Infof("MQE: expanded query into %d sub-queries: %v", len(result), result)
	return result, nil
}

func extractQuotedStrings(s string) []string {
	var result []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			if inQuote {
				if current.Len() > 0 {
					result = append(result, current.String())
					current.Reset()
				}
				inQuote = false
			} else {
				inQuote = true
			}
		} else if inQuote {
			current.WriteByte(c)
		}
	}
	return result
}

type MQERetriever struct {
	expander QueryExpander // 查询扩展器
	inner    interface {   // 内部检索器（支持泛型接口）
		Search(ctx context.Context, query string, filters SearchFilter) ([]*schema.Document, error)
	}
	cfg HybridConfig // 混合检索配置
}

func NewMQERetriever(expander QueryExpander, inner *HybridRetriever, cfg HybridConfig) *MQERetriever {
	return &MQERetriever{expander: expander, inner: inner, cfg: cfg}
}

type docEntry struct {
	doc *schema.Document // 文档对象
	rrf float64          // RRF 分数（用于排序）
}

func (m *MQERetriever) Search(ctx context.Context, query string, filters SearchFilter) ([]*schema.Document, error) {
	var subQueries []string
	if m.expander != nil && m.expander.Available() {
		expanded, err := m.expander.Expand(ctx, query)
		if err != nil || len(expanded) == 0 {
			logs.Warnf("MQE: expand failed or empty, fallback to original query: %v", err)
			subQueries = []string{query}
		} else {
			subQueries = expanded
		}
	} else {
		subQueries = []string{query}
	}

	globalMap := make(map[string]*docEntry)
	for _, sq := range subQueries {
		docs, err := m.inner.Search(ctx, sq, filters)
		if err != nil {
			logs.Warnf("MQE: sub-query search failed for %q: %v", sq, err)
			continue
		}
		// 计算 RRF 分数
		for rank, doc := range docs {
			idx := float64(rank + 1)
			rrfScore := 1.0 / (m.cfg.RRFConstant + idx)
			if existing, ok := globalMap[doc.ID]; ok {
				existing.rrf += rrfScore
			} else {
				globalMap[doc.ID] = &docEntry{doc: doc, rrf: rrfScore}
			}
		}
	}
	if len(globalMap) == 0 {
		return nil, fmt.Errorf("MQE: all sub-queries returned empty results")
	}

	var merged []*docEntry
	for _, e := range globalMap {
		merged = append(merged, e)
	}
	sortMQEDocs(merged)

	finalTopK := m.cfg.FinalTopK
	if finalTopK <= 0 || finalTopK > len(merged) {
		finalTopK = len(merged)
	}

	out := make([]*schema.Document, 0, finalTopK)
	for i := 0; i < finalTopK; i++ {
		d := merged[i].doc
		d.WithScore(merged[i].rrf)
		out = append(out, d)
	}

	logs.Infof("MQE: merged %d docs from %d sub-queries, output top %d", len(globalMap), len(subQueries), finalTopK)
	return out, nil
}

func sortMQEDocs(entries []*docEntry) {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].rrf > entries[i].rrf {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}
