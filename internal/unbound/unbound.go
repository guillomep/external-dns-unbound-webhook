package unbound

import (
	"context"
	"fmt"
	unboundlib "github.com/guillomep/go-unbound"
	log "github.com/sirupsen/logrus"
	"regexp"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
	"strings"
)

const (
	actionCreate = "CREATE"
	actionRemove = "REMOVE"
)

type UnboundProvider struct {
	provider.BaseProvider
	client unboundlib.Client

	domainFilter endpoint.DomainFilter
	dryRun       bool
	defaultTTL   int
}

type UnboundChange struct {
	Action string
	RR     *unboundlib.RR
}

// Configuration contains the Unbound provider's configuration.
type Configuration struct {
	Host                 string   `env:"UNBOUND_HOST" required:"true"`
	CaPemPath            string   `env:"UNBOUND_CA_PEM_PATH" default:""`
	KeyPemPath           string   `env:"UNBOUND_KEY_PEM_PATH" default:""`
	CertPemPath          string   `env:"UNBOUND_CERT_PEM_PATH" default:""`
	DryRun               bool     `env:"DRY_RUN" default:"false"`
	DefaultTTL           int      `env:"DEFAULT_TTL" default:"300"`
	DomainFilter         []string `env:"DOMAIN_FILTER" default:""`
	ExcludeDomains       []string `env:"EXCLUDE_DOMAIN_FILTER" default:""`
	RegexDomainFilter    string   `env:"REGEXP_DOMAIN_FILTER" default:""`
	RegexDomainExclusion string   `env:"REGEXP_DOMAIN_FILTER_EXCLUSION" default:""`
}

func NewProvider(config *Configuration) (*UnboundProvider, error) {
	unboundClient, err := unboundlib.NewClient(config.Host,
		unboundlib.WithServerCertificatesFile(config.CaPemPath),
		unboundlib.WithControlCertificatesFile(config.KeyPemPath),
		unboundlib.WithControlPrivateKeyFile(config.CertPemPath))
	if err != nil {
		return nil, err
	}

	return &UnboundProvider{
		client:       unboundClient,
		dryRun:       config.DryRun,
		defaultTTL:   config.DefaultTTL,
		domainFilter: GetDomainFilter(*config),
	}, nil
}

// Records returns the list of records.
func (p *UnboundProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	endpoints := []*endpoint.Endpoint{}

	records := p.client.LocalData()

	for _, r := range records {
		if provider.SupportedRecordType(r.Type) {
			if !p.domainFilter.Match(r.Name) {
				continue
			}

			endpoints = append(endpoints, endpoint.NewEndpointWithTTL(r.Name, r.Type, endpoint.TTL(r.TTL), r.Value))
		}
	}

	return endpoints, nil
}

func (p *UnboundProvider) submitChanges(changes []*UnboundChange) error {
	if len(changes) == 0 {
		log.Infof("All records are already up to date")
		return nil
	}

	for _, change := range changes {
		log.WithFields(log.Fields{
			"record": change.RR.Name,
			"type":   change.RR.Type,
			"ttl":    change.RR.TTL,
			"action": change.Action,
		}).Info("Changing record.")

		if p.dryRun {
			continue
		}

		switch change.Action {
		case actionCreate:
			if err := p.client.AddLocalData(*change.RR); err != nil {
				return err
			}
		case actionRemove:
			if err := p.client.RemoveLocalData(*change.RR); err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *UnboundProvider) newUnboundChange(action string, endpoints []*endpoint.Endpoint) []*UnboundChange {
	changes := make([]*UnboundChange, 0, len(endpoints))
	for _, e := range endpoints {
		var ttl int
		if e.RecordTTL.IsConfigured() {
			ttl = int(e.RecordTTL)
		} else {
			ttl = p.defaultTTL
		}

		for _, t := range e.Targets {
			change := &UnboundChange{
				Action: action,
				RR: &unboundlib.RR{
					Name:  e.DNSName,
					TTL:   ttl,
					Type:  e.RecordType,
					Value: t,
				},
			}

			changes = append(changes, change)
		}
	}
	return changes
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *UnboundProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	combinedChanges := make([]*UnboundChange, 0, len(changes.Create)+len(changes.UpdateNew)+len(changes.Delete))

	combinedChanges = append(combinedChanges, p.newUnboundChange(actionCreate, changes.Create)...)
	combinedChanges = append(combinedChanges, p.newUnboundChange(actionRemove, changes.UpdateOld)...)
	combinedChanges = append(combinedChanges, p.newUnboundChange(actionCreate, changes.UpdateNew)...)
	combinedChanges = append(combinedChanges, p.newUnboundChange(actionRemove, changes.Delete)...)

	return p.submitChanges(combinedChanges)
}

func (p *UnboundProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	adjustedEndpoints := []*endpoint.Endpoint{}

	for _, ep := range endpoints {
		if !strings.HasSuffix(ep.DNSName, ".") {
			ep.DNSName = ep.DNSName + "."
		}
		adjustedEndpoints = append(adjustedEndpoints, ep)
	}

	return adjustedEndpoints, nil
}

func GetDomainFilter(config Configuration) endpoint.DomainFilter {
	var domainFilter endpoint.DomainFilter
	createMsg := "Creating Unbound provider with "

	if config.RegexDomainFilter != "" {
		createMsg += fmt.Sprintf("Regexp domain filter: '%s', ", config.RegexDomainFilter)
		if config.RegexDomainExclusion != "" {
			createMsg += fmt.Sprintf("with exclusion: '%s', ", config.RegexDomainExclusion)
		}
		domainFilter = endpoint.NewRegexDomainFilter(
			regexp.MustCompile(config.RegexDomainFilter),
			regexp.MustCompile(config.RegexDomainExclusion),
		)
	} else {
		if len(config.DomainFilter) > 0 {
			createMsg += fmt.Sprintf("Domain filter: '%s', ", strings.Join(config.DomainFilter, ","))
		}
		if len(config.ExcludeDomains) > 0 {
			createMsg += fmt.Sprintf("Exclude domain filter: '%s', ", strings.Join(config.ExcludeDomains, ","))
		}
		domainFilter = endpoint.NewDomainFilterWithExclusions(config.DomainFilter, config.ExcludeDomains)
	}

	createMsg = strings.TrimSuffix(createMsg, ", ")
	if strings.HasSuffix(createMsg, "with ") {
		createMsg += "no kind of domain filters"
	}
	log.Info(createMsg)
	return domainFilter
}
