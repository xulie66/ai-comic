package httpapi

import (
	"comic.local/go-service/internal/generation"
	"comic.local/go-service/internal/operations"
	"math/big"
)

func tokenCost(r operations.Route, u generation.Usage) int64 {
	input := u.InputTokens - u.CachedTokens
	if input < 0 {
		input = 0
	}
	sum := new(big.Int)
	for _, pair := range [][2]int64{{input, r.InputTokenPriceMicro}, {u.CachedTokens, r.CachedTokenPriceMicro}, {u.OutputTokens, r.OutputTokenPriceMicro}} {
		if pair[0] > 0 && pair[1] > 0 {
			sum.Add(sum, new(big.Int).Mul(big.NewInt(pair[0]), big.NewInt(pair[1])))
		}
	}
	sum.Add(sum, big.NewInt(999999))
	sum.Div(sum, big.NewInt(1000000))
	if !sum.IsInt64() {
		return int64(^uint64(0) >> 1)
	}
	return sum.Int64()
}
