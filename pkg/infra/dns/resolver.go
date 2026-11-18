// Package dns resolves monitored DNS names against real name servers.
package dns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	mdns "github.com/miekg/dns"
)

// Resolver queries DNS servers for the answer set of a target.
type Resolver struct {
	client          *mdns.Client
	defaultServers  []string
	timeout         time.Duration
	maxCnameHops    int
	systemFallback  bool
	defaultResolver string
	logger          *slog.Logger
}

// Options configure the resolver.
type Options struct {
	// Servers are the DNS servers used when a target names none, as
	// host:port entries.
	Servers []string
	// Timeout bounds a single exchange with a name server.
	Timeout time.Duration
	// Logger receives the diagnostics of the adapter, in particular the
	// record of every query sent to a name server.
	Logger *slog.Logger
}

// New builds a resolver.
func New(options Options) *Resolver {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Resolver{
		client:         &mdns.Client{Timeout: timeout},
		defaultServers: append([]string(nil), options.Servers...),
		timeout:        timeout,
		maxCnameHops:   8,
		logger:         logger,
	}
}

// Resolve implements record.Resolver.
func (r *Resolver) Resolve(ctx context.Context, target record.Target) (record.Resolution, error) {
	server, err := r.serverFor(target)
	if err != nil {
		return record.Resolution{}, err
	}

	queryType, err := dnsType(target.Type)
	if err != nil {
		return record.Resolution{}, err
	}

	name := target.FQDN()
	var values []string
	ttl := time.Duration(0)

	for hop := 0; hop <= r.maxCnameHops; hop++ {
		answers, answerTTL, cname, err := r.exchange(ctx, server, name, queryType)
		if err != nil {
			return record.Resolution{}, err
		}
		values = append(values, answers...)
		if answerTTL > 0 && (ttl == 0 || answerTTL < ttl) {
			ttl = answerTTL
		}
		if cname == "" || len(answers) > 0 {
			break
		}
		name = cname
	}

	normalised := record.NormaliseValues(values)
	if len(normalised) == 0 {
		return record.Resolution{}, fmt.Errorf("%w: the name %s carries no %s record",
			record.ErrNoRecords, target.Name, target.Type)
	}
	return record.Resolution{Values: normalised, TTL: ttl}, nil
}

// exchange runs one query and returns the matching answers, the smallest
// time to live and the canonical name to follow, if any.
func (r *Resolver) exchange(ctx context.Context, server, name string, queryType uint16) ([]string, time.Duration, string, error) {
	message := new(mdns.Msg)
	message.SetQuestion(mdns.Fqdn(name), queryType)
	message.RecursionDesired = true

	typeName := mdns.TypeToString[queryType]
	r.logger.Info("dns query",
		slog.String("name", name),
		slog.String("type", typeName),
		slog.String("server", server))

	started := time.Now()
	response, _, err := r.client.ExchangeContext(ctx, message, server)
	if err != nil {
		r.logger.Warn("dns query failed",
			slog.String("name", name),
			slog.String("type", typeName),
			slog.String("server", server),
			slog.Duration("elapsed", time.Since(started)),
			slog.String("error", err.Error()))
		return nil, 0, "", fmt.Errorf("query %s %s via %s: %w", name, mdns.TypeToString[queryType], server, err)
	}
	switch response.Rcode {
	case mdns.RcodeSuccess:
	case mdns.RcodeNameError:
		return nil, 0, "", fmt.Errorf("%w: the name %s does not exist (NXDOMAIN)",
			record.ErrNoRecords, strings.TrimSuffix(name, "."))
	default:
		return nil, 0, "", fmt.Errorf("query %s via %s: %s", name, server, mdns.RcodeToString[response.Rcode])
	}

	var values []string
	var ttl time.Duration
	canonical := ""

	for _, answer := range response.Answer {
		header := answer.Header()
		if header.Ttl > 0 && (ttl == 0 || time.Duration(header.Ttl)*time.Second < ttl) {
			ttl = time.Duration(header.Ttl) * time.Second
		}
		switch record := answer.(type) {
		case *mdns.CNAME:
			if canonical == "" {
				canonical = record.Target
			}
		default:
			if header.Rrtype != queryType {
				continue
			}
			values = append(values, formatAnswer(answer))
		}
	}

	r.logger.Info("dns answer",
		slog.String("name", name),
		slog.String("type", typeName),
		slog.String("server", server),
		slog.Int("answers", len(values)),
		slog.Duration("elapsed", time.Since(started)))

	return values, ttl, canonical, nil
}

// serverFor picks the name server to query for a target.
func (r *Resolver) serverFor(target record.Target) (string, error) {
	if target.Resolver != "" {
		return normaliseServer(target.Resolver), nil
	}
	if len(r.defaultServers) > 0 {
		return normaliseServer(r.defaultServers[0]), nil
	}
	return "", fmt.Errorf("no DNS server configured for %s", target.Name)
}

// normaliseServer makes sure a server address carries a port.
func normaliseServer(server string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		return server
	}
	if _, _, err := net.SplitHostPort(server); err == nil {
		return server
	}
	return net.JoinHostPort(server, "53")
}

// dnsType maps a domain record type to its wire representation.
func dnsType(recordType record.Type) (uint16, error) {
	switch recordType {
	case record.TypeA:
		return mdns.TypeA, nil
	case record.TypeAAAA:
		return mdns.TypeAAAA, nil
	case record.TypeCNAME:
		return mdns.TypeCNAME, nil
	case record.TypeTXT:
		return mdns.TypeTXT, nil
	case record.TypeNS:
		return mdns.TypeNS, nil
	case record.TypeMX:
		return mdns.TypeMX, nil
	case record.TypePTR:
		return mdns.TypePTR, nil
	case record.TypeSOA:
		return mdns.TypeSOA, nil
	default:
		return 0, fmt.Errorf("unsupported record type %q", recordType)
	}
}

// formatAnswer renders a DNS answer as the value stored in a snapshot.
func formatAnswer(answer mdns.RR) string {
	switch typed := answer.(type) {
	case *mdns.A:
		return typed.A.String()
	case *mdns.AAAA:
		return typed.AAAA.String()
	case *mdns.CNAME:
		return typed.Target
	case *mdns.TXT:
		return strings.Join(typed.Txt, " ")
	case *mdns.NS:
		return typed.Ns
	case *mdns.MX:
		return strconv.Itoa(int(typed.Preference)) + " " + typed.Mx
	case *mdns.PTR:
		return typed.Ptr
	case *mdns.SOA:
		return strings.Join([]string{
			typed.Ns,
			typed.Mbox,
			strconv.FormatUint(uint64(typed.Serial), 10),
			strconv.FormatUint(uint64(typed.Refresh), 10),
			strconv.FormatUint(uint64(typed.Retry), 10),
			strconv.FormatUint(uint64(typed.Expire), 10),
			strconv.FormatUint(uint64(typed.Minttl), 10),
		}, " ")
	default:
		return answer.String()
	}
}
