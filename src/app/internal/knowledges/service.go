package knowledges

import (
	"app/shared"
	"bufio"
	"bytes"
	"common/biz"
	"common/utils"
	"context"
	"core/ai/kbs"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"model"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"thunder/ai/einos"
	"thunder/config"
	"thunder/database"
	"thunder/errs"
	"thunder/event"
	"thunder/logs"

	"github.com/PuerkitoBio/goquery"
	"github.com/cloudwego/eino-ext/components/document/loader/file"
	"github.com/cloudwego/eino-ext/components/document/parser/docx"
	"github.com/cloudwego/eino-ext/components/document/parser/html"
	"github.com/cloudwego/eino-ext/components/document/parser/pdf"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/components/embedding"
	aiModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/google/uuid"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	html2 "golang.org/x/net/html"
	"gorm.io/gorm"
)

type service struct {
	repo         repository
	esClient     *elasticsearch.Client
	milvusClient client.Client
}

func (s *service) createKnowledgeBase(ctx context.Context, userId uuid.UUID, req createKnowledgeBaseReq) (any, error) {
	kb := model.KnowledgeBase{
		BaseModel: model.BaseModel{
			ID: uuid.New(),
		},
		CreatorID:              userId,
		Name:                   req.Name,
		Description:            req.Description,
		ChatModelName:          req.ChatModelName,
		ChatModelProvider:      req.ChatModelProvider,
		EmbeddingModelName:     req.EmbeddingModelName,
		EmbeddingModelProvider: req.EmbeddingModelProvider,
		StorageType:            model.StorageTypeElasticSearch,
		StorageConfig:          model.JSON{},
		DocumentCount:          0,
		Tags:                   req.Tags,
	}
	err := s.repo.createKnowledgeBase(ctx, &kb)
	if err != nil {
		logs.Errorf("create knowledge base error: %v", err)
		return nil, errs.DBError
	}
	return &kb, nil
}

func (s *service) listKnowledgeBases(ctx context.Context, userId uuid.UUID, params listReq) (*ListResp, error) {
	page := params.Page
	if page <= 0 {
		page = 1
	}
	size := params.PageSize
	if size <= 0 {
		size = 10
	}
	filter := KnowledgeBaseFilter{
		Search: params.Search,
		Limit:  size,
		Offset: (page - 1) * size,
	}
	kbss, total, err := s.repo.listKnowledgeBases(ctx, userId, filter)
	if err != nil {
		logs.Errorf("list knowledge base error: %v", err)
		return nil, errs.DBError
	}
	return &ListResp{
		KnowledgeBases: kbss,
		Total:          total,
	}, nil
}

func (s *service) getKnowledgeBase(ctx context.Context, userId uuid.UUID, id uuid.UUID) (*KnowledgeBaseResponse, error) {
	kb, err := s.repo.getKnowledgeBase(ctx, userId, id)
	if err != nil {
		logs.Errorf("get knowledge base error: %v", err)
		return nil, errs.DBError
	}
	// 统计文档数和总字节数
	totalSize, docCount, err := s.repo.countKnowledgeBaseDocuments(ctx, kb.ID)
	if err != nil {
		logs.Errorf("count knowledge base documents error: %v", err)
		return nil, errs.DBError
	}
	return &KnowledgeBaseResponse{
		Id:                     kb.ID,
		Name:                   kb.Name,
		Description:            kb.Description,
		EmbeddingModelName:     kb.EmbeddingModelName,
		EmbeddingModelProvider: kb.EmbeddingModelProvider,
		ChatModelName:          kb.ChatModelName,
		ChatModelProvider:      kb.ChatModelProvider,
		StorageType:            kb.StorageType,
		StorageConfig:          kb.StorageConfig,
		Tags:                   kb.Tags,
		TotalSize:              totalSize,
		DocumentCount:          int(docCount),
		CreatorId:              kb.CreatorID,
		CreatedAt:              kb.CreatedAt.Unix(),
		UpdatedAt:              kb.UpdatedAt.Unix(),
	}, nil
}

func (s *service) updateKnowledgeBase(ctx context.Context, userId uuid.UUID, id uuid.UUID, req updateKnowledgeBaseReq) (any, error) {
	kb, err := s.repo.getKnowledgeBase(ctx, userId, id)
	if err != nil {
		logs.Errorf("get knowledge base error: %v", err)
		return nil, errs.DBError
	}
	if kb == nil {
		return nil, biz.ErrKnowledgeBaseNotFound
	}
	if req.Name != "" {
		kb.Name = req.Name
	}
	if req.Description != "" {
		kb.Description = req.Description
	}
	if req.EmbeddingModelName != "" {
		kb.EmbeddingModelName = req.EmbeddingModelName
	}
	if req.EmbeddingModelProvider != "" {
		kb.EmbeddingModelProvider = req.EmbeddingModelProvider
	}
	if req.ChatModelName != "" {
		kb.ChatModelName = req.ChatModelName
	}
	if req.ChatModelProvider != "" {
		kb.ChatModelProvider = req.ChatModelProvider
	}
	err = s.repo.updateKnowledgeBase(ctx, kb)
	if err != nil {
		logs.Errorf("update knowledge base error: %v", err)
		return nil, errs.DBError
	}
	return kb, nil
}

func (s *service) deleteKnowledgeBase(ctx context.Context, userId uuid.UUID, id uuid.UUID) error {
	kb, err := s.repo.getKnowledgeBase(ctx, userId, id)
	if err != nil {
		logs.Errorf("get knowledge base error: %v", err)
		return errs.DBError
	}
	if kb == nil {
		return biz.ErrKnowledgeBaseNotFound
	}
	err = s.repo.deleteKnowledgeBase(ctx, kb.ID)
	if err != nil {
		logs.Errorf("delete knowledge base error: %v", err)
		return errs.DBError
	}
	return nil
}

