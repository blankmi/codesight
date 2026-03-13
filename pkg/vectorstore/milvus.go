package vectorstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	metadataCollectionSuffix = "_meta"
	fieldID                  = "id"
	fieldContent             = "content"
	fieldFilePath            = "file_path"
	fieldStartLine           = "start_line"
	fieldEndLine             = "end_line"
	fieldLanguage            = "language"
	fieldNodeType            = "node_type"
	fieldVector              = "vector"
	fieldMetaKey             = "key"
	fieldMetaValue           = "value"
	fieldMetaVector          = "meta_vector"
	metaVectorDimension      = 2
	indexName                = "vector_idx"
	metricType               = entity.COSINE
)

// MilvusStore implements Store backed by Milvus.
type MilvusStore struct {
	address   string
	token     string
	client    client.Client
	dimension int
}

// NewMilvusStore creates a new MilvusStore.
func NewMilvusStore(address string, token string) *MilvusStore {
	return &MilvusStore{
		address: address,
		token:   token,
	}
}

func (m *MilvusStore) Connect(ctx context.Context) error {
	opts := []client.Config{
		{Address: m.address},
	}
	if m.token != "" {
		opts[0].APIKey = m.token
	}
	c, err := client.NewClient(ctx, opts[0])
	if err != nil {
		return fmt.Errorf("milvus connect: %w", err)
	}
	m.client = c
	return nil
}

func (m *MilvusStore) Close() error {
	if m.client != nil {
		return m.client.Close()
	}
	return nil
}

func (m *MilvusStore) CreateCollection(ctx context.Context, name string, dimension int) error {
	m.dimension = dimension

	schema := &entity.Schema{
		CollectionName: name,
		Fields: []*entity.Field{
			{Name: fieldID, DataType: entity.FieldTypeVarChar, PrimaryKey: true, AutoID: false, TypeParams: map[string]string{"max_length": "256"}},
			{Name: fieldContent, DataType: entity.FieldTypeVarChar, TypeParams: map[string]string{"max_length": "65535"}},
			{Name: fieldFilePath, DataType: entity.FieldTypeVarChar, TypeParams: map[string]string{"max_length": "1024"}},
			{Name: fieldStartLine, DataType: entity.FieldTypeInt64},
			{Name: fieldEndLine, DataType: entity.FieldTypeInt64},
			{Name: fieldLanguage, DataType: entity.FieldTypeVarChar, TypeParams: map[string]string{"max_length": "64"}},
			{Name: fieldNodeType, DataType: entity.FieldTypeVarChar, TypeParams: map[string]string{"max_length": "64"}},
			{Name: fieldVector, DataType: entity.FieldTypeFloatVector, TypeParams: map[string]string{"dim": strconv.Itoa(dimension)}},
		},
	}

	if err := m.client.CreateCollection(ctx, schema, entity.DefaultShardNumber); err != nil {
		return fmt.Errorf("milvus create collection %s: %w", name, err)
	}

	idx, err := entity.NewIndexIvfFlat(metricType, 128)
	if err != nil {
		return fmt.Errorf("milvus create index params: %w", err)
	}
	if err := m.client.CreateIndex(ctx, name, fieldVector, idx, false); err != nil {
		return fmt.Errorf("milvus create index on %s: %w", name, err)
	}

	if err := m.client.LoadCollection(ctx, name, false); err != nil {
		return fmt.Errorf("milvus load collection %s: %w", name, err)
	}

	// Create metadata collection
	metaName := name + metadataCollectionSuffix
	metaSchema := &entity.Schema{
		CollectionName: metaName,
		Fields: []*entity.Field{
			{Name: fieldMetaKey, DataType: entity.FieldTypeVarChar, PrimaryKey: true, AutoID: false, TypeParams: map[string]string{"max_length": "256"}},
			{Name: fieldMetaValue, DataType: entity.FieldTypeVarChar, TypeParams: map[string]string{"max_length": "65535"}},
			{Name: fieldMetaVector, DataType: entity.FieldTypeFloatVector, TypeParams: map[string]string{"dim": strconv.Itoa(metaVectorDimension)}},
		},
	}
	if err := m.client.CreateCollection(ctx, metaSchema, entity.DefaultShardNumber); err != nil {
		return fmt.Errorf("milvus create metadata collection %s: %w", metaName, err)
	}
	metaIdx, err := entity.NewIndexIvfFlat(metricType, 8)
	if err != nil {
		return fmt.Errorf("milvus create metadata index params: %w", err)
	}
	if err := m.client.CreateIndex(ctx, metaName, fieldMetaVector, metaIdx, false); err != nil {
		return fmt.Errorf("milvus create metadata index on %s: %w", metaName, err)
	}
	if err := m.client.LoadCollection(ctx, metaName, false); err != nil {
		return fmt.Errorf("milvus load metadata collection %s: %w", metaName, err)
	}

	return nil
}

