package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MaxGetMethodArgs bounds how many arguments a single get-method call may
// carry — generous enough for any real getter, small enough to keep the
// encoded query body bounded.
const MaxGetMethodArgs = 16

// GetMethodArg is one typed argument of a get-method call. Type is any
// canonical AVM value-type spelling the runtime's message-field decoder
// understands — uint2..uint256, int2..int256 (long or short form: "uint64"
// or "u64"), bool, address, hash, string, bytes, coins, timestamp — or the
// convenience alias "number" (an unsized decimal integer; infers uint256 for
// a non-negative value, int256 for a negative one, so it always fits without
// the caller having to pick a width).
type GetMethodArg struct {
	Type  string `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

// QueryContractGetRequest invokes a read-only @get function on a deployed
// contract by its EXACT source-level function name (currentCounter,
// lpBalanceOf, …). Dispatch binds to the name via the compiler-emitted
// name-alias selector, so the name is the ABI. A getter may declare any
// number of parameters; each argument here binds positionally to one
// (Args[0] to the getter's first parameter, and so on).
type QueryContractGetRequest struct {
	ContractAddress string         `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	Method          string         `protobuf:"bytes,2,opt,name=method,proto3" json:"method,omitempty"`
	Args            []GetMethodArg `protobuf:"bytes,3,rep,name=args,proto3" json:"args,omitempty"`
	GasLimit        uint64         `protobuf:"varint,4,opt,name=gas_limit,json=gasLimit,proto3" json:"gas_limit,omitempty"`
}

// QueryContractGetResponse carries the get-method outcome: an exit code,
// gas used, and the returned value rendered as a string plus its runtime
// type name.
type QueryContractGetResponse struct {
	Success    bool   `protobuf:"varint,1,opt,name=success,proto3" json:"success,omitempty"`
	ExitCode   uint32 `protobuf:"varint,2,opt,name=exit_code,json=exitCode,proto3" json:"exit_code,omitempty"`
	GasUsed    uint64 `protobuf:"varint,3,opt,name=gas_used,json=gasUsed,proto3" json:"gas_used,omitempty"`
	Result     string `protobuf:"bytes,4,opt,name=result,proto3" json:"result,omitempty"`
	ResultType string `protobuf:"bytes,5,opt,name=result_type,json=resultType,proto3" json:"result_type,omitempty"`
	Method     string `protobuf:"bytes,6,opt,name=method,proto3" json:"method,omitempty"`
	Selector   uint32 `protobuf:"varint,7,opt,name=selector,proto3" json:"selector,omitempty"`
	Error      string `protobuf:"bytes,8,opt,name=error,proto3" json:"error,omitempty"`
}

// getMethodIntegerArgTypes is the set of accepted integer-type spellings —
// both the canonical long form (uint64, int256, …) the compiler actually
// emits and the short form (u64, i256, …) — mirroring
// avm.buildIntegerKindTags. Kept independent of the avm package: this is a
// small accepted-spelling whitelist for request validation, not runtime
// decode logic, so the two are allowed to live in their own layers.
var getMethodIntegerArgTypes = buildGetMethodIntegerArgTypes()

func buildGetMethodIntegerArgTypes() map[string]bool {
	out := map[string]bool{}
	for _, bits := range []int{2, 4, 8, 16, 32, 64, 128, 256} {
		out[fmt.Sprintf("u%d", bits)] = true
		out[fmt.Sprintf("uint%d", bits)] = true
		out[fmt.Sprintf("i%d", bits)] = true
		out[fmt.Sprintf("int%d", bits)] = true
	}
	return out
}

// getMethodOtherArgTypes is every non-integer, non-bool type spelling the
// runtime's message-field decoder accepts.
var getMethodOtherArgTypes = map[string]bool{
	"coins": true, "timestamp": true, "address": true,
	"hash": true, "hash32": true, "string": true,
	"bytes": true, "code": true, "chunk": true, "stateinit": true,
}

// ValidateBasic checks the request shape without touching state.
func (m QueryContractGetRequest) ValidateBasic() error {
	if strings.TrimSpace(m.Method) == "" {
		return errors.New("get method name is required (the exact @get function name)")
	}
	if len(m.Args) > MaxGetMethodArgs {
		return fmt.Errorf("get method call carries %d arguments, exceeds limit %d", len(m.Args), MaxGetMethodArgs)
	}
	for i, arg := range m.Args {
		if _, _, err := arg.canonicalize(); err != nil {
			return fmt.Errorf("argument %d: %w", i, err)
		}
	}
	return nil
}

// EncodeMessageBody renders the argument list as the {name,type,value}
// field-array JSON the AVM runtime decodes message-body fields with (see
// runtimeMessageFieldValue in x/aetravm/avm). Positional field names "arg0",
// "arg1", … match the compiler's getter/entrypoint parameter binding
// (IRExprMsgField), so a getter with N parameters reads Args[i] as its
// (i+1)-th parameter regardless of N — the previous "at most one argument"
// ceiling was a compiler limitation, not a wire-format one.
func (m QueryContractGetRequest) EncodeMessageBody() ([]byte, error) {
	type fieldEntry struct {
		Name  string          `json:"name"`
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	entries := make([]fieldEntry, 0, len(m.Args))
	for i, arg := range m.Args {
		typ, value, err := arg.canonicalize()
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i, err)
		}
		entries = append(entries, fieldEntry{Name: fmt.Sprintf("arg%d", i), Type: typ, Value: value})
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return json.Marshal(entries)
}

// canonicalize resolves the argument's declared (or inferred, for "number")
// canonical AVM type and renders its value as the JSON the runtime field
// decoder expects.
func (a GetMethodArg) canonicalize() (typeName string, value json.RawMessage, err error) {
	typ := strings.ToLower(strings.TrimSpace(a.Type))
	raw := strings.TrimSpace(a.Value)

	switch {
	case typ == "" || typ == "number":
		// The convenience umbrella type: an unsized decimal integer, in the
		// widest safe range so the caller never has to pick a width.
		if !isDecimalInteger(raw) {
			return "", nil, fmt.Errorf("argument value %q is not a decimal integer", raw)
		}
		typ = "uint256"
		if strings.HasPrefix(raw, "-") {
			typ = "int256"
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return "", nil, err
		}
		return typ, encoded, nil
	case typ == "bool":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return "", nil, fmt.Errorf("argument value %q is not a boolean: %w", raw, err)
		}
		encoded, err := json.Marshal(b)
		if err != nil {
			return "", nil, err
		}
		return typ, encoded, nil
	case getMethodIntegerArgTypes[typ]:
		if !isDecimalInteger(raw) {
			return "", nil, fmt.Errorf("argument value %q is not a decimal integer", raw)
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return "", nil, err
		}
		return typ, encoded, nil
	case getMethodOtherArgTypes[typ]:
		// address / hash / hash32 / string / bytes / code / chunk / stateinit
		// / coins / timestamp: pass the raw text through as a JSON string;
		// the runtime decoder parses coins/timestamp text as decimal too.
		encoded, err := json.Marshal(raw)
		if err != nil {
			return "", nil, err
		}
		return typ, encoded, nil
	default:
		return "", nil, fmt.Errorf("unsupported get method argument type %q", a.Type)
	}
}

func isDecimalInteger(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
