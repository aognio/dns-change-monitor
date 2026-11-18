package record

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
)

// Mode is the way a record is checked on every sweep.
type Mode string

// The check modes the monitor understands.
const (
	// ModeChange compares the answer with the previous observation. It is
	// the default and carries no value to check against.
	ModeChange Mode = "change"
	// ModeAny is satisfied when the record carries at least one answer.
	ModeAny Mode = "any"
	// ModeExact is satisfied when the answer set is exactly the expected one.
	ModeExact Mode = "exact"
	// ModeRegex is satisfied when at least one answer matches the pattern.
	ModeRegex Mode = "regex"
)

// SupportedModes lists every check mode the monitor understands.
func SupportedModes() []Mode {
	return []Mode{ModeChange, ModeAny, ModeExact, ModeRegex}
}

// ParseMode validates a mode coming from the configuration or the command
// line. An empty mode is the change mode.
func ParseMode(raw string) (Mode, error) {
	normalised := Mode(strings.ToLower(strings.TrimSpace(raw)))
	if normalised == "" {
		return ModeChange, nil
	}
	for _, supported := range SupportedModes() {
		if normalised == supported {
			return supported, nil
		}
	}
	return "", fmt.Errorf("%w: unknown check mode %q", shared.ErrInvalid, raw)
}

// Check is the way a record is checked.
type Check struct {
	// Mode is the kind of check performed. The empty value means ModeChange.
	Mode Mode
	// Expect holds the expected values. It is empty for ModeChange and
	// ModeAny, and holds exactly one pattern for ModeRegex.
	Expect []string
}

// NewCheck validates and builds a check.
func NewCheck(mode Mode, expect []string) (Check, error) {
	parsed, err := ParseMode(string(mode))
	if err != nil {
		return Check{}, err
	}

	values := make([]string, 0, len(expect))
	for _, value := range expect {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		values = append(values, trimmed)
	}

	switch parsed {
	case ModeChange:
		if len(values) > 0 {
			return Check{}, fmt.Errorf("%w: the change mode takes no expected value", shared.ErrInvalid)
		}
	case ModeAny:
		if len(values) > 0 {
			return Check{}, fmt.Errorf("%w: the any mode takes no expected value", shared.ErrInvalid)
		}
	case ModeExact:
		if len(values) == 0 {
			return Check{}, fmt.Errorf("%w: the exact mode needs at least one expected value", shared.ErrInvalid)
		}
	case ModeRegex:
		if len(values) != 1 {
			return Check{}, fmt.Errorf("%w: the regex mode needs exactly one expected value", shared.ErrInvalid)
		}
		if _, err := regexp.Compile(values[0]); err != nil {
			return Check{}, fmt.Errorf("%w: the expected pattern %q is not a valid regular expression: %s",
				shared.ErrInvalid, values[0], err.Error())
		}
	}

	return Check{Mode: parsed, Expect: values}, nil
}

// EffectiveMode returns the mode to apply, defaulting to ModeChange.
func (c Check) EffectiveMode() Mode {
	if c.Mode == "" {
		return ModeChange
	}
	return c.Mode
}

// IsDeclared reports whether the check carries a declared expectation, that
// is, whether it is anything but ModeChange.
func (c Check) IsDeclared() bool {
	return c.EffectiveMode() != ModeChange
}

// Outcome is the result of checking one answer set.
type Outcome struct {
	// Mode is the mode the check was performed with.
	Mode Mode
	// Satisfied tells whether the answer honours the expectation.
	Satisfied bool
	// Reason explains the verdict in one sentence, ready to be reported.
	Reason string
	// Added holds the values that appeared since the previous observation.
	Added []string
	// Removed holds the values that disappeared since the previous observation.
	Removed []string
	// Matched holds the observed values that satisfy the expectation.
	Matched []string
	// Unexpected holds the observed values that do not satisfy it.
	Unexpected []string
	// Missing holds the expected values that were not observed.
	Missing []string
}

// Evaluate checks an answer set against the expectation of the mode. The
// previous values are the ones of the last observation; they are the
// reference of ModeChange and the source of the diff of every mode.
func (c Check) Evaluate(current []string, previous []string) Outcome {
	mode := c.EffectiveMode()
	observed := NormaliseValues(current)
	before := NormaliseValues(previous)

	added, removed := DiffValues(before, observed)

	outcome := Outcome{
		Mode:    mode,
		Added:   added,
		Removed: removed,
	}

	switch mode {
	case ModeAny:
		outcome.Satisfied = len(observed) > 0
		outcome.Matched = observed
		if outcome.Satisfied {
			outcome.Reason = "the record carries at least one answer"
		} else {
			outcome.Reason = "the record carries no answer"
		}
	case ModeExact:
		expected := NormaliseValues(c.Expect)
		outcome.Missing = missingValues(expected, observed)
		outcome.Unexpected = missingValues(observed, expected)
		outcome.Matched = matchedValues(observed, expected)
		outcome.Satisfied = len(outcome.Missing) == 0 && len(outcome.Unexpected) == 0
		if outcome.Satisfied {
			outcome.Reason = fmt.Sprintf("the answer is exactly the expected set %s", renderList(expected))
		} else {
			outcome.Reason = fmt.Sprintf("expected exactly %s, got %s",
				renderList(expected), renderList(observed))
		}
	case ModeRegex:
		outcome.Matched, outcome.Unexpected = matchValues(observed, c.Expect)
		outcome.Satisfied = len(outcome.Matched) > 0
		pattern := ""
		if len(c.Expect) > 0 {
			pattern = c.Expect[0]
		}
		if outcome.Satisfied {
			outcome.Reason = fmt.Sprintf("at least one answer matches %s", pattern)
		} else {
			outcome.Reason = fmt.Sprintf("no answer matches %s, got %s",
				pattern, renderList(observed))
		}
	default:
		outcome.Satisfied = len(added) == 0 && len(removed) == 0
		if outcome.Satisfied {
			outcome.Reason = "the answer is the same as the last observation"
		} else {
			outcome.Reason = "the answer changed"
		}
	}

	return outcome
}

// missingValues returns the values of wanted that are not in observed.
func missingValues(wanted, observed []string) []string {
	present := index(observed)
	missing := make([]string, 0, len(wanted))
	for _, value := range wanted {
		if _, ok := present[value]; !ok {
			missing = append(missing, value)
		}
	}
	return missing
}

// matchedValues returns the values of observed that are in expected.
func matchedValues(observed, expected []string) []string {
	present := index(expected)
	matched := make([]string, 0, len(observed))
	for _, value := range observed {
		if _, ok := present[value]; ok {
			matched = append(matched, value)
		}
	}
	return matched
}

// matchValues splits the observed values into the ones matching the pattern
// and the ones that do not.
func matchValues(observed, patterns []string) (matched []string, unexpected []string) {
	if len(patterns) == 0 {
		return nil, observed
	}
	expression, err := regexp.Compile(patterns[0])
	if err != nil {
		// The pattern is validated when the check is built, so this can
		// only happen when the check was assembled by hand.
		return nil, observed
	}
	for _, value := range observed {
		if expression.MatchString(value) {
			matched = append(matched, value)
			continue
		}
		unexpected = append(unexpected, value)
	}
	return matched, unexpected
}

// renderList renders a set of values the way the reports show it.
func renderList(values []string) string {
	return "[" + strings.Join(values, ", ") + "]"
}
