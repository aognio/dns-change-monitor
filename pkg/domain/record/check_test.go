package record

import (
	"testing"
)

func TestParseModeAcceptsTheKnownModes(t *testing.T) {
	cases := []struct {
		raw  string
		want Mode
	}{
		{"change", ModeChange},
		{"any", ModeAny},
		{"exact", ModeExact},
		{"regex", ModeRegex},
		{"EXACT", ModeExact},
		{" regex ", ModeRegex},
	}

	for _, testCase := range cases {
		t.Run(testCase.raw, func(t *testing.T) {
			mode, err := ParseMode(testCase.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != testCase.want {
				t.Fatalf("unexpected mode for %q: %q", testCase.raw, mode)
			}
		})
	}
}

func TestParseModeDefaultsToChange(t *testing.T) {
	mode, err := ParseMode("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != ModeChange {
		t.Fatalf("unexpected mode: %q", mode)
	}
}

func TestParseModeRejectsAnUnknownMode(t *testing.T) {
	if _, err := ParseMode("sometimes"); err == nil {
		t.Fatal("expected an error for an unknown mode")
	}
}

func TestTheZeroValueMeansChange(t *testing.T) {
	var check Check
	if check.EffectiveMode() != ModeChange {
		t.Fatalf("unexpected mode: %q", check.EffectiveMode())
	}
	if check.IsDeclared() {
		t.Fatal("the zero value declares no expectation")
	}
}

func TestNewCheckRejectsTheInvalidCombinations(t *testing.T) {
	cases := []struct {
		name   string
		mode   Mode
		expect []string
	}{
		{"change with an expectation", ModeChange, []string{"1.2.3.4"}},
		{"any with an expectation", ModeAny, []string{"1.2.3.4"}},
		{"exact without an expectation", ModeExact, nil},
		{"regex without an expectation", ModeRegex, nil},
		{"regex with two expectations", ModeRegex, []string{"a", "b"}},
		{"regex with an invalid pattern", ModeRegex, []string{"([a-z"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewCheck(testCase.mode, testCase.expect); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestNewCheckBuildsTheDeclaredModes(t *testing.T) {
	check, err := NewCheck(ModeExact, []string{"1.2.3.4", " 5.6.7.8 "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if check.EffectiveMode() != ModeExact {
		t.Fatalf("unexpected mode: %q", check.EffectiveMode())
	}
	if !check.IsDeclared() {
		t.Fatal("the exact mode declares an expectation")
	}
	if len(check.Expect) != 2 || check.Expect[1] != "5.6.7.8" {
		t.Fatalf("unexpected expectation: %v", check.Expect)
	}
}

func TestEvaluateInTheChangeMode(t *testing.T) {
	var check Check

	outcome := check.Evaluate([]string{"1.1.1.1"}, []string{"1.1.1.1"})
	if !outcome.Satisfied {
		t.Fatal("an unchanged answer satisfies the change mode")
	}
	if outcome.Reason != "the answer is the same as the last observation" {
		t.Fatalf("unexpected reason: %q", outcome.Reason)
	}
	if len(outcome.Added) != 0 || len(outcome.Removed) != 0 {
		t.Fatalf("unexpected diff: %+v", outcome)
	}

	outcome = check.Evaluate([]string{"2.2.2.2"}, []string{"1.1.1.1"})
	if outcome.Satisfied {
		t.Fatal("a changed answer does not satisfy the change mode")
	}
	if outcome.Reason != "the answer changed" {
		t.Fatalf("unexpected reason: %q", outcome.Reason)
	}
	if len(outcome.Added) != 1 || outcome.Added[0] != "2.2.2.2" {
		t.Fatalf("unexpected additions: %v", outcome.Added)
	}
	if len(outcome.Removed) != 1 || outcome.Removed[0] != "1.1.1.1" {
		t.Fatalf("unexpected removals: %v", outcome.Removed)
	}
}

func TestEvaluateInTheAnyMode(t *testing.T) {
	check, err := NewCheck(ModeAny, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome := check.Evaluate([]string{"1.1.1.1"}, nil)
	if !outcome.Satisfied {
		t.Fatal("a record with an answer satisfies the any mode")
	}
	if outcome.Reason != "the record carries at least one answer" {
		t.Fatalf("unexpected reason: %q", outcome.Reason)
	}

	outcome = check.Evaluate(nil, []string{"1.1.1.1"})
	if outcome.Satisfied {
		t.Fatal("a record without an answer violates the any mode")
	}
	if outcome.Reason != "the record carries no answer" {
		t.Fatalf("unexpected reason: %q", outcome.Reason)
	}
}

func TestEvaluateInTheExactMode(t *testing.T) {
	check, err := NewCheck(ModeExact, []string{"1.2.3.4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome := check.Evaluate([]string{"1.2.3.4"}, nil)
	if !outcome.Satisfied {
		t.Fatal("the expected answer satisfies the exact mode")
	}
	if outcome.Reason != "the answer is exactly the expected set [1.2.3.4]" {
		t.Fatalf("unexpected reason: %q", outcome.Reason)
	}

	outcome = check.Evaluate([]string{"5.6.7.8"}, nil)
	if outcome.Satisfied {
		t.Fatal("an unexpected answer violates the exact mode")
	}
	if outcome.Reason != "expected exactly [1.2.3.4], got [5.6.7.8]" {
		t.Fatalf("unexpected reason: %q", outcome.Reason)
	}
	if len(outcome.Missing) != 1 || outcome.Missing[0] != "1.2.3.4" {
		t.Fatalf("unexpected missing values: %v", outcome.Missing)
	}
	if len(outcome.Unexpected) != 1 || outcome.Unexpected[0] != "5.6.7.8" {
		t.Fatalf("unexpected unexpected values: %v", outcome.Unexpected)
	}
}

func TestEvaluateInTheExactModeIgnoresTheOrder(t *testing.T) {
	check, err := NewCheck(ModeExact, []string{"1.2.3.4", "5.6.7.8"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome := check.Evaluate([]string{"5.6.7.8", "1.2.3.4"}, nil)
	if !outcome.Satisfied {
		t.Fatal("the order of the answer does not matter")
	}
}

func TestEvaluateInTheExactModeNormalisesTheValues(t *testing.T) {
	check, err := NewCheck(ModeExact, []string{"1.2.3.4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome := check.Evaluate([]string{" 1.2.3.4 ", "1.2.3.4"}, nil)
	if !outcome.Satisfied {
		t.Fatal("the duplicates and the blanks do not matter")
	}
}

func TestEvaluateInTheRegexMode(t *testing.T) {
	check, err := NewCheck(ModeRegex, []string{"^v=spf1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome := check.Evaluate([]string{"v=spf1 -all"}, nil)
	if !outcome.Satisfied {
		t.Fatal("a matching answer satisfies the regex mode")
	}
	if outcome.Reason != "at least one answer matches ^v=spf1" {
		t.Fatalf("unexpected reason: %q", outcome.Reason)
	}
	if len(outcome.Matched) != 1 || outcome.Matched[0] != "v=spf1 -all" {
		t.Fatalf("unexpected matched values: %v", outcome.Matched)
	}

	outcome = check.Evaluate([]string{"a", "b"}, nil)
	if outcome.Satisfied {
		t.Fatal("an answer that does not match violates the regex mode")
	}
	if outcome.Reason != "no answer matches ^v=spf1, got [a, b]" {
		t.Fatalf("unexpected reason: %q", outcome.Reason)
	}
	if len(outcome.Unexpected) != 2 {
		t.Fatalf("unexpected unexpected values: %v", outcome.Unexpected)
	}
}

func TestEvaluateSearchesThePatternAnywhereInTheValue(t *testing.T) {
	check, err := NewCheck(ModeRegex, []string{"spf1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome := check.Evaluate([]string{"v=spf1 -all"}, nil)
	if !outcome.Satisfied {
		t.Fatal("the pattern is searched anywhere in the value")
	}
}

func TestEvaluateAlwaysComputesTheDiff(t *testing.T) {
	check, err := NewCheck(ModeExact, []string{"1.2.3.4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome := check.Evaluate([]string{"1.2.3.4", "5.6.7.8"}, []string{"1.2.3.4"})
	if len(outcome.Added) != 1 || outcome.Added[0] != "5.6.7.8" {
		t.Fatalf("unexpected additions: %v", outcome.Added)
	}
	if len(outcome.Removed) != 0 {
		t.Fatalf("unexpected removals: %v", outcome.Removed)
	}
}
