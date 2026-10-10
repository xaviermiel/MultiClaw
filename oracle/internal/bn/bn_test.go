package bn

import (
	"math/big"
	"testing"
)

func TestQuoTruncatesTowardZeroLikeJS(t *testing.T) {
	cases := []struct{ a, b, want int64 }{
		{7, 2, 3},
		{-7, 2, -3}, // JS: -7n / 2n === -3n (big.Int.Div would give -4)
		{7, -2, -3},
		{-7, -2, 3},
		{0, 5, 0},
		{-1, 3, 0},
	}
	for _, c := range cases {
		if got := Quo(New(c.a), New(c.b)); got.Int64() != c.want {
			t.Errorf("Quo(%d, %d) = %s, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestHelpersDoNotMutateOperands(t *testing.T) {
	a, b := New(10), New(3)
	_ = Add(a, b)
	_ = Sub(a, b)
	_ = Mul(a, b)
	_ = Quo(a, b)
	_ = MulDiv(a, b, b)
	_ = Min(a, b)
	_ = Max(a, b)
	_ = Sum(a, b)
	if a.Int64() != 10 || b.Int64() != 3 {
		t.Fatalf("operands mutated: a=%s b=%s", a, b)
	}

	c := Copy(a)
	c.SetInt64(99)
	if a.Int64() != 10 {
		t.Fatalf("Copy aliases its input")
	}
	if Copy(nil).Sign() != 0 {
		t.Fatalf("Copy(nil) should be zero")
	}

	m := Min(a, b)
	m.SetInt64(0)
	if b.Int64() != 3 {
		t.Fatalf("Min aliases its result with an operand")
	}
}

func TestPow10IsExact(t *testing.T) {
	// 10^24 is not representable as a float64; BigInt(10 ** 24) in JS yields
	// 999999999999999983222784n. Pow10 must be exact.
	want, _ := new(big.Int).SetString("1000000000000000000000000", 10)
	if got := Pow10(24); got.Cmp(want) != 0 {
		t.Fatalf("Pow10(24) = %s", got)
	}
	if Pow10(0).Int64() != 1 || Pow10(18).Cmp(Precision) != 0 {
		t.Fatalf("Pow10 basic values wrong")
	}
}

func TestParse(t *testing.T) {
	if v, err := Parse("-123"); err != nil || v.Int64() != -123 {
		t.Fatalf("Parse(-123) = %v, %v", v, err)
	}
	for _, bad := range []string{"", "1e3", "0x10", "12a", " 1"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

// Expected values produced with viem's formatUnits.
func TestFormatUnitsMatchesViem(t *testing.T) {
	cases := []struct {
		v        string
		decimals int
		want     string
	}{
		{"0", 18, "0"},
		{"5", 18, "0.000000000000000005"},
		{"1000000000000000000", 18, "1"},
		{"1500000000000000000", 18, "1.5"},
		{"25555346832028400000", 18, "25.5553468320284"},
		{"-1500000000000000000", 18, "-1.5"},
		{"-5", 18, "-0.000000000000000005"},
		{"84161345", 6, "84.161345"},
		{"232564140523", 8, "2325.64140523"},
		{"123", 0, "123"},
		{"100", 2, "1"},
		{"120", 2, "1.2"},
	}
	for _, c := range cases {
		if got := FormatUnits(MustParse(c.v), c.decimals); got != c.want {
			t.Errorf("FormatUnits(%s, %d) = %q, want %q", c.v, c.decimals, got, c.want)
		}
	}
}