func (s *service) listDocuments(ctx context.Context, userId uuid.UUID, kbId uuid.UUID, params listDocumentReq) (*ListDocumentsResp, error) {
	page := params.Page
	if page <= 0 {
		page = 1
	}
	size := params.PageSize
	if size <= 0 {
		size = 10
	}

	filter := DocumentFilter{
		Status: params.Status,
		Search: params.Search,
		Limit:  size,
		Offset: (page - 1) * size,
	}
	documents, total, err := s.repo.listDocuments(ctx, userId, kbId, filter)
	if err != nil {
		logs.Errorf("list documents error: %v", err)
		return nil, errs.DBError
	}
	return &ListDocumentsResp{
		Documents: documents,
		Total:     total,
	}, nil
}

func (s *service) uploadDocuments(ctx context.Context, userId uuid.UUID, kbId uuid.UUID, uploadFile *multipart.FileHeader) (any, error) {
	kb, err := s.repo.getKnowledgeBase(ctx, userId, kbId)
	if err != nil {
		logs.Errorf("get knowledge base error: %v", err)
		return nil, errs.DBError
	}
	if kb == nil {
		return nil, biz.ErrKnowledgeBaseNotFound
	}

	ext := strings.ToLower(filepath.Ext(uploadFile.Filename))
	fileType := kbs.FromExtension(ext)
	var selectParser parser.Parser
	switch fileType {
	case kbs.Markdown:
		selectParser = parser.TextParser{}
	case kbs.Docx:
		selectParser, err = kbs.DocxParser(&docx.Config{
			ToSections:     true,
			IncludeTables:  true,
			IncludeFooters: true,
			IncludeHeaders: true,
		})
		if err != nil {
			logs.Errorf("new docx parser error: %v", err)
			return nil, biz.FileLoadError
		}
	case kbs.PDF:
		selectParser, err = kbs.PDFParser(&pdf.Config{
			ToPages: false,
		})
		if err != nil {
			logs.Errorf("new pdf parser error: %v", err)
			return nil, biz.FileLoadError
		}
	case kbs.Html:
		selectParser, err = kbs.HtmlParser(&html.Config{
			Selector: &html.BodySelector,
		})
		if err != nil {
			logs.Errorf("new html parser error: %v", err)
			return nil, biz.FileLoadError
		}
	default:
		selectParser = parser.TextParser{}
	}

	loader, err := file.NewFileLoader(ctx, &file.FileLoaderConfig{
		Parser: selectParser,
	})
	if err != nil {
		logs.Errorf("new file loader error: %v", err)
		return nil, biz.FileLoadError
	}

	src, err := uploadFile.Open()
	if err != nil {
		logs.Errorf("open file error: %v", err)
		return nil, biz.FileLoadError
	}
	defer src.Close()

	tempFile, err := s.createTempFileFromUploadFile(src, uploadFile.Filename)
	if err != nil {
		logs.Errorf("create temp file error: %v", err)
		return nil, biz.FileLoadError
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())

	docs, err := loader.Load(ctx, document.Source{
		URI: tempFile.Name(),
	})
	if err != nil {
		logs.Errorf("load file error: %v", err)
		return nil, biz.FileLoadError
	}
	doc := &model.Document{
		KnowledgeBaseID: kb.ID,
		CreatorID:       userId,
		Name:            uploadFile.Filename,
		FileType:        ext,
		Size:            uploadFile.Size,
		StorageKey:      uploadFile.Filename,
		FileHash:        "",
		Status:          model.DocumentStatusPending,
		ErrorMessage:    "",
	}
	err = s.repo.createDocument(ctx, doc)
	if err != nil {
		logs.Errorf("create document error: %v", err)
		return nil, errs.DBError
	}

	// 切分+向量化+索引
	go func() {
		ctx = context.Background()
		err = s.repo.updateDocumentStatus(ctx, doc.ID, model.DocumentStatusProcessing)
		if err != nil {
			logs.Errorf("update document status error: %v", err)
			return
		}
		err = s.processDocumentAndVectorAndStore(ctx, doc, docs, kb)
		if err != nil {
			logs.Errorf("process file error: %v", err)

			err = s.repo.updateDocumentStatus(ctx, doc.ID, model.DocumentStatusFailed)
			if err != nil {
				logs.Errorf("update document status error: %v", err)
				return
			}
			return
		}

		err = s.repo.updateDocumentStatus(ctx, doc.ID, model.DocumentStatusCompleted)
		if err != nil {
			logs.Errorf("update document status error: %v", err)
			return
		}
	}()
	return doc, nil
}

const (
	maxChildSize     = 500 // 子块最大长度
	childOverlapSize = 150 // 子块重叠长度
)

func (s *service) processDocumentAndVectorAndStore(ctx context.Context, doc *model.Document, docs []*schema.Document, kb *model.KnowledgeBase) error {
	var content string
	if len(docs) > 0 && docs[0] != nil {
		content = docs[0].Content
	}
	if content == "" {
		logs.Warnf("document content is empty")
		return nil
	}
	var parentDocs []*model.DocumentChunk
	var childSchemaDocs []*schema.Document
	fileType := kbs.FromExtension(doc.FileType)

	if fileType == kbs.Markdown {
		// h1认为是文档名称 h2认为是章节 h3认为是小节; chunk表存储h2的内容
		parentDocs, childSchemaDocs = s.processMarkdown(content, doc, parentDocs, kb, childSchemaDocs)
	} else if fileType == kbs.Docx {
		parentDocs, childSchemaDocs = s.processDocx(docs, doc, parentDocs, kb, childSchemaDocs)
	} else if fileType == kbs.PDF {
		parentDocs, childSchemaDocs = s.processPDF(docs, doc, parentDocs, kb, childSchemaDocs)
	} else if fileType == kbs.Html {
		parentDocs, childSchemaDocs = s.processHtml(docs, doc, parentDocs, kb, childSchemaDocs)
	} else if fileType == kbs.Epub {
		parentDocs, childSchemaDocs = s.processEpub(docs, doc, parentDocs, kb, childSchemaDocs)
	} else {
		parentTexts := utils.SplitByWindow(content, 1000, 200)
		for i, pText := range parentTexts {
			parentDocs = append(parentDocs, &model.DocumentChunk{
				BaseModel:       model.BaseModel{ID: uuid.New()},
				DocumentID:      doc.ID,
				KnowledgeBaseID: kb.ID,
				Content:         pText,
				ChunkIndex:      i,
				MetaInfo: map[string]interface{}{
					"source":    doc.Name,
					"file_type": doc.FileType,
					"type":      "generic",
				},
				TokenCount: utils.GetTokenCount(pText),
				Status:     model.ChunkStatusEmbedded,
			})
			pathPrefix := fmt.Sprintf("【文档:%s】【片段:%d】\n", doc.Name, i+1)
			childTexts := utils.SplitByWindow(content, 400, 80)
			for j, cText := range childTexts {
				childSchemaDocs = append(childSchemaDocs, s.buildChildSchemaDoc(parentDocs[i].ID, doc, kb, pathPrefix+cText, i, j, 0, nil))
			}
		}
	}
	return s.saveToStores(ctx, kb, parentDocs, childSchemaDocs)
}

