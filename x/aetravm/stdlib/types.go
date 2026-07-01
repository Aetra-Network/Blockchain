package stdlib

import (
	"errors"
	"fmt"
	"math/big"
	"math"
	"math/bits"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmath "cosmossdk.io/math"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
)

const CanonicalVersion = "avm-stdlib/v1"

type Address struct {
	raw []byte
}

type Coins struct {
	Denom  string
	Amount uint64
}

type Option[T any] struct {
	Value T
	Valid bool
}

type Result[T any] struct {
	Value T
	Err   error
}

type List[T any] []T

type Map[K comparable, V any] map[K]V

type Ref[T any] struct {
	Value T
	Valid bool
}

func AddressFromBytes(bz []byte) (Address, error) {
	if len(bz) == 0 {
		return Address{}, errors.New("address bytes are required")
	}
	out := make([]byte, len(bz))
	copy(out, bz)
	return Address{raw: out}, nil
}

func ParseAddress(text string) (Address, error) {
	bz, err := aetraaddress.Parse(strings.TrimSpace(text))
	if err != nil {
		return Address{}, err
	}
	return AddressFromBytes(bz)
}

func MustAddress(text string) Address {
	addr, err := ParseAddress(text)
	if err != nil {
		panic(err)
	}
	return addr
}

func (a Address) Bytes() []byte {
	return append([]byte(nil), a.raw...)
}

func (a Address) IsZero() bool {
	if len(a.raw) == 0 {
		return true
	}
	for _, b := range a.raw {
		if b != 0 {
			return false
		}
	}
	return true
}

func (a Address) Denom() string {
	return appparams.BaseDenom
}

func (a Address) String() string {
	if len(a.raw) == 0 {
		return ""
	}
	return sdk.AccAddress(a.raw).String()
}

func (c Coins) Normalize() Coins {
	if strings.TrimSpace(c.Denom) == "" {
		c.Denom = appparams.BaseDenom
	}
	return c
}

func NewCoins(amount uint64) Coins {
	return Coins{Denom: appparams.BaseDenom, Amount: amount}
}

func ParseCoins(amount uint64, denom string) Coins {
	if strings.TrimSpace(denom) == "" {
		denom = appparams.BaseDenom
	}
	return Coins{Denom: denom, Amount: amount}
}

func (c Coins) ToSDK() sdk.Coin {
	c = c.Normalize()
	return sdk.NewCoin(c.Denom, sdkmath.NewIntFromBigInt(new(big.Int).SetUint64(c.Amount)))
}

func (c Coins) String() string {
	c = c.Normalize()
	return fmt.Sprintf("%d%s", c.Amount, c.Denom)
}

func (c Coins) IsZero() bool {
	return c.Amount == 0
}

func (c Coins) Add(other Coins) (Coins, error) {
	if err := c.compatible(other); err != nil {
		return Coins{}, err
	}
	sum, ok := SafeAddUint64(c.Amount, other.Amount)
	if !ok {
		return Coins{}, errors.New("coin amount overflow")
	}
	return Coins{Denom: c.Normalize().Denom, Amount: sum}, nil
}

func (c Coins) Sub(other Coins) (Coins, error) {
	if err := c.compatible(other); err != nil {
		return Coins{}, err
	}
	if c.Amount < other.Amount {
		return Coins{}, errors.New("coin amount underflow")
	}
	return Coins{Denom: c.Normalize().Denom, Amount: c.Amount - other.Amount}, nil
}

func (c Coins) Mul(multiplier uint64) (Coins, error) {
	product, ok := SafeMulUint64(c.Amount, multiplier)
	if !ok {
		return Coins{}, errors.New("coin amount overflow")
	}
	return Coins{Denom: c.Normalize().Denom, Amount: product}, nil
}

func (c Coins) compatible(other Coins) error {
	left := c.Normalize()
	right := other.Normalize()
	if left.Denom != right.Denom {
		return fmt.Errorf("coin denomination mismatch: %q vs %q", left.Denom, right.Denom)
	}
	return nil
}

func SafeAddUint64(a, b uint64) (uint64, bool) {
	sum, carry := bits.Add64(a, b, 0)
	return sum, carry == 0
}

func SafeSubUint64(a, b uint64) (uint64, bool) {
	if a < b {
		return 0, false
	}
	return a - b, true
}

func SafeMulUint64(a, b uint64) (uint64, bool) {
	hi, lo := bits.Mul64(a, b)
	if hi != 0 {
		return 0, false
	}
	return lo, true
}

func ClampUint64(value, min, max uint64) uint64 {
	if min > max {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func SaturatingAddUint64(a, b uint64) uint64 {
	sum, ok := SafeAddUint64(a, b)
	if !ok {
		return math.MaxUint64
	}
	return sum
}

func SaturatingMulUint64(a, b uint64) uint64 {
	product, ok := SafeMulUint64(a, b)
	if !ok {
		return math.MaxUint64
	}
	return product
}

func Some[T any](value T) Option[T] {
	return Option[T]{Value: value, Valid: true}
}

func None[T any]() Option[T] {
	var zero T
	return Option[T]{Value: zero, Valid: false}
}

func Ok[T any](value T) Result[T] {
	return Result[T]{Value: value, Err: nil}
}

func ErrResult[T any](err error) Result[T] {
	var zero T
	return Result[T]{Value: zero, Err: err}
}

func (o Option[T]) OrElse(defaultValue T) T {
	if o.Valid {
		return o.Value
	}
	return defaultValue
}

func (r Result[T]) Unwrap() (T, error) {
	return r.Value, r.Err
}
