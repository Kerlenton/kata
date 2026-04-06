package kata_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/kerlenton/kata"
)

// testState is a shared state type used across all tests.
type testState struct {
	log  []string
	mu   sync.Mutex
	fail string // step name to inject failure into
}

func (s *testState) append(v string) {
	s.mu.Lock()
	s.log = append(s.log, v)
	s.mu.Unlock()
}

func (s *testState) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]string, len(s.log))
	copy(cp, s.log)
	return cp
}

// mkStep returns a step function that appends "do:<name>" to the log.
// If state.fail == name the step returns an error instead.
func mkStep(name string) kata.StepFunc[*testState] {
	return func(_ context.Context, s *testState) error {
		if s.fail == name {
			return errors.New("injected failure in " + name)
		}
		s.append("do:" + name)
		return nil
	}
}

// mkComp returns a compensation function that appends "undo:<name>" to the log.
func mkComp(name string) kata.StepFunc[*testState] {
	return func(_ context.Context, s *testState) error {
		s.append("undo:" + name)
		return nil
	}
}

// assertLog checks that got matches want exactly.
func assertLog(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("log length mismatch:\n  got:  %v\n  want: %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("log[%d] mismatch:\n  got:  %v\n  want: %v", i, got, want)
		}
	}
}

// assertContains checks that haystack contains needle.
func assertContains(t *testing.T, haystack []string, needle string) {
	t.Helper()
	for _, v := range haystack {
		if v == needle {
			return
		}
	}
	t.Errorf("expected %v to contain %q", haystack, needle)
}
