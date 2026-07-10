package types

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// GetMethodArg is one typed argument of a get-method call. AVM v1 maps the
// first (and only) argument onto the query envelope's uint64 slot, so the
// accepted types are the numeric spellings; richer argument ABIs arrive with
// the extended entry convention.
type GetMethodArg struct {
	Type  string `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

// QueryContractGetRequest invokes a read-only @get function on a deployed
// contract by its EXACT source-level function name (currentCounter,
// lpBalanceOf, …). Dispatch binds to the name via the compiler-emitted
// name-alias selector, so the name is the ABI.
type QueryContractGetRequest struct {
	ContractAddress string         `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	Method          string         `protobuf:"bytes,2,opt,name=method,proto3" json:"method,omitempty"`
	Args            []GetMethodArg `protobuf:"bytes,3,rep,name=args,proto3" json:"args,omitempty"`
	GasLimit        uint64         `protobuf:"varint,4,opt,name=gas_limit,json=gasLimit,proto3" json:"gas_limit,omitempty"`
}

// QueryContractGetResponse carries the get-method outcome: TON-style exit
// code, gas used, and the returned value rendered as a string plus its
// runtime type name.
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

// ValidateBasic checks the request shape without touching state.
func (m QueryContractGetRequest) ValidateBasic() error {
	if strings.TrimSpace(m.Method) == "" {
		return errors.New("get method name is required (the exact @get function name)")
	}
	if len(m.Args) > 1 {
		return errors.New("AVM v1 get methods accept at most one argument (mapped to the query envelope)")
	}
	if len(m.Args) == 1 {
		if _, err := m.Args[0].AsUint64(); err != nil {
			return err
		}
	}
	return nil
}

// AsUint64 parses the argument value according to its declared type into the
// uint64 the query envelope carries.
func (a GetMethodArg) AsUint64() (uint64, error) {
	typ := strings.ToLower(strings.TrimSpace(a.Type))
	switch typ {
	case "", "uint64", "uint", "uint32", "uint16", "uint8", "number", "int", "int64", "coins", "timestamp":
		value := strings.TrimSpace(a.Value)
		if value == "" {
			return 0, errors.New("get method argument value is required")
		}
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("get method argument %q is not a valid %s: %w", a.Value, typ, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported get method argument type %q: AVM v1 passes one numeric argument", a.Type)
	}
}
