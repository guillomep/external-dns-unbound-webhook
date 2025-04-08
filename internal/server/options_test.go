package server

import (
	"github.com/codingconcepts/env"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	options := &ServerOptions{}
	if err := env.Set(options); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "localhost", options.WebhookHost)
	assert.Equal(t, uint16(8888), options.WebhookPort)
	assert.Equal(t, "localhost:8888", options.GetWebhookAddress())

	assert.Equal(t, "0.0.0.0", options.HealthHost)
	assert.Equal(t, uint16(8080), options.HealthPort)
	assert.Equal(t, "0.0.0.0:8080", options.GetHealthAddress())

	assert.Equal(t, 60000, options.ReadTimeout, 60000)
	assert.Equal(t, 60000*time.Millisecond, options.GetReadTimeout())
	assert.Equal(t, 60000, options.WriteTimeout, 60000)
	assert.Equal(t, 60000*time.Millisecond, options.GetWriteTimeout())
}

func TestSetting(t *testing.T) {
	options := &ServerOptions{
		WebhookHost:  "webhookhost",
		WebhookPort:  1234,
		HealthHost:   "healthhost",
		HealthPort:   5678,
		ReadTimeout:  1011,
		WriteTimeout: 1213,
	}

	assert.Equal(t, "webhookhost:1234", options.GetWebhookAddress())
	assert.Equal(t, "healthhost:5678", options.GetHealthAddress())
	assert.Equal(t, 1011*time.Millisecond, options.GetReadTimeout())
	assert.Equal(t, 1213*time.Millisecond, options.GetWriteTimeout())
}
