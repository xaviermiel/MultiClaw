// Package bn provides helpers for *big.Int values that are treated as immutable.
//
// The TypeScript oracle used JS BigInt everywhere: arbitrary precision, signed,
// immutable, with division truncating toward zero. Every helper here returns a
// freshly allocated result and never mutates its operands, so values can be
// shared freely (queue entries, deposit records, caches) without aliasing bugs.
// Never use a value obtained from this package as the receiver of a big.Int
// method.
package bn

import (
	"fmt"
	"math/big"
	"strings"
)

// Precision is the 1e18 fixed-point scale used for ratio calculations.
var Precision = Pow10(18)

// BPS is the basis-point denominator (10000).
var BPS = big.NewInt(10_000)

// New returns a new big.Int holding x.
func New(x int64) *big.Int { return big.NewInt(x) }

// Zero returns a new zero value.
func Zero() *big.Int { return new(big.Int) }

// Copy returns a new big.Int equal to x (nil is treated as zero).
func Copy(x *big.Int) *big.Int {
	if x == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(x)
}

// Add returns a + b.
func Add(a, b *big.Int) *big.Int { return new(big.Int).Add(a, b) }

// Sub returns a - b.
func Sub(a, b *big.Int) *big.Int { return new(big.Int).Sub(a, b) }

// Mul returns a * b.
func Mul(a, b *big.Int) *big.Int { return new(big.Int).Mul(a, b) }

// Quo returns a / b truncated toward zero, matching JS BigInt division.
// It panics if b is zero, like big.Int (JS throws a RangeError).
func Quo(a, b *big.Int) *big.Int { return new(big.Int).Quo(a, b) }

// MulDiv returns a * b / c, truncated toward zero.
func MulDiv(a, b, c *big.Int) *big.Int { return Quo(Mul(a, b), c) }

// Min returns the smaller of a and b (a new value).
func Min(a, b *big.Int) *big.Int {
	if a.Cmp(b) <= 0 {
		return Copy(a)
	}
	return Copy(b)
}

// Max returns the larger of a and b (a new value).
func Max(a, b *big.Int) *big.Int {
	if a.Cmp(b) >= 0 {
		return Copy(a)
	}
	return Copy(b)
}

// Sum returns the sum of xs (zero when empty).
func Sum(xs ...*big.Int) *big.Int {
	total := new(big.Int)
	for _, x := range xs {
		total.Add(total, x)
	}
	return total
}

// IsZero reports whether x == 0.
func IsZero(x *big.Int) bool { return x.Sign() == 0 }

// IsPos reports whether x > 0.
func IsPos(x *big.Int) bool { return x.Sign() > 0 }

// Pow10 returns 10^n exactly. (The TS used BigInt(10 ** n), which is only exact
// for n <= 22.)
func Pow10(n uint) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// Parse parses a base-10 integer string (optionally signed).
func Parse(s string) (*big.Int, error) {
	x, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer %q", s)
	}
	return x, nil
}

// MustParse is Parse that panics on error. Intended for constants and tests.
func MustParse(s string) *big.Int {
	x, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return x
}

// FormatUnits renders v as a decimal string with the given number of decimals,
// trimming trailing fractional zeros. Port of viem's formatUnits.
func FormatUnits(v *big.Int, decimals int) string {
	display := v.String()
	negative := strings.HasPrefix(display, "-")
	if negative {
		display = display[1:]
	}
	if len(display) < decimals {
		display = strings.Repeat("0", decimals-len(display)) + display
	}
	integer := display[:len(display)-decimals]
	fraction := strings.TrimRight(display[len(display)-decimals:], "0")

	var b strings.Builder
	if negative {
		b.WriteByte('-')
	}
	if integer == "" {
		b.WriteByte('0')
	} else {
		b.WriteString(integer)
	}
	if fraction != "" {
		b.WriteByte('.')
		b.WriteString(fraction)
	}
	return b.String()
}
