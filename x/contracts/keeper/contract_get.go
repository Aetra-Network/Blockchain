package keeper

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// contractGetDefaultGas bounds a get-method execution when the caller does
// not pick a limit. Getters are read-only lookups; this is generous.
const contractGetDefaultGas = 200_000

// ContractGet executes a read-only @get function on a deployed contract by
// its EXACT source-level name. Dispatch uses the compiler-emitted name-alias
// selector (avm.GetterNameSelector), so the binding is to the function name
// character for character — currentCounter and current_counter are different
// methods. State is never mutated: the VM runs against a copy of the
// contract's storage snapshot and the result is discarded after rendering.
func (k Keeper) ContractGet(req types.QueryContractGetRequest) (types.QueryContractGetResponse, error) {
	if err := req.ValidateBasic(); err != nil {
		return types.QueryContractGetResponse{}, err
	}
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return types.QueryContractGetResponse{}, err
	}
	contract, found := findContract(k.genesis.State.Contracts, req.ContractAddress)
	if !found {
		return types.QueryContractGetResponse{}, errors.New(types.ErrContractNotFound + ": no contract at address")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionQuery); err != nil {
		return types.QueryContractGetResponse{}, err
	}
	code, ok := findCode(k.genesis.State.Codes, contract.CodeID)
	if !ok {
		return types.QueryContractGetResponse{}, errors.New(types.ErrContractNotFound + ": contract code record missing")
	}
	module, executable, err := loadAVMModule(code)
	if err != nil {
		return types.QueryContractGetResponse{}, err
	}
	if !executable {
		return types.QueryContractGetResponse{}, errors.New(types.ErrInvalidBytecode + ": contract code is not an executable AVM module")
	}
	state, decodable, err := decodeContractSnapshot(contract.Data)
	if err != nil {
		return types.QueryContractGetResponse{}, fmt.Errorf("decode contract storage: %w", err)
	}
	if !decodable {
		return types.QueryContractGetResponse{}, errors.New(types.ErrExecutionFailed + ": contract state is not AVM snapshot compatible")
	}

	method := strings.TrimSpace(req.Method)
	selector := avm.GetterNameSelector(method)

	var queryID uint64
	if len(req.Args) == 1 {
		queryID, err = req.Args[0].AsUint64()
		if err != nil {
			return types.QueryContractGetResponse{}, err
		}
	}

	gas := req.GasLimit
	if gas == 0 {
		gas = contractGetDefaultGas
	}
	if max := k.genesis.Params.MaxGasPerExecution; max > 0 && gas > max {
		gas = max
	}

	runner, err := avm.NewRunner(avm.DefaultParams())
	if err != nil {
		return types.QueryContractGetResponse{}, err
	}
	exec, runErr := runner.Run(module, state, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: gas,
		Message: async.MessageEnvelope{
			Opcode:   selector,
			QueryID:  queryID,
			GasLimit: gas,
		},
	})
	resp := types.QueryContractGetResponse{Method: method, Selector: selector}
	if runErr != nil {
		resp.Success = false
		resp.Error = runErr.Error()
		if strings.Contains(runErr.Error(), "abort") || strings.Contains(runErr.Error(), "ffff") {
			resp.Error = fmt.Sprintf("get method %q not found on this contract (dispatch aborted): %v", method, runErr)
		}
		return resp, nil
	}
	resp.GasUsed = exec.GasUsed
	resp.ExitCode = exec.ResultCode
	if exec.ResultCode != async.ResultOK {
		resp.Success = false
		resp.Error = fmt.Sprintf("get method exited with code %d", exec.ResultCode)
		return resp, nil
	}
	resp.Success = true
	resp.Result, resp.ResultType = renderRuntimeValue(exec.ReturnValue)
	return resp, nil
}

// renderRuntimeValue turns a getter's return value into the human-facing
// string an explorer shows, plus the runtime type name.
func renderRuntimeValue(v avm.RuntimeValue) (string, string) {
	typeName := v.Tag.String()
	switch v.Tag {
	case avm.TagNull:
		return "null", typeName
	case avm.TagBool:
		b, err := v.AsBool()
		if err == nil {
			return fmt.Sprintf("%t", b), typeName
		}
	case avm.TagAddress:
		a, err := v.AsAddress()
		if err == nil {
			return a, typeName
		}
	case avm.TagString:
		s, err := v.AsString()
		if err == nil {
			return s, typeName
		}
	case avm.TagBytes:
		b, err := v.AsBytes()
		if err == nil {
			return hex.EncodeToString(b), typeName
		}
	case avm.TagHash:
		h, err := v.AsHash()
		if err == nil {
			return hex.EncodeToString(h[:]), typeName
		}
	}
	// Every numeric tag (ints, uints, coins, timestamp) renders as decimal.
	if n, err := v.AsBigInt(); err == nil {
		return n.String(), typeName
	}
	// Tuples, maps, chunk refs: canonical encoding as hex.
	if encoded, err := avm.CanonicalEncode(v); err == nil {
		return hex.EncodeToString(encoded), typeName
	}
	return "", typeName
}
