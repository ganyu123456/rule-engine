package target

import (
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"k8s.io/klog/v2"

	"github.com/kubeedge/rule-engine/config"
)

// Target is the interface all output destinations must implement.
type Target interface {
	Send(payload []byte) error
	Close() error
}

// Pool manages shared MQTT connections (keyed by broker address) and creates
// HTTP targets on demand.  Multiple rules that target the same MQTT broker
// reuse a single underlying mqtt.Client.
type Pool struct {
	mu      sync.Mutex
	clients map[string]mqtt.Client // broker address → client
}

func NewPool() *Pool {
	return &Pool{clients: make(map[string]mqtt.Client)}
}

// Get returns an appropriate Target for the given TargetConfig.
func (p *Pool) Get(cfg config.TargetConfig) (Target, error) {
	switch cfg.Type {
	case "mqtt":
		client, err := p.mqttClient(cfg.Broker)
		if err != nil {
			return nil, err
		}
		return &MQTTTarget{client: client, topic: cfg.Topic, qos: cfg.QoS}, nil

	case "http":
		timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
		if timeout == 0 {
			timeout = 10 * time.Second
		}
		method := cfg.Method
		if method == "" {
			method = "POST"
		}
		return &HTTPTarget{
			url:     cfg.URL,
			method:  method,
			headers: cfg.Headers,
			timeout: timeout,
		}, nil

	default:
		return nil, fmt.Errorf("unknown target type %q (supported: mqtt, http)", cfg.Type)
	}
}

// mqttClient returns an existing connected client for broker, or creates one.
func (p *Pool) mqttClient(broker string) (mqtt.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.clients[broker]; ok && c.IsConnected() {
		return c, nil
	}

	clientID := fmt.Sprintf("rule-engine-pub-%d", time.Now().UnixNano())
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetOnConnectHandler(func(_ mqtt.Client) {
			klog.Infof("target MQTT connected: %s", broker)
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			klog.Warningf("target MQTT connection lost (%s): %v", broker, err)
		})

	c := mqtt.NewClient(opts)
	token := c.Connect()
	if token.WaitTimeout(15*time.Second) && token.Error() != nil {
		return nil, fmt.Errorf("connect to target broker %s: %w", broker, token.Error())
	}

	p.clients[broker] = c
	return c, nil
}

// CloseAll disconnects all pooled MQTT clients.
func (p *Pool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for addr, c := range p.clients {
		if c.IsConnected() {
			c.Disconnect(1000)
			klog.Infof("target MQTT disconnected: %s", addr)
		}
	}
}