func (s *service) processMarkdown(content string, doc *model.Document, parentModels []*model.DocumentChunk, kb *model.KnowledgeBase, childSchemaDocs []*schema.Document) ([]*model.DocumentChunk, []*schema.Document) {
	h1Title := utils.ExtractTitle(content, "#")
	if h1Title == "" {
		h1Title = doc.Name
	}

	// 获取 h2 的内容
	h2Block := utils.SplitByHeading(content, "##")
	for i, h2 := range h2Block {
		parentId := uuid.New()
		h2Title := utils.ExtractTitle(h2, "##")
		if h2Title == "" {
			h2Title = "概览"
		}
		// h2的内容是parent
		parentModels = append(parentModels, &model.DocumentChunk{
			BaseModel:       model.BaseModel{ID: parentId},
			DocumentID:      doc.ID,
			KnowledgeBaseID: kb.ID,
			Content:         h2,
			ChunkIndex:      i,
			MetaInfo: map[string]interface{}{
				"h1": h1Title,
				"h2": h2Title,
			},
			TokenCount: utils.GetTokenCount(h2),
			Status:     model.ChunkStatusEmbedded,
		})
		// 获取h3的内容 作为child
		h3Block := utils.SplitByHeading(h2, "###")
		for j, h3 := range h3Block {
			h3Title := utils.ExtractTitle(h3, "###")
			// 给child的内容添加一个前缀 表明所属的上级
			pathPrefix := fmt.Sprintf("【文档:%s】 > 【主题:%s】", h1Title, h2Title)
			if h3Title != "" {
				h3Title += " > 【子题: " + h3Title + "】"
			}
			pathPrefix += "\n"
			// 设定一个长度，做一次切分，防止子内容过长
			subTexts := utils.SplitTextByLength(h3, maxChildSize-len(pathPrefix), childOverlapSize)
			for k, text := range subTexts {
				childSchemaDocs = append(childSchemaDocs, s.buildChildSchemaDoc(parentId, doc, kb, pathPrefix+text, i, j, k, nil))
			}
		}
	}
	return parentModels, childSchemaDocs
}

func (s *service) createTempFileFromUploadFile(src multipart.File, fileName string) (*os.File, error) {
	tempFile, err := os.CreateTemp("", "upload-"+fileName+".tmp")
	if err != nil {
		return nil, err
	}
	defer tempFile.Close()

	_, err = io.Copy(tempFile, src)
	if err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return nil, err
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return nil, err
	}
	return tempFile, nil
}

func (s *service) getEmbeddingConfig(provider string, embeddingModelName string, creatorID uuid.UUID) (embedding.Embedder, error) {
	trigger, err := event.Trigger("getEmbeddingConfig", &shared.LLMParams{
		Provider:  provider,
		Model:     embeddingModelName,
		UserId:    creatorID,
		ModelType: model.LLMTypeEmbedding,
	})
	if err != nil {
		return nil, err
	}
	response := trigger.(*shared.EmbeddingConfigResponse)
	embedder, err := einos.LoadEmbedding(context.Background(),
		response.Model.ProviderConfig.Provider,
		response.Model.ToEmbeddingConfig())
	return embedder, err
}

func (s *service) deleteDocuments(ctx context.Context, userId uuid.UUID, kbId uuid.UUID, documentId uuid.UUID) error {
	knowledgeBase, err := s.repo.getKnowledgeBase(ctx, userId, kbId)
	if err != nil {
		logs.Errorf("get knowledge base error: %v", err)
		return errs.DBError
	}
	if knowledgeBase == nil {
		return biz.ErrKnowledgeBaseNotFound
	}

	doc, err := s.repo.getDocument(ctx, userId, kbId, documentId)
	if err != nil {
		logs.Errorf("get document error: %v", err)
		return errs.DBError
	}
	if doc == nil {
		return biz.ErrDocumentNotFound
	}

	err = s.repo.transaction(ctx, func(tx *gorm.DB) error {
		err = s.repo.deleteDocuments(ctx, tx, userId, kbId, documentId)
		if err != nil {
			logs.Errorf("delete documents error: %v", err)
			return err
		}
		err = s.repo.deleteDocumentChunks(ctx, tx, kbId, documentId)
		if err != nil {
			logs.Errorf("delete document chunks error: %v", err)
			return err
		}
		if knowledgeBase.StorageType == model.StorageTypeElasticSearch {
			err = s.deleteEsIndex(ctx, kbId, documentId)
			if err != nil {
				logs.Errorf("delete es index error: %v", err)
				return err
			}
		}
		if knowledgeBase.StorageType == model.StorageTypeMilvus {
			err = s.deleteMilvusIndex(ctx, kbId, documentId)
		}
		return nil
	})
	if err != nil {
		logs.Errorf("delete documents error: %v", err)
		return errs.DBError
	}
	return nil
}

