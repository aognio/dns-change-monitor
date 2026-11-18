// Package record models the DNS names this monitor watches, the observations
// taken of them and the ports used to reach DNS servers and persist them.
package record

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
)

// ErrNoRecords reports that a name carries no record of the requested type,
// or that it does not exist at all. It is an observation rather than a
// failure: a record that appears later is a change worth reporting.
var ErrNoRecords = errors.New("no record")

// Type is a DNS record type the monitor is able to resolve.
type Type string

// The record types the monitor understands.
const (
	TypeA     Type = "A"
	TypeAAAA  Type = "AAAA"
	TypeCNAME Type = "CNAME"
	TypeTXT   Type = "TXT"
	TypeNS    Type = "NS"
	TypeMX    Type = "MX"
	TypePTR   Type = "PTR"
	TypeSOA   Type = "SOA"
)

// SupportedTypes lists every record type the monitor can resolve.
func SupportedTypes() []Type {
	return []Type{TypeA, TypeAAAA, TypeCNAME, TypeTXT, TypeNS, TypeMX, TypePTR, TypeSOA}
}

// ParseType validates and normalises a record type coming from configuration
// or the command line.
func ParseType(raw string) (Type, error) {
	normalised := Type(strings.ToUpper(strings.TrimSpace(raw)))
	for _, supported := range SupportedTypes() {
		if normalised == supported {
			return supported, nil
		}
	}
	return "", fmt.Errorf("%w: unsupported record type %q", shared.ErrInvalid, raw)
}

// Target is the description of a DNS name under observation.
type Target struct {
	ID        string
	Name      string
	Type      Type
	Resolver  string
	Enabled   bool
	Source    string
	CreatedAt time.Time
	UpdatedAt time.Time
	// Check is the way the target is checked. The zero value means it is
	// watched for changes only.
	Check Check
}

// Sources describe where a target came from, so that targets declared in the
// configuration file can be reconciled without touching manually added ones.
const (
	SourceConfig = "config"
	SourceManual = "manual"
)

// NewTarget validates the parts of a target and returns it in canonical form.
func NewTarget(name string, recordType Type, resolver string, source string, now time.Time) (Target, error) {
	canonical, err := CanonicalName(name)
	if err != nil {
		return Target{}, err
	}
	if source == "" {
		source = SourceManual
	}
	return Target{
		ID:        "",
		Name:      canonical,
		Type:      recordType,
		Resolver:  strings.TrimSpace(resolver),
		Enabled:   true,
		Source:    source,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// CanonicalName normalises a DNS name to lower case without a trailing dot.
func CanonicalName(name string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(name))
	trimmed = strings.TrimSuffix(trimmed, ".")
	if trimmed == "" {
		return "", fmt.Errorf("%w: record name must not be empty", shared.ErrInvalid)
	}
	if strings.ContainsAny(trimmed, " \t") {
		return "", fmt.Errorf("%w: record name %q contains whitespace", shared.ErrInvalid, name)
	}
	return trimmed, nil
}

// Identity is the natural key of a target: the same name, type and resolver
// always refer to the same monitored record.
func (t Target) Identity() string {
	return strings.Join([]string{t.Name, string(t.Type), t.Resolver}, "|")
}

// FQDN returns the fully qualified form of the target name.
func (t Target) FQDN() string { return t.Name + "." }

// Snapshot is the set of values a target resolved to at a point in time.
type Snapshot struct {
	ID         string
	TargetID   string
	Values     []string
	TTL        time.Duration
	ObservedAt time.Time
	// Satisfied is the verdict of the check at the moment of the
	// observation. It is nil when the target is watched for changes only.
	Satisfied *bool
	// Reason explains the verdict.
	Reason string
}

// Resolution is the outcome of asking a DNS server about a target.
type Resolution struct {
	Values []string
	TTL    time.Duration
}

// DiffValues compares two ordered value sets and returns the values that
// appeared and the values that disappeared.
func DiffValues(previous, current []string) (added []string, removed []string) {
	before := index(previous)
	after := index(current)

	for _, value := range current {
		if _, seen := before[value]; !seen {
			added = append(added, value)
		}
	}
	for _, value := range previous {
		if _, seen := after[value]; !seen {
			removed = append(removed, value)
		}
	}
	return added, removed
}

func index(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// NormaliseValues removes duplicates and empty entries and sorts the result
// so that two identical answers always compare equal.
func NormaliseValues(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := unique[trimmed]; exists {
			continue
		}
		unique[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	sort.Strings(cleaned)
	return cleaned
}
