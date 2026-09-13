package kbs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"github.com/elastic/go-elasticsearch/v8"
	"thunder/logs"
)

type ESBM25Retriever struct {
	client *elasticsearch.Client
	index  string
	topK   int
}

func NewESBM25Retriever(client *elasticsearch.Client, index string, topK int) *ESBM25Retriever {
	if topK <= 0 {
		topK = 20
	}
	return &ESBM25Retriever{client: client, index: index, topK: topK}
}

type bm25Hit struct {
	ID       string                 `json:"_id"`     // 文档ID
	Score    float64                `json:"_score"`  // 相关性分数
	Source   map[string]interface{} `json:"_source"` // 文档源数据
	Metadata map[string]interface{} `json:"metadata"`
}

type bm25Response struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []bm25Hit `json:"hits"`
	} `json:"hits"`
}

func (r *ESBM25Retriever) Search(ctx context.Context, query string, topK int, filters SearchFilter) ([]*schema.Document, error) {
	if r.client == nil {
		return nil, fmt.Errorf("es client is nil")
	}
	if r.topK == 0 {
		r.topK = topK
	}

	boolQuery := map[string]interface{}{
		"bool": map[string]interface{}{
			"should": []interface{}{ // should 子句，至少匹配一个
				map[string]interface{}{
					"multi_match": map[string]interface{}{ // 多字段匹配
						"query":  query,
						"fields": []string{"content^3", "parent_id", "doc_id"}, // content 权重3倍
						"type":   "best_fields",                                // 取最佳匹配字段的分数
					},
				},
				map[string]interface{}{
					"match_phrase": map[string]interface{}{ // 短语匹配
						"content": map[string]interface{}{
							"query": query,
							"boost": 2.0, // 提升权重
							"slop":  3,   // 词间距允许3个词
						},
					},
				},
			},
			"minimum_should_match": 1,
		},
	}

	if len(filters) > 0 {
		var mustTerms []interface{}
		for k, v := range filters {
			mustTerms = append(mustTerms, map[string]interface{}{
				"term": map[string]interface{}{
					fmt.Sprintf("metadata.%s.keyword", k): v, // 使用 .keyword 精确匹配
				},
			})
		}
		boolQuery["bool"].(map[string]interface{})["filter"] = mustTerms // filter 不参与评分
	}

	searchBody := map[string]interface{}{
		"query":            boolQuery,                                                    // 查询条件
		"size":             r.topK,                                                       // 返回数量
		"_source":          []string{"id", "content", "metadata", "parent_id", "doc_id"}, // 返回字段
		"track_total_hits": false,                                                        // 不跟踪总数，提高性能
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(searchBody); err != nil {
		return nil, fmt.Errorf("encode es query: %w", err)
	}

	res, err := r.client.Search(
		r.client.Search.WithContext(ctx),
		r.client.Search.WithIndex(r.index),
		r.client.Search.WithBody(&buf),
	)
	if err != nil {
		return nil, fmt.Errorf("es search: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		logs.Warnf("es search error status: %s", res.Status())
		return nil, nil
	}

	var resp bm25Response
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode es response: %w", err)
	}

	docs := make([]*schema.Document, 0, len(resp.Hits.Hits))
	for _, hit := range resp.Hits.Hits {
		content, _ := hit.Source["content"].(string)
		if content == "" {
			continue
		}

		meta := hit.Metadata
		if meta == nil {
			meta = make(map[string]interface{})
		}
		if pid, ok := hit.Source["parent_id"]; ok {
			meta["parent_id"] = pid
		}
		if did, ok := hit.Source["doc_id"]; ok {
			meta["doc_id"] = did
		}

		doc := &schema.Document{
			ID:       hit.ID,
			Content:  content,
			MetaData: meta,
		}
		doc.WithScore(hit.Score)
		docs = append(docs, doc)
	}
	return docs, nil
}

func (r *ESBM25Retriever) Index(ctx context.Context, docs []*schema.Document) error {
	if r.client == nil {
		return fmt.Errorf("es client is nil")
	}

	for _, doc := range docs {
		body := map[string]interface{}{
			"content":  doc.Content,
			"metadata": doc.MetaData,
		}
		// 从 metadata 提取常用字段到顶层，方便 filter
		for _, key := range []string{"doc_id", "parent_id", "kb_id"} {
			if v, ok := doc.MetaData[key]; ok {
				body[key] = v
			}
		}

		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			continue
		}

		_, err := r.client.Index(r.index, &buf, r.client.Index.WithContext(ctx), r.client.Index.WithDocumentID(doc.ID))
		if err != nil {
			logs.Warnf("es index doc %s failed: %v", doc.ID, err)
		}
	}
	return nil
}

func (r *ESBM25Retriever) EnsureIndex(ctx context.Context) error {
	exists, err := r.client.Indices.Exists([]string{r.index})
	if err != nil {
		return err
	}
	if exists.StatusCode == 200 {
		return nil
	}

	// mapping: content 字段用 ik_smart 中文分词
	mapping := map[string]interface{}{
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":            "text",
					"analyzer":        "ik_smart",
					"search_analyzer": "ik_smart",
					"fields": map[string]interface{}{
						"keyword": map[string]interface{}{
							"type":         "keyword",
							"ignore_above": 256,
						},
					},
				},
				"metadata": map[string]interface{}{
					"type": "object",
				},
				"parent_id": map[string]interface{}{
					"type": "keyword",
				},
				"doc_id": map[string]interface{}{
					"type": "keyword",
				},
				"kb_id": map[string]interface{}{
					"type": "keyword",
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(mapping); err != nil {
		return err
	}

	_, err = r.client.Indices.Create(r.index, r.client.Indices.Create.WithBody(&buf))
	if err != nil {
		logs.Warnf("es create index failed (ik_smart may not be installed), falling back: %v", err)
		// fallback: 不带中文分词
		simpleMapping := map[string]interface{}{
			"mappings": map[string]interface{}{
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type": "text",
						"fields": map[string]interface{}{
							"keyword": map[string]interface{}{
								"type":         "keyword",
								"ignore_above": 256,
							},
						},
					},
					"metadata":  map[string]interface{}{"type": "object"},
					"parent_id": map[string]interface{}{"type": "keyword"},
					"doc_id":    map[string]interface{}{"type": "keyword"},
					"kb_id":     map[string]interface{}{"type": "keyword"},
				},
			},
		}
		buf.Reset()
		_ = json.NewEncoder(&buf).Encode(simpleMapping)
		_, err = r.client.Indices.Create(r.index, r.client.Indices.Create.WithBody(&buf))
		return err
	}
	return nil
}
