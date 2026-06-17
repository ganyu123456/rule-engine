package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"k8s.io/klog/v2"

	"github.com/kubeedge/rule-engine/config"
	"github.com/kubeedge/rule-engine/rules/builtin"
	"github.com/kubeedge/rule-engine/rules/script"
	"github.com/kubeedge/rule-engine/target"
)

// RuleStatus summarises a rule's runtime state (exposed via HTTP API).
type RuleStatus struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	SourceTopic string `json:"source_topic"`
	TargetType  string `json:"target_type"`
	TargetDest  string `json:"target_dest"`
	Enabled     bool   `json:"enabled"`
	Processed   int64  `json:"processed"`
	Dropped     int64  `json:"dropped"`
	Errors      int64  `json:"errors"`
}

// binding associates a compiled rule with its output target and runtime counters.
type binding struct {
	rule      Rule
	tgt       target.Target
	ruleType  string
	targetCfg config.TargetConfig
	mu        sync.Mutex
	processed int64
	dropped   int64
	errors    int64
}

// Engine subscribes to the source MQTT broker and routes messages through rules.
type Engine struct {
	cfg        *config.Config
	mqttClient mqtt.Client
	bindings   []*binding
	pool       *target.Pool
}

// New builds and validates the engine from the provided config.
func New(cfg *config.Config) (*Engine, error) {
	e := &Engine{
		cfg:  cfg,
		pool: target.NewPool(),
	}
	if err := e.buildBindings(); err != nil {
		return nil, err
	}
	return e, nil
}

// buildBindings compiles all enabled rules and resolves their targets.
func (e *Engine) buildBindings() error {
	for _, rc := range e.cfg.Rules {
		if !rc.Enabled {
			klog.Infof("rule %q disabled, skipping", rc.Name)
			continue
		}

		r, err := e.buildRule(rc)
		if err != nil {
			return fmt.Errorf("rule %q: %w", rc.Name, err)
		}

		// If target broker is empty, fall back to the source broker.
		tgtCfg := rc.Target
		if tgtCfg.Type == "mqtt" && tgtCfg.Broker == "" {
			tgtCfg.Broker = e.cfg.Source.Broker
		}

		tgt, err := e.pool.Get(tgtCfg)
		if err != nil {
			return fmt.Errorf("rule %q: build target: %w", rc.Name, err)
		}

		e.bindings = append(e.bindings, &binding{
			rule:      r,
			tgt:       tgt,
			ruleType:  rc.Type,
			targetCfg: tgtCfg,
		})

		dest := tgtCfg.Topic
		if tgtCfg.Type == "http" {
			dest = tgtCfg.URL
		}
		klog.Infof("rule registered: name=%q type=%s src=%s → target.type=%s dest=%s",
			rc.Name, rc.Type, rc.SourceTopic, tgtCfg.Type, dest)
	}
	return nil
}

// buildRule compiles one RuleConfig into a Rule implementation.
func (e *Engine) buildRule(rc config.RuleConfig) (Rule, error) {
	switch rc.Type {
	case "forward":
		return builtin.NewForwardRule(rc.Name, rc.SourceTopic), nil

	case "filter":
		if rc.Filter == nil {
			return nil, fmt.Errorf("type=filter requires a filter block")
		}
		return builtin.NewFilterRule(rc.Name, rc.SourceTopic, rc.Filter)

	case "transform":
		if rc.Transform == nil {
			return nil, fmt.Errorf("type=transform requires a transform block")
		}
		return builtin.NewTransformRule(rc.Name, rc.SourceTopic, rc.Transform), nil

	case "script":
		code := rc.Script
		if code == "" && rc.ScriptFile != "" {
			data, err := os.ReadFile(rc.ScriptFile)
			if err != nil {
				return nil, fmt.Errorf("read script_file %s: %w", rc.ScriptFile, err)
			}
			code = string(data)
		}
		if code == "" {
			return nil, fmt.Errorf("type=script requires script or script_file")
		}
		return script.NewJSRule(rc.Name, rc.SourceTopic, code)

	default:
		return nil, fmt.Errorf("unknown type %q (supported: forward, filter, transform, script)", rc.Type)
	}
}

