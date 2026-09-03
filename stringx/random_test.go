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
		name     string
		n        int
		randType RandType
		pattern  string
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

func TestRandn_NegativeOrZeroLength(t *testing.T) {
	// 负数长度不应 panic，返回空串
	if s := Randn(-1, RandTypeAll); s != "" {
		t.Errorf("Randn(-1) = %q, want empty string", s)
	}
	if s := Randn(-100, RandTypeDigit); s != "" {
		t.Errorf("Randn(-100, Digit) = %q, want empty string", s)
	}
	// 零长度返回空串（已有用例，这里再确认一次）
	if s := Randn(0, RandTypeUpper); s != "" {
		t.Errorf("Randn(0) = %q, want empty string", s)
	}
}

func TestRandId_HexCharset(t *testing.T) {
	// RandId 应返回小写 hex 字符串（32 字符 = 16 字节）
	for i := 0; i < 20; i++ {
		s := RandId()
		if len(s) != idLen*2 {
			t.Errorf("RandId() length = %d, want %d", len(s), idLen*2)
		}
		matched, err := regexp.MatchString(`^[0-9a-f]{16}$`, s)
		if err != nil {
			t.Fatalf("invalid pattern: %v", err)
		}
		if !matched {
			t.Errorf("RandId() = %q, not a 16-char lowercase hex string", s)
		}
	}
}

func TestRandn_LargeLength(t *testing.T) {
	// 大长度不应 panic，且严格满足字符集约束
	for _, tt := range []struct {
		n        int
		randType RandType
		pattern  string
	}{
		{2048, RandTypeAll, `^[a-zA-Z0-9]+$`},
		{1024, RandTypeUpper, `^[A-Z]+$`},
		{1024, RandTypeLower, `^[a-z]+$`},
		{1024, RandTypeDigit, `^[0-9]+$`},
	} {
		s := Randn(tt.n, tt.randType)
		if len(s) != tt.n {
			t.Errorf("Randn(%d, %v) length = %d, want %d", tt.n, tt.randType, len(s), tt.n)
		}
		matched, err := regexp.MatchString(tt.pattern, s)
		if err != nil {
			t.Fatalf("invalid pattern: %v", err)
		}
		if !matched {
			t.Errorf("Randn(%d, %v) contains invalid characters", tt.n, tt.randType)
		}
	}
}

func TestSeed_DeterministicByType(t *testing.T) {
	// 各类别下相同 seed 均产生相同序列
	for _, rt := range []RandType{RandTypeAll, RandTypeUpper, RandTypeLower, RandTypeDigit} {
		Seed(99)
		s1 := Randn(12, rt)
		Seed(99)
		s2 := Randn(12, rt)
		if s1 != s2 {
			t.Errorf("Seed(99) type %v: got %q and %q, want identical", rt, s1, s2)
		}
	}
}
