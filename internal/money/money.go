// Package money writes US dollar amounts the one way Byname shows them to
// people: whole cents with thousands separators, and "under $0.01" for an
// amount above zero that rounds below a cent. web/src/money.ts does the
// same in the browser; cases.json holds the cases both are tested against.
package money

import (
	"math/big"
	"strconv"
)

// Format writes usd, an amount of zero or more, as "$0.00", "under $0.01",
// "$7.97" or "$1,234.50".
func Format(usd float64) string {
	if usd > 0 && usd < 0.01 {
		return "under $0.01"
	}
	cents := roundCents(usd)
	whole := strconv.FormatInt(cents/100, 10)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	frac := strconv.FormatInt(cents%100, 10)
	if len(frac) == 1 {
		frac = "0" + frac
	}
	return "$" + whole + "." + frac
}

// roundCents rounds half up from the shortest decimal that reads back as
// usd, as the browser's Intl.NumberFormat does, so 1.005 is 101 cents even
// though its float64 is a hair below 1.005.
func roundCents(usd float64) int64 {
	r, _ := new(big.Rat).SetString(strconv.FormatFloat(usd, 'f', -1, 64))
	r.Mul(r, big.NewRat(100, 1)).Add(r, big.NewRat(1, 2))
	return new(big.Int).Quo(r.Num(), r.Denom()).Int64()
}