func (s *service) deleteEsIndex(ctx context.Context, kbId uuid.UUID, documentId uuid.UUID) error {
	index := s.buildIndex(kbId)
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"term": map[string]interface{}{
				"doc_id.keyword": documentId.String(),
			},
		},
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	err := encoder.Encode(query)
	if err != nil {
		logs.Errorf("encode query error: %v", err)
		return err
	}
	res, err := s.esClient.DeleteByQuery(
		[]string{index},
		&buf,
		s.esClient.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		logs.Errorf("delete by query error: %v", err)
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		logs.Errorf("delete by query error: %v", res)
		return err
	}
	return nil
}

func (s *service) buildIndex(kbId uuid.UUID) string {
	sprintf := fmt.Sprintf("kb_%s", kbId.String())
	sprintf = strings.ReplaceAll(sprintf, "-", "_")
	return sprintf
}

const (
	maxSearchResult    = 5
	hybridVectorTopK   = 20    // Stage 1: 向量召回数
	hybridBM25TopK     = 20    // Stage 1: BM25 召回数
	hybridRRFConstant  = 60.0  // RRF 常数 k
	hybridFinalTopK    = 5     // Stage 2: rerank 后的最终输出
	hybridEnableRerank = false // 默认不启用 rerank

	mqeEnableMQE   = true // Multi-Query Expansion 开关
	mqeMaxSubQuery = 4    // 最多生成几个子查询）
)

func (s *service) searchKnowledgeBase(ctx context.Context, userId uuid.UUID, kbId uuid.UUID, params searchParams) (*SearchResponse, error) {
	startTime := time.Now()
	index := s.buildIndex(kbId)

	knowledgeBase, err := s.repo.getKnowledgeBase(ctx, userId, kbId)
	if err != nil {
		logs.Errorf("get knowledge base error: %v", err)
		return nil, errs.DBError
	}
	if knowledgeBase == nil {
		return nil, biz.ErrKnowledgeBaseNotFound
	}
	if params.Query == "" {
		return &SearchResponse{
			KbId:    kbId,
			Query:   params.Query,
			Results: []*SearchResult{},
			Took:    time.Since(startTime).Microseconds(),
			Total:   0,
		}, nil
	}

	// LLM 做意图识别：提取 keywords + 卷号/章节号过滤条件
	intent, _ := s.parseQueryIntent(ctx, knowledgeBase, params.Query)

	embedder, err := s.getEmbeddingConfig(knowledgeBase.EmbeddingModelProvider, knowledgeBase.EmbeddingModelName, userId)
	if err != nil {
		logs.Errorf("get embedding config error: %v", err)
		return nil, biz.ErrEmbeddingConfigNotFound
	}

	store, err := kbs.NewMilvusVectorStore(ctx, s.milvusClient, index, embedder)
	if err != nil {
		logs.Errorf("new vector store error: %v", err)
		return nil, err
	}

	filter := make(kbs.SearchFilter)
	if intent.ChapterNum > 0 {
		filter["chapter_num"] = intent.ChapterNum
	}
	if intent.VolumeNum > 0 {
		filter["volume_num"] = intent.VolumeNum
	}

	var childDocs []*schema.Document
	if s.esClient != nil {
		bm25Retriever := kbs.NewESBM25Retriever(s.esClient, index, hybridBM25TopK)
		hybridCfg := kbs.HybridConfig{
			VectorTopK:   hybridVectorTopK,
			BM25TopK:     hybridBM25TopK,
			RRFConstant:  hybridRRFConstant,
			FinalTopK:    hybridFinalTopK,
			EnableRerank: hybridEnableRerank,
			Reranker:     kbs.NewNoneReranker(),
		}
		hybrid := kbs.NewHybridRetriever(store, bm25Retriever, hybridCfg)

		// Multi-Query Expansion：迭代检索第一环
		var retriever interface {
			Search(ctx context.Context, query string, filters kbs.SearchFilter) ([]*schema.Document, error)
		} = hybrid

		if mqeEnableMQE {
			chatModel, mqeErr := s.getChatModel(knowledgeBase.ChatModelName, knowledgeBase.ChatModelProvider)
			if mqeErr == nil {
				expander := kbs.NewLLMQueryExpander(chatModel, mqeMaxSubQuery)
				retriever = kbs.NewMQERetriever(expander, hybrid, hybridCfg)
			} else {
				logs.Warnf("MQE: get ChatModel failed, skip expansion: %v", mqeErr)
			}
		}

		childDocs, err = retriever.Search(ctx, intent.Keywords, filter)
		if err != nil {
			logs.Warnf("hybrid search failed, fallback to vector only: %v", err)
			childDocs, err = store.Search(ctx, intent.Keywords, maxSearchResult*2, filter)
		}
	} else {
		logs.Infof("es not available, using vector-only search")
		childDocs, err = store.Search(ctx, intent.Keywords, maxSearchResult*2, filter)
	}
	if err != nil {
		logs.Errorf("search error: %v", err)
		return nil, err
	}

	// 对子分段按 parent_id 聚合
	parentIdMap := make(map[string]float64)
	var orderedParentIds []string
	for _, cd := range childDocs {
		pId, ok := cd.MetaData["parent_id"].(string)
		if !ok {
			continue
		}
		if _, seen := parentIdMap[pId]; !seen {
			orderedParentIds = append(orderedParentIds, pId)
			parentIdMap[pId] = cd.Score()
		}
	}
	if len(orderedParentIds) == 0 {
		return &SearchResponse{
			KbId:  kbId,
			Query: params.Query,
			Took:  time.Since(startTime).Microseconds(),
			Total: 0,
		}, nil
	}
	if len(orderedParentIds) > maxSearchResult {
		orderedParentIds = orderedParentIds[:maxSearchResult]
	}

	parentChunks, err := s.repo.getDocumentChunksByIds(ctx, orderedParentIds)
	if err != nil {
		logs.Errorf("get document chunks error: %v", err)
		return nil, errs.DBError
	}
	results := make([]*SearchResult, 0, len(parentChunks))
	for i, chunk := range parentChunks {
		results = append(results, &SearchResult{
			Content:    chunk.Content,
			DocumentId: chunk.DocumentID,
			Id:         chunk.ID,
			Metadata:   chunk.MetaInfo,
			Position:   i,
			Score:      parentIdMap[chunk.ID.String()],
		})
	}
	return &SearchResponse{
		KbId:    kbId,
		Query:   params.Query,
		Results: results,
		Took:    time.Since(startTime).Microseconds(),
		Total:   int64(len(results)),
	}, nil
}

