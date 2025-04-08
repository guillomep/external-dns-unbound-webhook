package unbound

import (
	"context"
	"github.com/codingconcepts/env"
	unboundlib "github.com/guillomep/go-unbound"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"testing"
)

// Compile time check for interface conformance
var _ unboundlib.Client = &mockClient{}

type mockClient struct {
	records []unboundlib.RR
}

func (m *mockClient) LocalData() []unboundlib.RR {
	return m.records
}

func (m *mockClient) AddLocalData(rr unboundlib.RR) error {
	m.records = append(m.records, rr)
	return nil
}

func (m *mockClient) RemoveLocalData(rr unboundlib.RR) error {
	for i, r := range m.records {
		if r == rr {
			m.records = append(m.records[:i], m.records[i+1:]...)
		}
	}
	return nil
}

func TestConfigurationDefault(t *testing.T) {
	config := Configuration{}
	t.Setenv("UNBOUND_HOST", "testhost")
	if err := env.Set(&config); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, config.Host, "testhost")
	assert.Empty(t, config.CaPemPath)
	assert.Empty(t, config.KeyPemPath)
	assert.Empty(t, config.CertPemPath)
	assert.False(t, config.DryRun)
	assert.Equal(t, 300, config.DefaultTTL)
	assert.Empty(t, config.DomainFilter)
	assert.Empty(t, config.ExcludeDomains)
	assert.Empty(t, config.RegexDomainFilter)
	assert.Empty(t, config.RegexDomainExclusion)
}

func TestConfigurationHostRequired(t *testing.T) {
	config := Configuration{}
	if err := env.Set(&config); err == nil {
		t.Fatal("Should failed because host is required")
	}
}

func TestNewProvider(t *testing.T) {
	p, err := NewProvider(&Configuration{CaPemPath: "./notexist", KeyPemPath: "./notexist", CertPemPath: "./notexist"})
	assert.Nil(t, p)
	assert.NotNil(t, err)

	p, err = NewProvider(&Configuration{Host: "testing", DryRun: true})
	assert.NotNil(t, p)
	assert.Nil(t, err)
	assert.NotNil(t, p.client)
	assert.True(t, p.dryRun)
	assert.NotNil(t, p.domainFilter)
}

