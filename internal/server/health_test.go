package server

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"net"
	"net/http"
	"testing"
)

func getFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

func TestHealthy(t *testing.T) {
	tests := []struct {
		name     string
		input    *HealthStatus
		change   bool
		expected *HealthStatus
	}{
		{
			name:     "healthy false -> true",
			input:    &HealthStatus{healthy: false},
			change:   true,
			expected: &HealthStatus{healthy: true},
		},
		{
			name:     "healthy true -> false",
			input:    &HealthStatus{healthy: true},
			change:   false,
			expected: &HealthStatus{healthy: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.SetHealth(tt.change)
			assert.Equal(t, tt.expected, tt.input)
			assert.Equal(t, tt.change, tt.input.IsHealthy())
		})
	}
}

func TestReady(t *testing.T) {
	tests := []struct {
		name     string
		input    *HealthStatus
		change   bool
		expected *HealthStatus
	}{
		{
			name:     "ready false -> true",
			input:    &HealthStatus{ready: false},
			change:   true,
			expected: &HealthStatus{ready: true},
		},
		{
			name:     "ready true -> false",
			input:    &HealthStatus{ready: true},
			change:   false,
			expected: &HealthStatus{ready: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.SetReady(tt.change)
			assert.Equal(t, tt.expected, tt.input)
			assert.Equal(t, tt.change, tt.input.IsReady())
		})
	}
}

func TestHealthServer(t *testing.T) {
	srv := &HealthServer{}
	status := &HealthStatus{}
	startedChan := make(chan struct{}, 1)

	port, err := getFreePort()
	if err != nil {
		t.Fatal("Cannot find free port for test")
	}

	options := ServerOptions{
		HealthHost:   "127.0.0.1",
		HealthPort:   uint16(port),
		ReadTimeout:  60000,
		WriteTimeout: 60000,
	}

	go srv.Start(status, startedChan, options)
	<-startedChan

	if status.IsReady() || status.IsHealthy() {
		t.Fatal("The server should not be ready or healthy until started")
	}

	url := fmt.Sprintf("http://%s", options.GetHealthAddress())

	res, err := http.Get(url)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	res, err = http.Get(url + "/ready")
	assert.Nil(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	res, err = http.Get(url + "/health")
	assert.Nil(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)

	status.SetReady(true)
	res, err = http.Get(url)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	res, err = http.Get(url + "/ready")
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	res, err = http.Get(url + "/health")
	assert.Nil(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)

	status.SetReady(false)
	status.SetHealth(true)

	res, err = http.Get(url)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	res, err = http.Get(url + "/ready")
	assert.Nil(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	res, err = http.Get(url + "/health")
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
}