func (m *MilvusStore) DropCollection(ctx context.Context, name string) error {
	if err := m.client.DropCollection(ctx, name); err != nil {
		return fmt.Errorf("milvus drop collection %s: %w", name, err)
	}
	metaName := name + metadataCollectionSuffix
	_ = m.client.DropCollection(ctx, metaName)
	return nil
}

func (m *MilvusStore) CollectionExists(ctx context.Context, name string) (bool, error) {
	exists, err := m.client.HasCollection(ctx, name)
	if err != nil {
		return false, fmt.Errorf("milvus check collection %s: %w", name, err)
	}
	return exists, nil
}

func (m *MilvusStore) Insert(ctx context.Context, collection string, docs []Document, vectors [][]float32) error {
	if len(docs) != len(vectors) {
		return fmt.Errorf("milvus insert: docs count %d != vectors count %d", len(docs), len(vectors))
	}
	if len(docs) == 0 {
		return nil
	}

	ids := make([]string, len(docs))
	contents := make([]string, len(docs))
	filePaths := make([]string, len(docs))
	startLines := make([]int64, len(docs))
	endLines := make([]int64, len(docs))
	languages := make([]string, len(docs))
	nodeTypes := make([]string, len(docs))
	vecs := make([][]float32, len(docs))

	for i, doc := range docs {
		docID := doc.ID
		if docID == "" {
			docID = generateDocID(doc.FilePath, doc.StartLine, doc.EndLine)
		}
		ids[i] = docID
		contents[i] = truncateContent(doc.Content, 65535)
		filePaths[i] = doc.FilePath
		startLines[i] = int64(doc.StartLine)
		endLines[i] = int64(doc.EndLine)
		languages[i] = doc.Language
		nodeTypes[i] = doc.NodeType
		vecs[i] = vectors[i]
	}

	idCol := entity.NewColumnVarChar(fieldID, ids)
	contentCol := entity.NewColumnVarChar(fieldContent, contents)
	filePathCol := entity.NewColumnVarChar(fieldFilePath, filePaths)
	startLineCol := entity.NewColumnInt64(fieldStartLine, startLines)
	endLineCol := entity.NewColumnInt64(fieldEndLine, endLines)
	langCol := entity.NewColumnVarChar(fieldLanguage, languages)
	nodeTypeCol := entity.NewColumnVarChar(fieldNodeType, nodeTypes)
	vectorCol := entity.NewColumnFloatVector(fieldVector, m.dimension, vecs)

	if _, err := m.client.Insert(ctx, collection, "",
		idCol, contentCol, filePathCol, startLineCol, endLineCol, langCol, nodeTypeCol, vectorCol,
	); err != nil {
		return fmt.Errorf("milvus insert into %s: %w", collection, err)
	}

	return nil
}