func (s *service) parseMarkdownHeaders(content string) []*schema.Document {
	var docs []*schema.Document
	scanner := bufio.NewScanner(strings.NewReader(content))
	// 正则匹配 #, ##, ### (最多支持到 h6)
	// 格式: 行首 + 1-6个# + 空格 + 标题内容
	headerRegex := regexp.MustCompile(`^(#{1,6})\s+(.*)`)
	var currentBuffer strings.Builder
	// 记录当前的标题层级状态 {"h1": "标题A", "h2": "子标题B"}
	currentHeaders := make(map[string]string)

	flushBuffer := func() {
		text := strings.TrimSpace(currentBuffer.String())
		if text == "" {
			return
		}
		// 深拷贝当前的header状态，防止后续修改影响
		meta := make(map[string]interface{})
		for k, v := range currentHeaders {
			meta[k] = v
		}

		docs = append(docs, &schema.Document{
			Content:  text,
			MetaData: meta,
		})
		currentBuffer.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		matches := headerRegex.FindStringSubmatch(line)

		if len(matches) == 3 {
			// === 发现新标题 ===
			// 1. 如果缓冲区有上一段的内容，先保存上一段
			flushBuffer()

			// 2. 更新层级上下文
			hashes := matches[1]                       // "##"
			titleText := strings.TrimSpace(matches[2]) // "部署指南"
			level := len(hashes)                       // 2

			// 记录当前级别标题
			levelKey := fmt.Sprintf("h%d", level)
			currentHeaders[levelKey] = titleText

			// 清除比当前级别更深的标题 (例如遇到新的 h2，旧的 h3, h4 应该失效)
			for i := level + 1; i <= 6; i++ {
				delete(currentHeaders, fmt.Sprintf("h%d", i))
			}
			// 3. 将标题行本身也写入新缓冲区的开头 (可选项，有助于语义完整)
			currentBuffer.WriteString(line + "\n")
		} else {
			currentBuffer.WriteString(line + "\n")
		}
	}
	flushBuffer()

	return docs
}

func (s *service) buildChildSchemaDoc(parentId uuid.UUID, doc *model.Document, kb *model.KnowledgeBase,
	text string, i int, j int, k int, meta map[string]any) *schema.Document {

	data := map[string]interface{}{
		"doc_id":    doc.ID.String(),
		"kb_id":     kb.ID.String(),
		"parent_id": parentId.String(),
		"seq":       fmt.Sprintf("%d.%d.%d", i, j, k), // 三维索引
	}
	if meta != nil {
		for k, v := range meta {
			data[k] = v
		}
	}
	return &schema.Document{
		ID:       uuid.New().String(),
		Content:  text,
		MetaData: data,
	}
}

func (s *service) saveToStores(ctx context.Context, kb *model.KnowledgeBase, parentModels []*model.DocumentChunk, docs []*schema.Document) error {
	// 父分段存入pg
	err := s.repo.createDocumentChunks(ctx, parentModels)
	if err != nil {
		logs.Errorf("create document chunks error: %v", err)
		return err
	}
	// 子分段存入 milvus（向量索引）
	embedder, err := s.getEmbeddingConfig(kb.EmbeddingModelProvider, kb.EmbeddingModelName, kb.CreatorID)
	if err != nil {
		logs.Errorf("get embedding config error: %v", err)
		return biz.ErrEmbeddingConfigNotFound
	}
	indexName := s.buildIndex(kb.ID)
	store, err := kbs.NewMilvusVectorStore(ctx, s.milvusClient, indexName, embedder)
	if err != nil {
		logs.Errorf("new indexer error: %v", err)
		return err
	}

	if err = store.Store(ctx, docs); err != nil {
		logs.Errorf("milvus store error: %v", err)
		return err
	}

	// 子分段存入 Elasticsearch（BM25 文本索引）
	if s.esClient != nil {
		bm25Retriever := kbs.NewESBM25Retriever(s.esClient, indexName, 20)
		if err := bm25Retriever.EnsureIndex(ctx); err != nil {
			logs.Warnf("es ensure index error (non-fatal): %v", err)
		}
		if err := bm25Retriever.Index(ctx, docs); err != nil {
			logs.Warnf("es bm25 index error (non-fatal): %v", err)
		} else {
			logs.Infof("indexed %d chunks to ES BM25 for kb %s", len(docs), kb.ID)
		}
	}
	return nil
}

func (s *service) processDocx(sections []*schema.Document, doc *model.Document, parentModels []*model.DocumentChunk, kb *model.KnowledgeBase, childSchemaDocs []*schema.Document) ([]*model.DocumentChunk, []*schema.Document) {
	for _, sec := range sections { // 面包屑导航
		sectionType := sec.MetaData["sectionType"].(string)
		sectionLabel := s.mapSectionToChinese(sectionType)
		breadcrumb := fmt.Sprintf("【文档：%s】> 【%s】", doc.Name, sectionLabel)

		parentTexts := utils.SplitByWindow(sec.Content, 1000, 200)
		for i, text := range parentTexts {
			endContent := breadcrumb + "> " + text
			parentId := uuid.New()
			parentModel := &model.DocumentChunk{
				BaseModel: model.BaseModel{
					ID: parentId,
				},
				Content:         endContent,
				DocumentID:      doc.ID,
				KnowledgeBaseID: kb.ID,
				ChunkIndex:      i,
				MetaInfo:        sec.MetaData,
				TokenCount:      utils.GetTokenCount(endContent),
				Status:          model.ChunkStatusEmbedded,
			}
			parentModels = append(parentModels, parentModel)

			pathPrefix := breadcrumb + "\n"
			childTexts := utils.SplitByWindow(text, 400, 50)
			for j, childText := range childTexts {
				childSchemaDoc := s.buildChildSchemaDoc(parentId, doc, kb, pathPrefix+childText, i, j, 0, nil)
				childSchemaDocs = append(childSchemaDocs, childSchemaDoc)
			}
		}
	}
	return parentModels, childSchemaDocs
}

