package target

import (
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"k8s.io/klog/v2"
)

// MQTTTarget publishes processed payloads to an MQTT broker topic.
type MQTTTarget struct {
	client mqtt.Client
	topic  string
	qos    byte
}

func (t *MQTTTarget) Send(payload []byte) error {
	token := t.client.Publish(t.topic, t.qos, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish timeout to topic %s", t.topic)
	}
	if token.Error() != nil {
		return fmt.Errorf("publish to %s: %w", t.topic, token.Error())
	}
	klog.V(5).Infof("MQTT target: published %d bytes to %s", len(payload), t.topic)
	return nil
}

func (t *MQTTTarget) Close() error {
	// Connection lifecycle is managed by Pool; nothing to close here.
	return nil
}
