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
func (k *Keeper) ContractGet(req types.QueryContractGetRequest) (types.QueryContractGetResponse, error) {
	if err := req.ValidateBasic(); err != nil {
		return types.QueryContractGetResponse{}, err
	}
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return types.QueryContractGetResponse{}, err
	}
	gs := k.snapshotGenesis()
	contract, found := findContract(gs.State.Contracts, req.ContractAddress)
	if !found {
		return types.QueryContractGetResponse{}, errors.New(types.ErrContractNotFound + ": no contract at address")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionQuery); err != nil {
		return types.QueryContractGetResponse{}, err
	}
	code, ok := findCode(gs.State.Codes, contract.CodeID)
	if !ok {
		return types.QueryContractGetResponse{}, errors.New(types.ErrContractNotFound + ": contract code record missing")
	}

	gas := req.GasLimit
	if gas == 0 {
		gas = contractGetDefaultGas
	}
	if max := gs.Params.MaxGasPerExecution; max > 0 && gas > max {
		gas = max
	}
	// Phase H: reject a caller-chosen near-zero GasLimit against a
	// large-code OR large-storage contract BEFORE paying loadAVMModule's/
	// decodeContractSnapshot's O(code+storage) decode cost. This check MUST
	// run before both decode calls below, not after: findCode above is a
	// cheap metadata lookup (CodeRecord.CodeBytes needs no decode), so
	// everything this check needs is already available without having paid
	// for either O(size) parse yet. See types.RequireCloneGasFloor (extends
	// RequireStorageCloneGasFloor with a codeBytes floor, so a contract with
	// tiny storage but bytecode near Params.MaxCodeBytes can't evade the
	// floor by keeping storage small).
	if err := types.RequireCloneGasFloor(code.CodeBytes, contract.StorageBytes, gas); err != nil {
		return types.QueryContractGetResponse{}, err
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

	// Arguments travel in the message body as the same {name,type,value}
	// field-array format used for message-body fields generally, with
	// positional names "arg0", "arg1", … matching the compiler's
	// getter-parameter binding (IRExprMsgField) — so a getter can accept any
	// number of typed arguments, not just one squeezed into query_id.
	body, err := req.EncodeMessageBody()
	if err != nil {
		return types.QueryContractGetResponse{}, err
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
			Body:     body,
			GasLimit: gas,
		},
		// Reuses the SAME gs snapshot already read above for the primary
		// target, so a nested externalGet() call sees one consistent
		// point-in-time view for the whole query (design doc §6.8 point 3).
		ExternalGetResolver: newExternalGetResolver(gs.State.Contracts, gs.State.Codes),
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

// newExternalGetResolver builds an avm.ExternalGetResolver (design doc §6,
// §6.8) closed over the given contract/code slices -- a pure lookup, never a
// Runner.Run() caller (avm.go itself owns the entire nested-Run() control
// flow; see the ExternalGetResolver doc comment in avm.go for why). Callers
// choose exactly which point-in-time snapshot to close over:
//   - a mutating method with no in-flight scratch copy yet
//     (executeContract, before its own `next` is built) passes
//     k.genesis.State.{Contracts,Codes} directly;
//   - a mutating method that already has a `next` scratch copy with earlier
//     mutations folded in (ReceiveInternalMessage) passes
//     next.State.{Contracts,Codes} -- NEVER a fresh k.snapshotGenesis(),
//     which would silently read pre-delivery state and contradict whatever
//     this same delivery already applied (design doc §6.7(e));
//   - a read-only query (ContractGet) passes the same gs.State.{Contracts,
//     Codes} already snapshotted once at the top of the query, so a nested
//     external-get sees the identical point-in-time view as the primary
//     target, not a second, later, potentially different read.
//
// Mirrors ContractGet's own lookup steps exactly (find contract, lifecycle
// gate, find code, code+storage-clone gas floor, decode module, decode
// storage -- the floor runs AFTER findCode/BEFORE either decode, since
// findCode is a cheap metadata lookup that makes CodeRecord.CodeBytes
// available for the floor without paying for any decode first) but returns
// the decoded (Module, Storage) instead of running them, so avm.go can
// charge the byte-proportional decode cost itself and drive the nested
// Run() with a depth cap it fully controls.
func newExternalGetResolver(contracts []types.Contract, codes []types.CodeRecord) avm.ExternalGetResolver {
	return func(targetAddress string, gasBudget uint64) (avm.Module, avm.Storage, bool, error) {
		contract, found := findContract(contracts, targetAddress)
		if !found {
			return avm.Module{}, nil, false, nil
		}
		if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionQuery); err != nil {
			return avm.Module{}, nil, false, err
		}
		// findCode moved ahead of the gas floor check (it used to run
		// after): findCode is a cheap metadata lookup, not a decode, so
		// there is no cost reason to defer it past the floor check, and
		// doing so is what makes code.CodeBytes available for the floor
		// below without paying for any decode first.
		code, found := findCode(codes, contract.CodeID)
		if !found {
			return avm.Module{}, nil, false, nil
		}
		// Phase H parity + code-size closure: reject a near-zero remaining
		// budget against a target with large storage OR large bytecode
		// before paying decodeContractSnapshot's/avm.DecodeModule's O(size)
		// cost -- the same guard ContractGet itself applies at
		// contract_get.go:39-61, extended (types.RequireCloneGasFloor, not
		// the storage-only RequireStorageCloneGasFloor) so a target with
		// near-empty storage but bytecode near Params.MaxCodeBytes can't
		// evade the floor: a storage-only check would let gasBudget=1
		// through no matter how large loadAVMModule's decode below is about
		// to be.
		if err := types.RequireCloneGasFloor(code.CodeBytes, contract.StorageBytes, gasBudget); err != nil {
			return avm.Module{}, nil, false, err
		}
		module, executable, err := loadAVMModule(code)
		if err != nil {
			return avm.Module{}, nil, false, err
		}
		if !executable {
			return avm.Module{}, nil, false, nil
		}
		storage, decodable, err := decodeContractSnapshot(contract.Data)
		if err != nil {
			return avm.Module{}, nil, false, err
		}
		if !decodable {
			return avm.Module{}, nil, false, nil
		}
		return module, storage, true, nil
	}
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
