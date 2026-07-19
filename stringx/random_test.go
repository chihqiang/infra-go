package stringx

import (
	"regexp"
	"testing"
)

func TestRand(t *testing.T) {
	s := Rand()
	if len(s) != defaultRandLen {
		t.Errorf("Rand() length = %d, want %d", len(s), defaultRandLen)
	}
}

func TestRandId(t *testing.T) {
	s := RandId()
	if len(s) != idLen*2 {
		t.Errorf("RandId() length = %d, want %d", len(s), idLen*2)
	}
}

func TestRandn(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		randType RandType
		pattern string
	}{
		{
			name:     "All",
			n:        10,
			randType: RandTypeAll,
			pattern:  `^[a-zA-Z0-9]{10}$`,
		},
		{
			name:     "Upper",
			n:        8,
			randType: RandTypeUpper,
			pattern:  `^[A-Z]{8}$`,
		},
		{
			name:     "Lower",
			n:        12,
			randType: RandTypeLower,
			pattern:  `^[a-z]{12}$`,
		},
		{
			name:     "Digit",
			n:        6,
			randType: RandTypeDigit,
			pattern:  `^[0-9]{6}$`,
		},
		{
			name:     "ZeroLength",
			n:        0,
			randType: RandTypeAll,
			pattern:  `^$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Randn(tt.n, tt.randType)
			if len(s) != tt.n {
				t.Errorf("Randn(%d, %v) length = %d, want %d", tt.n, tt.randType, len(s), tt.n)
			}
			matched, err := regexp.MatchString(tt.pattern, s)
			if err != nil {
				t.Fatalf("invalid pattern: %v", err)
			}
			if !matched {
				t.Errorf("Randn(%d, %v) = %q, does not match %s", tt.n, tt.randType, s, tt.pattern)
			}
		})
	}
}

func TestRandnUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s := Randn(16, RandTypeAll)
		if seen[s] {
			t.Errorf("Randn() produced duplicate: %q", s)
		}
		seen[s] = true
	}
}

func TestSeed(t *testing.T) {
	Seed(12345)
	s1 := Randn(10, RandTypeAll)
	Seed(12345)
	s2 := Randn(10, RandTypeAll)
	if s1 != s2 {
		t.Errorf("Seed() with same seed should produce same sequence, got %q and %q", s1, s2)
	}
}
