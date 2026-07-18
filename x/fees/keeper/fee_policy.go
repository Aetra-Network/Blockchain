package keeper

import (
	"context"
	"encoding/binary"

	sdkmath "cosmossdk.io/math"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/observability"
	"github.com/sovereign-l1/l1/x/fees/types"
)

// stateCreatingMsgTypes contains message type URLs whose execution creates or
// expands chain state. Transactions carrying at least one such message should
// incur the additional StorageRentSideEffects fee.
var stateCreatingMsgTypes = map[string]bool{
	"/l1.contracts.v1.MsgStoreCode":		true,
	"/l1.contracts.v1.MsgDeployContract":		true,
	"/l1.contracts.v1.MsgExecuteExternal":		true,
	"/l1.contracts.v1.MsgExecuteInternal":		true,
	"/l1.contracts.v1.MsgSendInternalMessage":	true,
	"/l1.contracts.v1.MsgUpdateContractParams":	true,
}

func (k Keeper) ValidateTxFees(ctx context.Context, fees sdk.Coins) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	return types.ValidateFeeCoins(params, fees, true)
}

func (k Keeper) AdmitTx(ctx sdk.Context, tx sdk.FeeTx, sender sdk.AccAddress, simulate bool) (types.FeeQuote, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return types.FeeQuote{}, err
	}
	formulaParams, err := k.GetFeeFormulaParams(ctx)
	if err != nil {
		return types.FeeQuote{}, err
	}
	blockCount, err := k.getBlockTxCount(ctx)
	if err != nil {
		return types.FeeQuote{}, err
	}
	senderCount, err := k.getSenderTxCount(ctx, sender)
	if err != nil {
		return types.FeeQuote{}, err
	}
	gasConsumed := uint64(0)
	if ctx.BlockGasMeter() != nil {
		gasConsumed = ctx.BlockGasMeter().GasConsumed()
	}

	// AEZ Phase 6: resolve this tx's home zone and that zone's per-block gas cap.
	//
	// The resolver reads x/aez state (routing table + params). Doing those reads
	// on the tx's REAL metered gas meter would raise gasUsed for EVERY tx --
	// including single-zone Core txs -- which feeds the block gas meter and thus
	// the congestion bps the fees EndBlocker commits, breaking the bit-identical
	// AppHash inertness guarantee. So resolution runs on an INFINITE-meter child
	// ctx (the reads are read-only and deterministic), leaving the metered
	// gasUsed byte-for-byte unchanged -- the analogue of Phase 4's gas-free inbox
	// scan.
	//
	// A nil resolver, a nil sender, or any resolver error is NON-FATAL: the tx
	// falls through as Core (zone 0, uncapped), so a broken or disabled x/aez can
	// never reject a tx that admits today (I-23).
	zone := zoneCore
	zoneMaxGas := uint64(0)
	if k.zoneResolver != nil && sender != nil {
		// FormatUserFriendly (not FormatAccAddress) so a malformed sender is a
		// returned error, never a panic: a bad address must degrade to Core, not
		// halt the block (I-23).
		if senderText, ferr := aetraaddress.FormatUserFriendly(sender); ferr == nil {
			rc := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
			if z, zerr := k.zoneResolver.ZoneOfAddress(rc, senderText); zerr == nil {
				zone = z
				if z != zoneCore {
					zoneMaxGas, _ = k.zoneResolver.GasQuotaForZone(rc, z)
				}
			}
		}
	}

	// Per-zone gas reserved so far this block. Read ONLY for an elastic zone: a
	// Core tx performs no new fees-store read here, which -- together with the
	// elastic-only write below -- is what keeps single-zone admission
	// bit-identical (the height-keyed counter is never touched).
	zoneGasConsumed := uint64(0)
	if zone != zoneCore {
		zoneGasConsumed, err = k.getZoneGasConsumed(ctx, zone)
		if err != nil {
			return types.FeeQuote{}, err
		}
	}

	quote, err := types.ValidateAdmission(params, types.AdmissionInput{
		Fee:			tx.GetFee(),
		GasLimit:		tx.GetGas(),
		BlockGasConsumed:	gasConsumed,
		BlockTxCount:		blockCount,
		SenderTxCount:		senderCount,
		SenderStake:		sdkmath.ZeroInt(),
		ZoneID:			zone,
		ZoneGasConsumed:	zoneGasConsumed,
		ZoneMaxGas:		zoneMaxGas,
	})
	if err != nil {
		return types.FeeQuote{}, err
	}

	kvUtilizationBps := k.getKVCongestionBps(ctx, gasConsumed, tx.GetGas(), params.MaxBlockGas)

	reputationScore, reputationFound, err := k.GetReputationScore(ctx, sender)
	if err != nil {

		reputationScore = types.ReputationNeutralScore
		reputationFound = false
	}

	storageRentNaet := sdkmath.ZeroInt()
	if srDefault, parseErr := formulaParams.StorageRentSideEffectsInt(); parseErr == nil && srDefault.IsPositive() {
		// Walk nested messages too (e.g. a MsgStoreCode wrapped inside an
		// authz.MsgExec), not just the top-level envelope, so this side-effect
		// fee can't be evaded the same way the message-count cap could
		// (FINDING-013's root cause applied to this closely related check).
		hasStateCreatingMsg := false
		if err := aetraaddress.WalkMessages(tx.GetMsgs(), func(msg sdk.Msg) error {
			if stateCreatingMsgTypes[sdk.MsgTypeURL(msg)] {
				hasStateCreatingMsg = true
			}
			return nil
		}); err != nil {
			return types.FeeQuote{}, err
		}
		if hasStateCreatingMsg {
			storageRentNaet = srDefault
		}
	}

	// Compute the full deterministic fee per Requirement 1.1.
	var txSizeBytes uint64
	if txBytes := ctx.TxBytes(); len(txBytes) > 0 {
		txSizeBytes = uint64(len(txBytes))
	}
	// Count nested messages too (e.g. inside an authz.MsgExec), not just the
	// top-level envelope, so the per-message fee component actually charges
	// for every message that will execute (FINDING-013).
	msgCount, err := aetraaddress.CountMessages(tx.GetMsgs())
	if err != nil {
		return types.FeeQuote{}, err
	}

	requiredFull, err := types.ComputeFullTransferFee(
		params,
		formulaParams,
		tx.GetGas(),
		txSizeBytes,
		msgCount,
		kvUtilizationBps,
		reputationScore,
		reputationFound,
		storageRentNaet,
	)
	if err != nil {
		return types.FeeQuote{}, err
	}

	paidAmount := tx.GetFee().AmountOf(types.BondDenom)
	if paidAmount.LT(requiredFull) {
		return types.FeeQuote{}, types.ErrInvalidFee.Wrapf(
			"fee must be at least %s%s (full formula requirement), paid %s%s",
			requiredFull.String(), types.BondDenom,
			paidAmount.String(), types.BondDenom,
		)
	}

	maxFee, err := params.MaxFeeInt()
	if err != nil {
		return types.FeeQuote{}, err
	}
	if paidAmount.GT(maxFee) {
		return types.FeeQuote{}, types.ErrInvalidFee.Wrapf(
			"fee must not exceed hard cap %s%s", maxFee.String(), types.BondDenom,
		)
	}

	if !simulate {
		observability.RecordEconomicControl(
			quote.EconomicControl.InflationBps,
			quote.EconomicControl.BurnRatioBps,
			quote.EconomicControl.ValidatorFeeRatioBps,
			quote.EconomicControl.DeflationGuardActive,
			quote.EconomicControl.QueueLimited,
			quote.EconomicControl.RateLimited,
		)
		if flow, err := appparams.ComputeProtocolEconomicFlow(appparams.ProtocolEconomicFlowInput{
			Activity: appparams.ProtocolEconomicActivity{
				TxFeeNaet: quote.AcceptedFeeAmount,
			},
			BurnRatioBps:		quote.EconomicControl.BurnRatioBps,
			TreasuryRatioBps:	appparams.TreasuryFeeRatioBps,
		}); err == nil {
			observability.RecordEconomicFlow(
				flow.TotalChargesNaet.Int64(),
				flow.BurnNaet.Int64(),
				flow.TreasuryNaet.Int64(),
				flow.ValidatorRewardsNaet.Int64(),
			)
		}
		if err := k.setBlockTxCount(ctx, blockCount+1); err != nil {
			return types.FeeQuote{}, err
		}
		if err := k.setSenderTxCount(ctx, sender, senderCount+1); err != nil {
			return types.FeeQuote{}, err
		}
		// AEZ Phase 6: reserve this tx's gas LIMIT against its zone's per-block
		// budget. Reservation is by LIMIT, not actual: actual usage is unknown
		// at ante time and the global check itself projects with GasLimit, so
		// this is deterministic and strictly conservative (limits >= actual, so
		// it can only ever reject MORE, never fewer, than an actual-based cap).
		// Written ONLY for an elastic zone -- a Core tx writes nothing new, which
		// is the other half of single-zone bit-identity.
		if zone != zoneCore {
			if err := k.setZoneGasConsumed(ctx, zone, zoneGasConsumed+tx.GetGas()); err != nil {
				return types.FeeQuote{}, err
			}
		}
	}
	return quote, nil
}