// Run connects to the source MQTT broker, subscribes, and blocks until done is closed.
func (e *Engine) Run(done <-chan struct{}) error {
	if err := e.connectSource(); err != nil {
		return fmt.Errorf("connect source broker: %w", err)
	}

	for _, topic := range e.uniqueTopics() {
		token := e.mqttClient.Subscribe(topic, e.cfg.Source.QoS, e.dispatch)
		if !token.WaitTimeout(10 * time.Second) {
			return fmt.Errorf("subscribe timeout for topic %s", topic)
		}
		if token.Error() != nil {
			return fmt.Errorf("subscribe %s: %w", topic, token.Error())
		}
		klog.Infof("subscribed to source topic: %s", topic)
	}

	klog.Infof("rule engine running: %d active rules", len(e.bindings))
	<-done
	klog.Infoln("rule engine stopping")
	e.mqttClient.Disconnect(2000)
	e.pool.CloseAll()
	return nil
}

// dispatch is the MQTT message handler; it fans out to all matching rules.
func (e *Engine) dispatch(_ mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	for _, b := range e.bindings {
		if b.rule.SourceTopic() != topic {
			continue
		}

		result, err := b.rule.Process(payload)

		b.mu.Lock()
		if err != nil {
			b.errors++
			b.mu.Unlock()
			klog.Errorf("rule %q process error: %v", b.rule.Name(), err)
			continue
		}
		if result == nil {
			b.dropped++
			b.mu.Unlock()
			klog.V(5).Infof("rule %q: message dropped (filtered)", b.rule.Name())
			continue
		}
		b.processed++
		b.mu.Unlock()

		if err := b.tgt.Send(result); err != nil {
			klog.Errorf("rule %q target send error: %v", b.rule.Name(), err)
		}
	}
}

// connectSource establishes the MQTT subscription connection to the edge broker.
func (e *Engine) connectSource() error {
	clientID := e.cfg.Source.ClientID
	if clientID == "" {
		clientID = "rule-engine"
	}
	opts := mqtt.NewClientOptions().
		AddBroker(e.cfg.Source.Broker).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetOnConnectHandler(func(c mqtt.Client) {
			klog.Infof("source MQTT connected: %s", e.cfg.Source.Broker)
			for _, topic := range e.uniqueTopics() {
				c.Subscribe(topic, e.cfg.Source.QoS, e.dispatch)
			}
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			klog.Warningf("source MQTT connection lost: %v", err)
		})

	if e.cfg.Source.Username != "" {
		opts.SetUsername(e.cfg.Source.Username).SetPassword(e.cfg.Source.Password)
	}

	e.mqttClient = mqtt.NewClient(opts)
	token := e.mqttClient.Connect()
	if token.WaitTimeout(15*time.Second) && token.Error() != nil {
		return token.Error()
	}
	return nil
}

// uniqueTopics returns a deduplicated list of source topics used by active rules.
func (e *Engine) uniqueTopics() []string {
	seen := make(map[string]struct{})
	var topics []string
	for _, b := range e.bindings {
		t := b.rule.SourceTopic()
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			topics = append(topics, t)
		}
	}
	return topics
}

// Status returns a snapshot of all rule runtime counters for the HTTP API.
func (e *Engine) Status() []RuleStatus {
	statuses := make([]RuleStatus, 0, len(e.bindings))
	for _, b := range e.bindings {
		dest := b.targetCfg.Topic
		if b.targetCfg.Type == "http" {
			dest = b.targetCfg.URL
		}
		b.mu.Lock()
		s := RuleStatus{
			Name:        b.rule.Name(),
			Type:        b.ruleType,
			SourceTopic: b.rule.SourceTopic(),
			TargetType:  b.targetCfg.Type,
			TargetDest:  dest,
			Enabled:     true,
			Processed:   b.processed,
			Dropped:     b.dropped,
			Errors:      b.errors,
		}
		b.mu.Unlock()
		statuses = append(statuses, s)
	}
	return statuses
}

// MarshalJSON is a helper used by the HTTP handler.
func (e *Engine) MarshalStatus() ([]byte, error) {
	return json.Marshal(e.Status())
}
