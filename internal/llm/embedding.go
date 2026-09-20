package llm

// Document is ADR content being indexed; query is a diff or code being searched.
type EmbeddingTaskType int

const (
	EmbeddingTaskDocument EmbeddingTaskType = iota
	EmbeddingTaskQuery
)

// Pick lets providers map the task role onto their backend's convention in one line.
func (t EmbeddingTaskType) Pick(document, query string) string {
	if t == EmbeddingTaskQuery {
		return query
	}
	return document
}