func (k Keeper) TxPriority(params types.Params, paidFee sdk.Coin, requiredFee sdk.Coin, stake sdkmath.Int) (int64, error) {
	return types.PriorityScore(params, paidFee, requiredFee, stake)
}

func (k Keeper) getBlockTxCount(ctx sdk.Context) (uint64, error) {
	return k.getHeightCounter(ctx, types.BlockTxCountKey)
}

func (k Keeper) setBlockTxCount(ctx sdk.Context, count uint64) error {
	return k.setHeightCounter(ctx, types.BlockTxCountKey, count)
}

func (k Keeper) getSenderTxCount(ctx sdk.Context, sender sdk.AccAddress) (uint64, error) {
	return k.getHeightCounter(ctx, senderTxCountKey(sender))
}

func (k Keeper) setSenderTxCount(ctx sdk.Context, sender sdk.AccAddress, count uint64) error {
	return k.setHeightCounter(ctx, senderTxCountKey(sender), count)
}

func (k Keeper) getHeightCounter(ctx sdk.Context, key []byte) (uint64, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(key)
	if err != nil {
		return 0, err
	}
	if len(bz) != 16 {
		return 0, nil
	}
	height := int64(binary.BigEndian.Uint64(bz[:8]))
	if height != ctx.BlockHeight() {
		return 0, nil
	}
	return binary.BigEndian.Uint64(bz[8:]), nil
}

