package unbound

import (
	"context"
	"fmt"
	unboundlib "github.com/guillomep/go-unbound"
	log "github.com/sirupsen/logrus"
	"net"
	"net/url"
	"os"
	"regexp"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
	"strings"
	"time"
)

const (
	actionCreate = "CREATE"
	actionRemove = "REMOVE"

	// probeTimeout bounds the TCP check run after a failed call. It is kept
	// short so the error still reaches external-dns before its own timeout.
	probeTimeout = 1 * time.Second
)

type UnboundProvider struct {
	provider.BaseProvider
	client unboundlib.Client
	host   string
	// timeout bounds every call to Unbound; zero means no limit.
	timeout time.Duration
	// probe checks that the Unbound address accepts TCP connections. It is
	// nil in tests.
	probe func() error

	domainFilter *endpoint.DomainFilter
	dryRun       bool
	defaultTTL   int
}

type UnboundChange struct {
	Action string
	RR     *unboundlib.RR
}

// Configuration contains the Unbound provider's configuration.
type Configuration struct {
	Host                 string        `env:"UNBOUND_HOST" required:"true"`
	CaPemPath            string        `env:"UNBOUND_CA_PEM_PATH" default:""`
	KeyPemPath           string        `env:"UNBOUND_KEY_PEM_PATH" default:""`
	CertPemPath          string        `env:"UNBOUND_CERT_PEM_PATH" default:""`
	Timeout              time.Duration `env:"UNBOUND_TIMEOUT" default:"3s"`
	DryRun               bool          `env:"DRY_RUN" default:"false"`
	DefaultTTL           int           `env:"DEFAULT_TTL" default:"300"`
	DomainFilter         []string      `env:"DOMAIN_FILTER" default:""`
	ExcludeDomains       []string      `env:"EXCLUDE_DOMAIN_FILTER" default:""`
	RegexDomainFilter    string        `env:"REGEXP_DOMAIN_FILTER" default:""`
	RegexDomainExclusion string        `env:"REGEXP_DOMAIN_FILTER_EXCLUSION" default:""`
}

func NewProvider(config *Configuration) (*UnboundProvider, error) {
	logConnectionSettings(config)

	unboundClient, err := unboundlib.NewClient(config.Host,
		unboundlib.WithServerCertificatesFile(config.CaPemPath),
		unboundlib.WithControlPrivateKeyFile(config.KeyPemPath),
		unboundlib.WithControlCertificatesFile(config.CertPemPath))
	if err != nil {
		// The library's error does not always name the right file, so add the
		// configured paths.
		return nil, fmt.Errorf("could not create Unbound client for %q (ca: %q, cert: %q, key: %q): %w",
			config.Host, config.CaPemPath, config.CertPemPath, config.KeyPemPath, err)
	}

	if err := checkReachable(config.Host, probeTimeout); err != nil {
		log.Warnf("Unbound control address is not reachable yet: %v", err)
	} else {
		log.Infof("Unbound control address %s is reachable (TLS not checked)", config.Host)
	}

	return &UnboundProvider{
		client:  unboundClient,
		host:    config.Host,
		timeout: config.Timeout,
		probe: func() error {
			return checkReachable(config.Host, probeTimeout)
		},
		dryRun:       config.DryRun,
		defaultTTL:   config.DefaultTTL,
		domainFilter: GetDomainFilter(*config),
	}, nil
}

