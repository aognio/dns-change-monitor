package dns_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gnrfan/dns-change-monitor/pkg/domain/record"
	"github.com/gnrfan/dns-change-monitor/pkg/infra/dns"
	mdns "github.com/miekg/dns"
)

// startServer runs a name server answering with the given records and returns
// its address.
func startServer(t *testing.T, answers map[string]string) string {
	t.Helper()

	handler := mdns.HandlerFunc(func(w mdns.ResponseWriter, request *mdns.Msg) {
		response := new(mdns.Msg)
		response.SetReply(request)
		for _, question := range request.Question {
			answer, ok := answers[question.Name]
			if !ok {
				continue
			}
			record, err := mdns.NewRR(answer)
			if err != nil {
				continue
			}
			response.Answer = append(response.Answer, record)
		}
		_ = w.WriteMsg(response)
	})

	connection, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	server := &mdns.Server{PacketConn: connection, Handler: handler}
	started := make(chan struct{})
	server.NotifyStartedFunc = func() { close(started) }
	go func() { _ = server.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = server.Shutdown() })

	return connection.LocalAddr().String()
}

func TestResolverReturnsTheAddressOfARecord(t *testing.T) {
	address := startServer(t, map[string]string{
		"example.com.": "example.com. 60 IN A 203.0.113.10",
	})

	resolver := dns.New(dns.Options{Servers: []string{address}, Timeout: 2 * time.Second})
	target, err := record.NewTarget("example.com", record.TypeA, "", record.SourceManual, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Values) != 1 || resolution.Values[0] != "203.0.113.10" {
		t.Fatalf("unexpected values: %v", resolution.Values)
	}
	if resolution.TTL != 60*time.Second {
		t.Fatalf("unexpected time to live: %s", resolution.TTL)
	}
}

func TestResolverFollowsTheCanonicalName(t *testing.T) {
	address := startServer(t, map[string]string{
		"www.example.com.": "www.example.com. 30 IN CNAME example.com.",
		"example.com.":     "example.com. 30 IN A 203.0.113.20",
	})

	resolver := dns.New(dns.Options{Servers: []string{address}, Timeout: 2 * time.Second})
	target, err := record.NewTarget("www.example.com", record.TypeA, "", record.SourceManual, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Values) != 1 || resolution.Values[0] != "203.0.113.20" {
		t.Fatalf("unexpected values: %v", resolution.Values)
	}
}

func TestResolverReportsAMissingRecord(t *testing.T) {
	address := startServer(t, map[string]string{})

	resolver := dns.New(dns.Options{Servers: []string{address}, Timeout: 2 * time.Second})
	target, err := record.NewTarget("missing.example.com", record.TypeA, "", record.SourceManual, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := resolver.Resolve(context.Background(), target); err == nil {
		t.Fatal("expected an error for a missing record")
	}
}

func TestResolverUsesTheResolverOfTheTarget(t *testing.T) {
	address := startServer(t, map[string]string{
		"example.com.": "example.com. 60 IN A 198.51.100.5",
	})

	resolver := dns.New(dns.Options{Timeout: 2 * time.Second})
	target, err := record.NewTarget("example.com", record.TypeA, address, record.SourceManual, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Values) != 1 || resolution.Values[0] != "198.51.100.5" {
		t.Fatalf("unexpected values: %v", resolution.Values)
	}
}
