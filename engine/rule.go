package engine

// Rule is the interface all rule types must implement.
type Rule interface {
	// Name returns the unique rule identifier.
	Name() string
	// SourceTopic returns the MQTT topic this rule listens on.
	SourceTopic() string
	// Process receives a raw JSON payload (array of sensor items) and returns
	// the transformed payload.  Returning (nil, nil) means the message is
	// intentionally dropped (filtered out) and will not be forwarded.
	Process(payload []byte) ([]byte, error)
}