// Records returns the list of records.
func (p *UnboundProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	endpoints := []*endpoint.Endpoint{}

	var records []unboundlib.RR
	err := p.call(ctx, "list_local_data", func() error {
		records = p.client.LocalData()
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(records) == 0 && p.probe != nil {
		// The client library swallows connection and TLS errors and returns no
		// records. Report an unreachable server as an error rather than as an
		// empty zone.
		if err := p.probe(); err != nil {
			return nil, fmt.Errorf("no local data returned by Unbound: %w", err)
		}
		log.Warnf("No local data returned by Unbound at %s; the address is reachable, so either there is no local data or the TLS handshake failed", p.host)
	} else {
		log.Debugf("Fetched %d local data records from Unbound at %s", len(records), p.host)
	}

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

// call runs fn, which talks to Unbound, within p.timeout. go-unbound sets no
// deadlines on its connections, so without this an unreachable server blocks
// the request for as long as the kernel retries the TCP connect, well past the
// external-dns webhook timeout. On timeout fn is left running in the
// background until the library gives up.
func (p *UnboundProvider) call(ctx context.Context, command string, fn func() error) error {
	log.Debugf("Sending %s to Unbound at %s", command, p.host)
	start := time.Now()

	if p.timeout <= 0 {
		err := fn()
		log.Debugf("Unbound %s finished in %s", command, time.Since(start))
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- fn() }()

	select {
	case err := <-done:
		log.Debugf("Unbound %s finished in %s", command, time.Since(start))
		return err
	case <-ctx.Done():
		reason := "the address is reachable, so the TLS handshake or the response stalled"
		if p.probe != nil {
			if err := p.probe(); err != nil {
				reason = err.Error()
			}
		}
		return fmt.Errorf("unbound %s at %s got no answer within %s: %s", command, p.host, p.timeout, reason)
	}
}

func (p *UnboundProvider) submitChanges(ctx context.Context, changes []*UnboundChange) error {
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

		rr := *change.RR
		switch change.Action {
		case actionCreate:
			if err := p.call(ctx, "local_data", func() error { return p.client.AddLocalData(rr) }); err != nil {
				return err
			}
		case actionRemove:
			if err := p.call(ctx, "local_data_remove", func() error { return p.client.RemoveLocalData(rr) }); err != nil {
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

	return p.submitChanges(ctx, combinedChanges)
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

// logConnectionSettings logs where the client will connect and which
// certificate files it will load.
func logConnectionSettings(config *Configuration) {
	scheme, address := "", config.Host
	if u, err := url.Parse(config.Host); err != nil {
		log.Warnf("Could not parse UNBOUND_HOST %q: %v", config.Host, err)
	} else {
		scheme, address = u.Scheme, u.Host
		if u.Scheme == "unix" {
			address = u.Path
		}
	}

	// Mirrors go-unbound: TLS is used as soon as a CA or a client certificate
	// is configured.
	tlsEnabled := config.CaPemPath != "" || config.CertPemPath != ""

	log.WithFields(log.Fields{
		"host":    config.Host,
		"network": scheme,
		"address": address,
		"tls":     tlsEnabled,
	}).Info("Unbound control connection settings.")

	logPemFile("UNBOUND_CA_PEM_PATH", config.CaPemPath)
	logPemFile("UNBOUND_CERT_PEM_PATH", config.CertPemPath)
	logPemFile("UNBOUND_KEY_PEM_PATH", config.KeyPemPath)

	if !tlsEnabled && config.KeyPemPath != "" {
		log.Warn("UNBOUND_KEY_PEM_PATH is set but neither UNBOUND_CA_PEM_PATH nor UNBOUND_CERT_PEM_PATH is, so TLS is disabled and the key is ignored")
	}
}

// logPemFile logs a configured PEM path and whether the process can read it.
func logPemFile(envName, path string) {
	if path == "" {
		log.Infof("%s is not set", envName)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		log.Warnf("%s=%s cannot be opened (uid %d, gid %d): %v", envName, path, os.Getuid(), os.Getgid(), err)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		log.Warnf("%s=%s cannot be stat'd: %v", envName, path, err)
		return
	}
	log.Infof("%s=%s (readable, %d bytes, mode %s)", envName, path, info.Size(), info.Mode())
}

// checkReachable checks that the Unbound control address accepts a network
// connection within timeout. It does not check TLS.
func checkReachable(host string, timeout time.Duration) error {
	u, err := url.Parse(host)
	if err != nil {
		return err
	}

	address := u.Host
	if u.Scheme == "unix" {
		address = u.Path
	}

	conn, err := net.DialTimeout(u.Scheme, address, timeout)
	if err != nil {
		return fmt.Errorf("%s://%s is not reachable: %w", u.Scheme, address, err)
	}
	return conn.Close()
}

func GetDomainFilter(config Configuration) *endpoint.DomainFilter {
	var domainFilter *endpoint.DomainFilter
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