func (s *service) mapSectionToChinese(sectionType string) string {
	switch sectionType {
	case "main":
		return "正文"
	case "header":
		return "标题"
	case "footer":
		return "页脚"
	case "table":
		return "表格"
	default:
		return "文档片段"
	}
}

func (s *service) processPDF(pages []*schema.Document, doc *model.Document, parentModels []*model.DocumentChunk, kb *model.KnowledgeBase, childSchemaDocs []*schema.Document) ([]*model.DocumentChunk, []*schema.Document) {
	if len(pages) == 0 {
		return parentModels, childSchemaDocs
	}

	parentTexts := s.cleanPDFText(pages[0].Content)
	for j, text := range parentTexts {
		breadcrumb := fmt.Sprintf("【文档：%s】> 【第%d页】", doc.Name, j+1)
		endContent := breadcrumb + "\n" + text
		parentId := uuid.New()
		parentModel := &model.DocumentChunk{
			BaseModel: model.BaseModel{
				ID: parentId,
			},
			Content:         endContent,
			DocumentID:      doc.ID,
			KnowledgeBaseID: kb.ID,
			ChunkIndex:      j,
			MetaInfo: map[string]interface{}{
				"page": j + 1,
			},
			TokenCount: utils.GetTokenCount(endContent),
			Status:     model.ChunkStatusEmbedded,
		}
		parentModels = append(parentModels, parentModel)

		pathPrefix := breadcrumb + "\n"
		childTexts := utils.SplitByWindow(text, 400, 50)
		for k, childText := range childTexts {
			childSchemaDoc := s.buildChildSchemaDoc(parentId, doc, kb, pathPrefix+childText, j, k, 0, nil)
			childSchemaDocs = append(childSchemaDocs, childSchemaDoc)
		}
	}
	return parentModels, childSchemaDocs
}

func (s *service) cleanPDFText(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\t", "")
	content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
	boundaryPatterns := []string{
		`\s#\s*`,        // 标题
		`Chapter\s+\d+`, // 英文章节
		`第[一二三四五六七八九十]+[章节]`, // 中文章节
	}
	for _, p := range boundaryPatterns {
		re := regexp.MustCompile(p)
		content = re.ReplaceAllString(content, "\n\n$0")
	}

	var parents []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		parents = append(parents, strings.TrimSpace(buf.String()))
		buf.Reset()
	}
	rawBlocks := strings.Split(content, "\n")
	for _, block := range rawBlocks {
		block = strings.TrimSpace(block)
		if block == "" {
			flush()
			continue
		}
		if looksLikeCode(block) {
			flush()
			parents = append(parents, block)
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString(" ")
		}
		buf.WriteString(block)
		// 判断是否有强语义结束
		if lookLikeSentenceEnd(block) {
			flush()
		}
	}
	flush()
	return deduplicateParents(parents)
}

func (s *service) processHtml(docs []*schema.Document, doc *model.Document, parentModels []*model.DocumentChunk,
	kb *model.KnowledgeBase, childSchemaDocs []*schema.Document) ([]*model.DocumentChunk, []*schema.Document) {

	if len(docs) == 0 {
		return parentModels, childSchemaDocs
	}
	htmlDoc := docs[0]
	htmlContent := htmlDoc.Content
	if htmlContent == "" {
		return parentModels, childSchemaDocs
	}

	dom, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		logs.Errorf("new document from reader error: %v", err)
		return parentModels, childSchemaDocs
	}
	webTitle := htmlDoc.MetaData[html.MetaKeyTitle].(string)
	if webTitle == "" {
		webTitle = doc.Name
	}
	type Block struct {
		Tag    string
		Text   string
		IsCode bool
	}
	var blocks []Block
	isHeading := func(tag string) bool {
		return regexp.MustCompile(`^h[1-6]$`).MatchString(tag)
	}
	isAtom := func(tag string) bool {
		switch tag {
		case "code", "pre", "blockquote", "ul", "ol", "li", "table":
			return true
		}
		return false
	}

	body := dom.Find("body")
	if body.Length() == 0 {
		body = dom.Selection
	}

	processed := make(map[*html2.Node]bool)
	body.Find("*").Each(func(i int, s *goquery.Selection) {
		node := s.Get(0)
		if processed[node] {
			return
		}
		tag := strings.ToLower(goquery.NodeName(s))
		// 标题
		if isHeading(tag) {
			blocks = append(blocks, Block{
				Tag:  tag,
				Text: strings.TrimSpace(s.Text()),
			})
			processed[node] = true
			return
		}
		// 原子块
		if isAtom(tag) {
			txt := strings.TrimSpace(s.Text())
			if txt != "" {
				blocks = append(blocks, Block{
					Tag:    tag,
					Text:   txt,
					IsCode: tag == "code" || tag == "pre",
				})
			}
			s.Find("*").Each(func(i int, sub *goquery.Selection) {
				processed[sub.Get(0)] = true
			})
			processed[node] = true
			return
		}
		// 普通文本
		if s.Children().Length() == 0 {
			txt := strings.TrimSpace(s.Text())
			if txt != "" {
				blocks = append(blocks, Block{
					Tag:  "p",
					Text: txt,
				})
			}
			processed[node] = true
		}
	})
	// 语义聚合
	var (
		h1, h2, h3  string
		buf         strings.Builder
		parentIndex = 0
	)
	flush := func() {
		content := strings.TrimSpace(buf.String())
		if content == "" {
			return
		}
		parentId := uuid.New()
		breadcrumb := fmt.Sprintf("【网页:%s】", webTitle)
		if h1 != "" {
			breadcrumb += " > " + h1
		}
		if h2 != "" {
			breadcrumb += " > " + h2
		}
		if h3 != "" {
			breadcrumb += " > " + h3
		}
		fullContent := breadcrumb + "\n" + content
		parentModel := &model.DocumentChunk{
			BaseModel: model.BaseModel{
				ID: parentId,
			},
			Content:         fullContent,
			DocumentID:      doc.ID,
			KnowledgeBaseID: kb.ID,
			ChunkIndex:      parentIndex,
			MetaInfo: map[string]interface{}{
				"h1": h1,
				"h2": h2,
				"h3": h3,
			},
			TokenCount: utils.GetTokenCount(fullContent),
			Status:     model.ChunkStatusEmbedded,
		}
		parentModels = append(parentModels, parentModel)
		// 子分段切分
		pathPrefix := breadcrumb + "\n"
		childTexts := utils.SplitByWindow(content, 400, 50)
		for k, childText := range childTexts {
			childSchemaDoc := s.buildChildSchemaDoc(parentId, doc, kb, pathPrefix+childText, parentIndex, k, 0, nil)
			childSchemaDocs = append(childSchemaDocs, childSchemaDoc)
		}
		buf.Reset()
		parentIndex++
	}
	for _, b := range blocks {
		switch b.Tag {
		case "h1":
			flush()
			h1, h2, h3 = b.Text, "", ""
		case "h2":
			flush()
			h2, h3 = b.Text, ""
		case "h3":
			buf.WriteString("\n### ")
			buf.WriteString(b.Text)
			buf.WriteString("\n")
		default:
			if b.IsCode {
				buf.WriteString("\n```\n")
				buf.WriteString(b.Text)
				buf.WriteString("\n```\n")
			} else {
				buf.WriteString(b.Text)
				buf.WriteString("\n")
			}
		}
		// 父块的理想长度
		if buf.Len() >= 1000 {
			flush()
		}
	}
	flush()
	return parentModels, childSchemaDocs
}

