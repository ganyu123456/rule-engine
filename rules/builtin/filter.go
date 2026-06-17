package builtin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kubeedge/rule-engine/config"
)

// FilterRule retains only sensor items that satisfy a numeric or string condition.
//
// Supported operators:
//
//	gt | lt | gte | lte      – numeric comparison against Threshold
//	eq | neq                  – equality (numeric or string)
//	contains                  – string contains check
type FilterRule struct {
	name        string
	sourceTopic string
	field       string
	operator    string
	threshold   string
}

func NewFilterRule(name, sourceTopic string, cfg *config.FilterConfig) (*FilterRule, error) {
	op := strings.ToLower(cfg.Operator)
	switch op {
	case "gt", "lt", "gte", "lte", "eq", "neq", "contains":
	default:
		return nil, fmt.Errorf("unknown filter operator %q (supported: gt, lt, gte, lte, eq, neq, contains)", cfg.Operator)
	}
	return &FilterRule{
		name:        name,
		sourceTopic: sourceTopic,
		field:       cfg.Field,
		operator:    op,
		threshold:   cfg.Threshold,
	}, nil
}

func (r *FilterRule) Name() string        { return r.name }
func (r *FilterRule) SourceTopic() string { return r.sourceTopic }

func (r *FilterRule) Process(payload []byte) ([]byte, error) {
	var items []map[string]interface{}
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	var matched []map[string]interface{}
	for _, item := range items {
		ok, err := r.matches(item)
		if err != nil {
			return nil, err
		}
		if ok {
			matched = append(matched, item)
		}
	}

	if len(matched) == 0 {
		return nil, nil // all items filtered – drop message
	}

	out, err := json.Marshal(matched)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return out, nil
}

func (r *FilterRule) matches(item map[string]interface{}) (bool, error) {
	raw, ok := item[r.field]
	if !ok {
		return false, nil
	}

	if r.operator == "contains" {
		return strings.Contains(fmt.Sprintf("%v", raw), r.threshold), nil
	}

	// Numeric comparison
	itemVal, err := toFloat64(raw)
	if err != nil {
		// Fall back to string eq/neq
		strVal := fmt.Sprintf("%v", raw)
		switch r.operator {
		case "eq":
			return strVal == r.threshold, nil
		case "neq":
			return strVal != r.threshold, nil
		}
		return false, nil
	}

	threshVal, err := strconv.ParseFloat(r.threshold, 64)
	if err != nil {
		return false, fmt.Errorf("parse threshold %q as float: %w", r.threshold, err)
	}

	switch r.operator {
	case "gt":
		return itemVal > threshVal, nil
	case "lt":
		return itemVal < threshVal, nil
	case "gte":
		return itemVal >= threshVal, nil
	case "lte":
		return itemVal <= threshVal, nil
	case "eq":
		return itemVal == threshVal, nil
	case "neq":
		return itemVal != threshVal, nil
	}
	return false, nil
}

func toFloat64(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case json.Number:
		return val.Float64()
	case string:
		return strconv.ParseFloat(val, 64)
	}
	return 0, fmt.Errorf("cannot convert %T to float64", v)
}