func (k Keeper) setHeightCounter(ctx sdk.Context, key []byte, count uint64) error {
	var bz [16]byte
	binary.BigEndian.PutUint64(bz[:8], uint64(ctx.BlockHeight()))
	binary.BigEndian.PutUint64(bz[8:], count)
	return k.storeService.OpenKVStore(ctx).Set(key, bz[:])
}

func senderTxCountKey(sender sdk.AccAddress) []byte {
	key := make([]byte, 0, len(types.SenderTxCountPrefix)+len(sender))
	key = append(key, types.SenderTxCountPrefix...)
	key = append(key, sender...)
	return key
}

// getZoneGasConsumed reads the AEZ Phase 6 per-zone gas reserved so far this
// block. It reuses the height-keyed self-resetting counter (getHeightCounter):
// a stored height that is not the current height reads as 0, so the value resets
// each block with no EndBlock write.
func (k Keeper) getZoneGasConsumed(ctx sdk.Context, zone uint32) (uint64, error) {
	return k.getHeightCounter(ctx, zoneGasConsumedKey(zone))
}

func (k Keeper) setZoneGasConsumed(ctx sdk.Context, zone uint32, gas uint64) error {
	return k.setHeightCounter(ctx, zoneGasConsumedKey(zone), gas)
}

func zoneGasConsumedKey(zone uint32) []byte {
	key := make([]byte, 0, len(types.ZoneGasConsumedPrefix)+4)
	key = append(key, types.ZoneGasConsumedPrefix...)
	var z [4]byte
	binary.BigEndian.PutUint32(z[:], zone)
	return append(key, z[:]...)
}