func (s *service) processEpub(chapters []*schema.Document, doc *model.Document, parentModels []*model.DocumentChunk,
	kb *model.KnowledgeBase, childSchemaDocs []*schema.Document) ([]*model.DocumentChunk, []*schema.Document) {

	if len(chapters) == 0 {
		return parentModels, childSchemaDocs
	}

	for i, chapter := range chapters {
		bookTitle := chapter.MetaData["book_title"].(string)
		if bookTitle == "" {
			bookTitle = doc.Name
		}
		// 章节
		chapterTitle := chapter.MetaData["chapter"].(string)
		if chapterTitle == "" {
			chapterTitle = "未定义章节"
		}
		breadcrumb := fmt.Sprintf("【书名:%s】 > 【章节:%s】", bookTitle, chapterTitle)

		fullParentContent := breadcrumb + "\n" + chapter.Content
		parentId := uuid.New()
		// 解析复杂标题，比如卷名，章节号 卷号 标题等等
		parsed := utils.ParseComplexTitle(chapterTitle)
		parentModel := &model.DocumentChunk{
			BaseModel: model.BaseModel{
				ID: parentId,
			},
			Content:         fullParentContent,
			DocumentID:      doc.ID,
			KnowledgeBaseID: kb.ID,
			ChunkIndex:      i,
			MetaInfo: map[string]interface{}{
				"chapter_num": parsed.ChapterNum,
				"volume_num":  parsed.VolumeNum,
				"volume_name": parsed.VolumeName,
				"raw_title":   parsed.RawTitle,
				"full_title":  chapterTitle,
			},
			TokenCount: utils.GetTokenCount(fullParentContent),
			Status:     model.ChunkStatusEmbedded,
		}
		parentModels = append(parentModels, parentModel)
		// 生成child
		childTexts := utils.SplitByWindow(chapter.Content, 400, 50)
		for k, childText := range childTexts {
			childSchemaDoc := s.buildChildSchemaDoc(parentId, doc, kb, breadcrumb+"\n"+childText, i, k, 0, parentModel.MetaInfo)
			childSchemaDocs = append(childSchemaDocs, childSchemaDoc)
		}
	}
	return parentModels, childSchemaDocs
}

type QueryIntent struct {
	Keywords   string `json:"keywords"`
	VolumeNum  int    `json:"volume_num"`  // 卷号0表示未指定
	ChapterNum int    `json:"chapter_num"` // 章节号0表示未指定
	DocName    string `json:"doc_name"`
}

func (s *service) parseQueryIntent(ctx context.Context, kb *model.KnowledgeBase, query string) (*QueryIntent, error) {
	prompt := `你是一个结构化数据提取助手。请从用户的提问中提取查询关键词、卷号和章节号。
规则：
1. volume_num: 提取"卷"的信息（如：第四卷、卷4）。若未提取到则返回 0。
2. chapter_num: 提取"章/回/节"的信息（如：第500章、五百回）。若未提取到则返回 0。
3. keywords: 除去卷和章信息后的核心查询关键词。
4. 所有的中文数字（如：第四卷、第五百回）必须转换为阿拉伯数字整数（4, 500）。
5. 必须仅返回 JSON 格式数据。

示例：
问题："凡人修仙传第四卷风起海外第五百章讲了什么？"
输出：{"keywords": "讲了什么", "volume_num": 4, "chapter_num": 500}`
	chatModel, err := s.getChatModel(kb.ChatModelName, kb.ChatModelProvider)
	if err != nil {
		logs.Errorf("getChatModel 获取对话模型失败: %v", err)
		return nil, err
	}
	message, err := chatModel.Generate(ctx, []*schema.Message{
		{
			Role:    schema.System,
			Content: prompt,
		},
		{
			Role:    schema.User,
			Content: query,
		},
	})
	if err != nil {
		logs.Errorf("generate 模型生成失败: %v", err)
		return nil, err
	}

	rawJSON := message.Content
	rawJSON = strings.TrimPrefix(rawJSON, "```json")
	rawJSON = strings.TrimPrefix(rawJSON, "```")
	rawJSON = strings.TrimSuffix(rawJSON, "```")
	rawJSON = strings.TrimSpace(rawJSON)
	var intent QueryIntent
	if err := json.Unmarshal([]byte(rawJSON), &intent); err != nil {
		logs.Errorf("json.Unmarshal 解析失败: %v", err)
		return &QueryIntent{Keywords: query, VolumeNum: 0, ChapterNum: 0}, nil
	}
	if intent.Keywords == "" {
		intent.Keywords = query
	}
	return &intent, nil
}