func (m *MilvusStore) Search(ctx context.Context, collection string, vector []float32, limit int, filters map[string]string) ([]SearchResult, error) {
	searchVectors := []entity.Vector{entity.FloatVector(vector)}

	sp, err := entity.NewIndexIvfFlatSearchParam(16)
	if err != nil {
		return nil, fmt.Errorf("milvus search params: %w", err)
	}

	expr := ""
	if lang, ok := filters["language"]; ok && lang != "" {
		expr = fmt.Sprintf(`language == "%s"`, lang)
	}

	results, err := m.client.Search(
		ctx,
		collection,
		nil,
		expr,
		[]string{fieldID, fieldContent, fieldFilePath, fieldStartLine, fieldEndLine, fieldLanguage, fieldNodeType},
		searchVectors,
		fieldVector,
		metricType,
		limit,
		sp,
	)
	if err != nil {
		return nil, fmt.Errorf("milvus search in %s: %w", collection, err)
	}

	var out []SearchResult
	for _, result := range results {
		for i := 0; i < result.ResultCount; i++ {
			doc := Document{}

			if col, ok := getVarCharColumn(result.Fields, fieldID); ok && i < len(col) {
				doc.ID = col[i]
			}
			if col, ok := getVarCharColumn(result.Fields, fieldContent); ok && i < len(col) {
				doc.Content = col[i]
			}
			if col, ok := getVarCharColumn(result.Fields, fieldFilePath); ok && i < len(col) {
				doc.FilePath = col[i]
			}
			if col, ok := getInt64Column(result.Fields, fieldStartLine); ok && i < len(col) {
				doc.StartLine = int(col[i])
			}
			if col, ok := getInt64Column(result.Fields, fieldEndLine); ok && i < len(col) {
				doc.EndLine = int(col[i])
			}
			if col, ok := getVarCharColumn(result.Fields, fieldLanguage); ok && i < len(col) {
				doc.Language = col[i]
			}
			if col, ok := getVarCharColumn(result.Fields, fieldNodeType); ok && i < len(col) {
				doc.NodeType = col[i]
			}

			score := float64(0)
			if i < len(result.Scores) {
				score = float64(result.Scores[i])
			}

			out = append(out, SearchResult{Document: doc, Score: score})
		}
	}

	return out, nil
}

func (m *MilvusStore) SetMetadata(ctx context.Context, collection string, meta IndexMetadata) error {
	metaName := collection + metadataCollectionSuffix

	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("milvus marshal metadata: %w", err)
	}

	keys := entity.NewColumnVarChar(fieldMetaKey, []string{"index_metadata"})
	values := entity.NewColumnVarChar(fieldMetaValue, []string{string(data)})
	vectors := entity.NewColumnFloatVector(fieldMetaVector, metaVectorDimension, [][]float32{{1, 0}})

	if _, err := m.client.Upsert(ctx, metaName, "", keys, values, vectors); err != nil {
		return fmt.Errorf("milvus set metadata on %s: %w", metaName, err)
	}

	return nil
}

func (m *MilvusStore) GetMetadata(ctx context.Context, collection string) (*IndexMetadata, error) {
	metaName := collection + metadataCollectionSuffix

	exists, err := m.client.HasCollection(ctx, metaName)
	if err != nil || !exists {
		return nil, nil
	}

	if err := m.client.LoadCollection(ctx, metaName, false); err != nil {
		return nil, fmt.Errorf("milvus load metadata collection %s: %w", metaName, err)
	}

	results, err := m.client.Query(ctx, metaName, nil, `key == "index_metadata"`, []string{fieldMetaValue})
	if err != nil {
		return nil, fmt.Errorf("milvus query metadata from %s: %w", metaName, err)
	}

	if len(results) == 0 {
		return nil, nil
	}

	if col, ok := getVarCharColumn(results, fieldMetaValue); ok && len(col) > 0 {
		var meta IndexMetadata
		if err := json.Unmarshal([]byte(col[0]), &meta); err != nil {
			return nil, fmt.Errorf("milvus unmarshal metadata: %w", err)
		}
		return &meta, nil
	}

	return nil, nil
}

func generateDocID(filePath string, startLine, endLine int) string {
	raw := fmt.Sprintf("%s:%d-%d", filePath, startLine, endLine)
	hash := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", hash[:16])
}

func truncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func getVarCharColumn(columns []entity.Column, name string) ([]string, bool) {
	for _, col := range columns {
		if col.Name() == name {
			data := col.FieldData().GetScalars().GetStringData()
			if data != nil {
				return data.Data, true
			}
		}
	}
	return nil, false
}

func getInt64Column(columns []entity.Column, name string) ([]int64, bool) {
	for _, col := range columns {
		if col.Name() == name {
			data := col.FieldData().GetScalars().GetLongData()
			if data != nil {
				return data.Data, true
			}
		}
	}
	return nil, false
}
