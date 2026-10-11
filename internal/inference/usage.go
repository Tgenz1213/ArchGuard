package inference

// Reported is false when the backend sent no usage, so a missing figure is never read as zero.
type Usage struct {
	InputTokens  int
	OutputTokens int
	Reported     bool
}

type ChatReply struct {
	Text  string
	Usage Usage
}

// An embedding call has no output, so only Usage.InputTokens is set.
type Embedding struct {
	Vector []float32
	Usage  Usage
}