func TestRecords(t *testing.T) {
	tests := []struct {
		name     string
		records  []unboundlib.RR
		expected []*endpoint.Endpoint
		config   Configuration
	}{
		{
			name:     "no record",
			records:  []unboundlib.RR{},
			expected: []*endpoint.Endpoint{},
			config:   Configuration{},
		},
		{
			name: "with records",
			records: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
				endpoint.NewEndpointWithTTL("a.example.com", "CNAME", endpoint.TTL(3600), "abc.def"),
			},
			config: Configuration{},
		},
		{
			name: "with records with domain filter",
			records: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("a.example.com", "CNAME", endpoint.TTL(3600), "abc.def"),
			},
			config: Configuration{
				DomainFilter: []string{"example.com"},
			},
		},
		{
			name: "with records with domain exclude",
			records: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
			},
			config: Configuration{
				ExcludeDomains: []string{"example.com"},
			},
		},
		{
			name: "with records with regex filter",
			records: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("a.example.com", "CNAME", endpoint.TTL(3600), "abc.def"),
			},
			config: Configuration{
				RegexDomainFilter: ".*.com",
			},
		},
		{
			name: "with records with regex exclude",
			records: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
				{Name: "a.test.lan", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []*endpoint.Endpoint{
				endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
			},
			config: Configuration{
				RegexDomainFilter:    ".*.lan",
				RegexDomainExclusion: "^a.*",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &UnboundProvider{
				client:       &mockClient{records: tt.records},
				domainFilter: GetDomainFilter(tt.config),
			}

			result, err := p.Records(context.TODO())
			assert.Nil(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyChange(t *testing.T) {
	tests := []struct {
		name     string
		records  []unboundlib.RR
		expected []unboundlib.RR
		changes  plan.Changes
	}{
		{
			name: "no changes",
			records: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
			},
			expected: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
			},
			changes: plan.Changes{},
		},
		{
			name:    "no record before",
			records: []unboundlib.RR{},
			expected: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
			},
			changes: plan.Changes{
				Create: []*endpoint.Endpoint{
					endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
				},
			},
		},
		{
			name: "record before",
			records: []unboundlib.RR{
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []unboundlib.RR{
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
			},
			changes: plan.Changes{
				Create: []*endpoint.Endpoint{
					endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
				},
			},
		},
		{
			name: "record no ttl",
			records: []unboundlib.RR{
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []unboundlib.RR{
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
				{Name: "test.lan", TTL: 7200, Type: "A", Value: "192.168.1.1"},
			},
			changes: plan.Changes{
				Create: []*endpoint.Endpoint{
					endpoint.NewEndpoint("test.lan", "A", "192.168.1.1"),
				},
			},
		},
		{
			name: "record delete existing",
			records: []unboundlib.RR{
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
			},
			expected: []unboundlib.RR{
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			changes: plan.Changes{
				Delete: []*endpoint.Endpoint{
					endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
				},
			},
		},
		{
			name: "record delete not exist",
			records: []unboundlib.RR{
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []unboundlib.RR{
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			changes: plan.Changes{
				Delete: []*endpoint.Endpoint{
					endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
				},
			},
		},
		{
			name: "record update existing",
			records: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []unboundlib.RR{
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.2"},
			},
			changes: plan.Changes{
				UpdateOld: []*endpoint.Endpoint{
					endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
				},
				UpdateNew: []*endpoint.Endpoint{
					endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.2"),
				},
			},
		},
		{
			name: "record update not exist",
			records: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
			},
			expected: []unboundlib.RR{
				{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
				{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
				{Name: "a.test.lan", TTL: 300, Type: "A", Value: "192.168.1.2"},
			},
			changes: plan.Changes{
				UpdateOld: []*endpoint.Endpoint{
					endpoint.NewEndpointWithTTL("a.test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
				},
				UpdateNew: []*endpoint.Endpoint{
					endpoint.NewEndpointWithTTL("a.test.lan", "A", endpoint.TTL(300), "192.168.1.2"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mockClient{records: tt.records}
			p := &UnboundProvider{
				client: &m,
				defaultTTL: 7200,
			}

			err := p.ApplyChanges(context.TODO(), &tt.changes)
			assert.Nil(t, err)
			assert.Equal(t, tt.expected, m.records)
		})
	}
}

func TestApplyChangesDryRun(t *testing.T) {
	expected := []unboundlib.RR{
		{Name: "test.lan", TTL: 300, Type: "A", Value: "192.168.1.1"},
		{Name: "a.example.com", TTL: 3600, Type: "CNAME", Value: "abc.def"},
	}

	m := mockClient{records: expected}
	p := &UnboundProvider{
		client: &m,
		dryRun: true,
	}

	changes := plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("a.test.lan", "A", endpoint.TTL(300), "192.168.1.2"),
		},
		Delete: []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("a.example.com", "CNAME", endpoint.TTL(3600), "abc.def"),
		},
		UpdateOld: []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("test.lan", "A", endpoint.TTL(300), "192.168.1.2"),
		},
	}

	err := p.ApplyChanges(context.TODO(), &changes)
	assert.Nil(t, err)
	assert.Equal(t, expected, m.records)
}

func TestAdjustEndpoints(t *testing.T) {
	input := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("a.test.lan", "A", endpoint.TTL(300), "192.168.1.1"),
		endpoint.NewEndpointWithTTL("b.test.lan.", "A", endpoint.TTL(300), "192.168.1.2"),
	}
	expected := []*endpoint.Endpoint{
		{DNSName: "a.test.lan.", RecordType: "A", RecordTTL: endpoint.TTL(300), Targets: []string{"192.168.1.1"}, Labels: endpoint.Labels{}},
		{DNSName: "b.test.lan.", RecordType: "A", RecordTTL: endpoint.TTL(300), Targets: []string{"192.168.1.2"}, Labels: endpoint.Labels{}},
	}
	p := &UnboundProvider{}

	result, err := p.AdjustEndpoints(input)
	assert.Nil(t, err)
	assert.Equal(t, expected, result)
}
