package llm

type Provider interface {
	Embedder
	Chatter
}
