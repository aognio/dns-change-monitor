package record

import (
	"testing"

	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
)

func TestDiffValuesReportsAdditionsAndRemovals(t *testing.T) {
	added, removed := DiffValues([]string{"1.1.1.1", "2.2.2.2"}, []string{"2.2.2.2", "3.3.3.3"})

	if len(added) != 1 || added[0] != "3.3.3.3" {
		t.Fatalf("unexpected additions: %v", added)
	}
	if len(removed) != 1 || removed[0] != "1.1.1.1" {
		t.Fatalf("unexpected removals: %v", removed)
	}
}

func TestDiffValuesReportsNoDifference(t *testing.T) {
	added, removed := DiffValues([]string{"1.1.1.1"}, []string{"1.1.1.1"})
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("expected no difference, got added=%v removed=%v", added, removed)
	}
}

func TestNormaliseValuesRemovesDuplicatesAndSorts(t *testing.T) {
	values := NormaliseValues([]string{"2.2.2.2", "1.1.1.1", "2.2.2.2", "  "})

	if len(values) != 2 {
		t.Fatalf("expected two values, got %v", values)
	}
	if values[0] != "1.1.1.1" || values[1] != "2.2.2.2" {
		t.Fatalf("unexpected order: %v", values)
	}
}

func TestCanonicalName(t *testing.T) {
	name, err := CanonicalName(" Example.COM. ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "example.com" {
		t.Fatalf("unexpected canonical name: %q", name)
	}
}

func TestCanonicalNameRejectsEmptyName(t *testing.T) {
	if _, err := CanonicalName("  "); err == nil {
		t.Fatal("expected an error for an empty name")
	}
}

func TestParseType(t *testing.T) {
	recordType, err := ParseType("a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recordType != TypeA {
		t.Fatalf("unexpected type: %q", recordType)
	}
	if _, err := ParseType("WAT"); err == nil {
		t.Fatal("expected an error for an unknown type")
	}
}

func TestTargetIdentitySeparatesResolvers(t *testing.T) {
	now := shared.SystemClock{}.Now()
	first, err := NewTarget("example.com", TypeA, "1.1.1.1:53", SourceConfig, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := NewTarget("example.com", TypeA, "8.8.8.8:53", SourceConfig, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.Identity() == second.Identity() {
		t.Fatal("targets using different resolvers must have different identities")
	}
}
