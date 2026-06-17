package builtin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kubeedge/rule-engine/config"
)

// TransformRule applies a sequence of field operations to every sensor item.
//
// Supported operations (op):
//
//	rename  – rename a field:  from → to
//	add     – add/overwrite a field with a constant string value
//	remove  – delete a field
type TransformRule struct {
	name        string
	sourceTopic string
	ops         []config.TransformOp
}

func NewTransformRule(name, sourceTopic string, cfg *config.TransformConfig) *TransformRule {
	return &TransformRule{
		name:        name,
		sourceTopic: sourceTopic,
		ops:         cfg.Operations,
	}
}

func (r *TransformRule) Name() string        { return r.name }
func (r *TransformRule) SourceTopic() string { return r.sourceTopic }

func (r *TransformRule) Process(payload []byte) ([]byte, error) {
	var items []map[string]interface{}
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	for i, item := range items {
		if err := r.applyOps(item); err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
	}

	out, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return out, nil
}

func (r *TransformRule) applyOps(item map[string]interface{}) error {
	for _, op := range r.ops {
		switch strings.ToLower(op.Op) {
		case "rename":
			if op.From == "" || op.To == "" {
				return fmt.Errorf("rename op requires from and to")
			}
			if val, ok := item[op.From]; ok {
				item[op.To] = val
				delete(item, op.From)
			}

		case "add":
			if op.Field == "" {
				return fmt.Errorf("add op requires field")
			}
			item[op.Field] = op.Value

		case "remove":
			if op.Field == "" {
				return fmt.Errorf("remove op requires field")
			}
			delete(item, op.Field)

		default:
			return fmt.Errorf("unknown transform op %q (supported: rename, add, remove)", op.Op)
		}
	}
	return nil
}
