package hubble

import (
	"errors"
	"testing"
)

func TestShouldExitForInfraDrops(t *testing.T) {
	tests := []struct {
		name             string
		failOnInfraDrops bool
		infraDropTotal   uint64
		want             bool
	}{
		{
			name:             "flag not set, drops observed -> always false (P3 backward-compat invariant)",
			failOnInfraDrops: false,
			infraDropTotal:   100,
			want:             false,
		},
		{
			name:             "flag set, no drops -> false (no infra issue)",
			failOnInfraDrops: true,
			infraDropTotal:   0,
			want:             false,
		},
		{
			name:             "flag set, drops observed -> true (exit 1)",
			failOnInfraDrops: true,
			infraDropTotal:   5,
			want:             true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldExitForInfraDrops(tc.failOnInfraDrops, tc.infraDropTotal)
			if got != tc.want {
				t.Errorf("shouldExitForInfraDrops(%v, %d) = %v, want %v",
					tc.failOnInfraDrops, tc.infraDropTotal, got, tc.want)
			}
		})
	}
}

func TestExitCodeError(t *testing.T) {
	t.Run("Error() returns message", func(t *testing.T) {
		e := &ExitCodeError{Code: 1, Msg: "infra drops detected: 5 flows suppressed"}
		if e.Error() != "infra drops detected: 5 flows suppressed" {
			t.Errorf("Error() = %q, want %q", e.Error(), "infra drops detected: 5 flows suppressed")
		}
	})

	t.Run("errors.As unwraps ExitCodeError", func(t *testing.T) {
		original := &ExitCodeError{Code: 1, Msg: "test"}
		wrapped := original // direct value for errors.As test
		var ec *ExitCodeError
		if !errors.As(wrapped, &ec) {
			t.Fatal("errors.As should succeed for *ExitCodeError")
		}
		if ec.Code != 1 {
			t.Errorf("ec.Code = %d, want 1", ec.Code)
		}
	})
}