func (s *service) getChatModel(modelName string, modelProvider string) (aiModel.ToolCallingChatModel, error) {
	ctx := context.Background()
	var chatModel aiModel.ToolCallingChatModel
	var err error

	chatProviderConfig, err := s.getProviderConfig(ctx, model.LLMTypeChat, modelProvider, modelName)
	if err != nil {
		logs.Errorf("获取模型配置失败: %v", err)
		return nil, err
	}
	if chatProviderConfig == nil {
		return nil, biz.ErrProviderConfigNotFound
	}

	if chatProviderConfig.Provider == model.OllamaProvider {
		chatModel, err = ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
			Model:   modelName,
			BaseURL: chatProviderConfig.APIBase,
		})
	} else if chatProviderConfig.Provider == model.QwenProvider {
		chatModel, err = qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
			Model:   modelName,
			BaseURL: chatProviderConfig.APIBase,
			APIKey:  chatProviderConfig.APIKey,
		})
	} else {
		chatModel, err = openai.NewChatModel(ctx, &openai.ChatModelConfig{
			Model:   modelName,
			BaseURL: chatProviderConfig.APIBase,
			APIKey:  chatProviderConfig.APIKey,
		})
	}
	return chatModel, err
}

func (s *service) getProviderConfig(ctx context.Context, llmType model.LLMType, provider string, name string) (*model.ProviderConfig, error) {
	trigger, err := event.Trigger("getProviderConfig", &shared.GetProviderConfigsRequest{
		Provider:  provider,
		ModelName: name,
		LLMType:   llmType,
	})
	if err != nil {
		logs.Errorf("触发getProviderConfig事件失败: %v", err)
		return nil, errs.DBError
	}
	result := trigger.(*model.ProviderConfig)
	return result, nil
}

func (s *service) deleteMilvusIndex(ctx context.Context, kbId uuid.UUID, docId uuid.UUID) error {
	index := s.buildIndex(kbId)
	expr := fmt.Sprintf("doc_id=='%s'", docId.String())
	err := s.milvusClient.Delete(ctx, index, "", expr)
	if err != nil {
		if errors.Is(err, client.ErrCollectionNotExists{}) {
			return nil
		}
		logs.Errorf("删除milvus索引失败: %v", err)
		return err
	}
	return nil
}

func deduplicateParents(parents []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, p := range parents {
		key := strings.TrimSpace(p)
		if len(key) < 20 {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, p)
	}
	return result
}

func lookLikeSentenceEnd(block string) bool {
	return regexp.MustCompile(`[。！？.!?]$`).MatchString(block)
}

func looksLikeCode(block string) bool {
	return strings.Contains(block, "package ") ||
		strings.Contains(block, "func ") ||
		strings.Contains(block, "class ") ||
		strings.Contains(block, "def ") ||
		strings.Contains(block, "import ") ||
		strings.Contains(block, "from ") ||
		strings.Contains(block, "using ") ||
		strings.Contains(block, "namespace ") ||
		strings.Contains(block, "struct ") ||
		strings.Contains(block, "interface ") ||
		strings.Contains(block, "enum ") ||
		strings.Contains(block, "{ ") ||
		strings.Contains(block, "}")
}

func newService() *service {
	conf := config.GetConfig()

	esConfig := elasticsearch.Config{
		Addresses: conf.Elasticsearch.GetAddresses(),
	}
	if conf.Elasticsearch.GetUsername() != "" {
		esConfig.Username = conf.Elasticsearch.GetUsername()
	}
	if conf.Elasticsearch.GetPassword() != "" {
		esConfig.Password = conf.Elasticsearch.GetPassword()
	}
	if conf.Elasticsearch.GetAPIKey() != "" {
		esConfig.APIKey = conf.Elasticsearch.GetAPIKey()
	}

	esClient, err := elasticsearch.NewClient(esConfig)
	if err != nil {
		logs.Errorf("elasticsearch client init error: %v", err)
		panic(err)
	}

	milvusConfig := client.Config{
		Address: conf.Milvus.GetAddress(),
	}
	if conf.Milvus.GetDBName() != "" {
		milvusConfig.DBName = conf.Milvus.GetDBName()
	}
	if conf.Milvus.GetUsername() != "" {
		milvusConfig.Username = conf.Milvus.GetUsername()
	}
	if conf.Milvus.GetPassword() != "" {
		milvusConfig.Password = conf.Milvus.GetPassword()
	}

	milvusClient, err := client.NewClient(context.Background(), milvusConfig)
	if err != nil {
		logs.Errorf("milvus client init error: %v", err)
		panic(err)
	}

	logs.Infof("elasticsearch and milvus client init success, es: %v, milvus: %s",
		conf.Elasticsearch.GetAddresses(), conf.Milvus.GetAddress())

	return &service{
		repo:         newModels(database.GetPostgresDB().GormDB),
		esClient:     esClient,
		milvusClient: milvusClient,
	}
}

func (s *service) Close() error {
	if s.milvusClient != nil {
		return s.milvusClient.Close()
	}
	return nil
}
