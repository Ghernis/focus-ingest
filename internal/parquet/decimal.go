package parquet

import (
	"math/big"

	"github.com/shopspring/decimal"
)

// Azure FOCUS exports cost/quantity columns as DECIMAL(38,18) stored in 16-byte fixed arrays.
const focusDecimalScale int32 = 18

// decimalFlbaString decodes a Parquet FIXED_LEN_BYTE_ARRAY DECIMAL (big-endian two's complement).
func decimalFlbaString(b *[16]byte) *string {
	if b == nil {
		return nil
	}
	data := (*b)[:]
	allZero := true
	for _, by := range data {
		if by != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		zero := "0"
		return &zero
	}

	val := new(big.Int)
	if data[0]&0x80 != 0 {
		tmp := make([]byte, len(data))
		for i, by := range data {
			tmp[i] = ^by
		}
		val.SetBytes(tmp)
		val.Add(val, big.NewInt(1))
		val.Neg(val)
	} else {
		val.SetBytes(data)
	}

	d := decimal.NewFromBigInt(val, -focusDecimalScale)
	s := d.String()
	return &s
}
