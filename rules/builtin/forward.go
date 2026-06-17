package builtin

// ForwardRule passes the payload to the target unchanged.
// It is the simplest rule type and is useful for topic bridging across brokers.
type ForwardRule struct {
	name        string
	sourceTopic string
}

func NewForwardRule(name, sourceTopic string) *ForwardRule {
	return &ForwardRule{name: name, sourceTopic: sourceTopic}
}

func (r *ForwardRule) Name() string        { return r.name }
func (r *ForwardRule) SourceTopic() string { return r.sourceTopic }

func (r *ForwardRule) Process(payload []byte) ([]byte, error) {
	return payload, nil
}
