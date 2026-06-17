package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for the rule engine.
type Config struct {
	Source SourceConfig `yaml:"source"`
	Rules  []RuleConfig `yaml:"rules"`
	HTTP   HTTPConfig   `yaml:"http"`
}

// SourceConfig defines the edge-local MQTT broker to subscribe from.
type SourceConfig struct {
	Broker   string   `yaml:"broker"`
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	ClientID string   `yaml:"client_id"`
	QoS      byte     `yaml:"qos"`
	Topics   []string `yaml:"topics"`
}

// RuleConfig defines a single routing rule.
type RuleConfig struct {
	Name        string           `yaml:"name"`
	Enabled     bool             `yaml:"enabled"`
	SourceTopic string           `yaml:"source_topic"`
	// Type: forward | filter | transform | script
	Type        string           `yaml:"type"`
	Filter      *FilterConfig    `yaml:"filter,omitempty"`
	Transform   *TransformConfig `yaml:"transform,omitempty"`
	// Script: inline JS code; process(messages) function must be defined.
	Script     string       `yaml:"script,omitempty"`
	// ScriptFile: path to an external JS file (alternative to inline Script).
	ScriptFile string       `yaml:"script_file,omitempty"`
	Target     TargetConfig `yaml:"target"`
}

// FilterConfig defines conditions to filter sensor items.
type FilterConfig struct {
	// Field is the JSON field name to evaluate (e.g. "value").
	Field     string `yaml:"field"`
	// Operator: gt | lt | gte | lte | eq | neq | contains
	Operator  string `yaml:"operator"`
	// Threshold is the comparison value (string-encoded number or string).
	Threshold string `yaml:"threshold"`
}

// TransformConfig defines a sequence of field-level operations.
type TransformConfig struct {
	Operations []TransformOp `yaml:"operations"`
}

// TransformOp is a single field operation.
type TransformOp struct {
	// Op: rename | add | remove
	Op    string `yaml:"op"`
	From  string `yaml:"from,omitempty"`
	To    string `yaml:"to,omitempty"`
	Field string `yaml:"field,omitempty"`
	Value string `yaml:"value,omitempty"`
}

// TargetConfig defines where to send the processed message.
type TargetConfig struct {
	// Type: mqtt | http
	Type    string            `yaml:"type"`
	// MQTT target fields
	Broker  string            `yaml:"broker,omitempty"`
	Topic   string            `yaml:"topic,omitempty"`
	QoS     byte              `yaml:"qos"`
	// HTTP target fields
	URL            string            `yaml:"url,omitempty"`
	Method         string            `yaml:"method,omitempty"`
	Headers        map[string]string `yaml:"headers,omitempty"`
	TimeoutSeconds int               `yaml:"timeout_seconds,omitempty"`
}

// HTTPConfig defines the built-in HTTP management API.
type HTTPConfig struct {
	Port int `yaml:"port"`
}

// Load reads and parses the YAML config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	setDefaults(&cfg)
	return &cfg, nil
}

func setDefaults(cfg *Config) {
	if cfg.Source.ClientID == "" {
		cfg.Source.ClientID = "rule-engine"
	}
	if cfg.HTTP.Port == 0 {
		cfg.HTTP.Port = 9090
	}
	for i := range cfg.Rules {
		if cfg.Rules[i].Target.Method == "" {
			cfg.Rules[i].Target.Method = "POST"
		}
		if cfg.Rules[i].Target.TimeoutSeconds == 0 {
			cfg.Rules[i].Target.TimeoutSeconds = 10
		}
	}
}
