package txhandlers

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/sovereign-l1/l1/app/stakingpolicy"
)

func RejectDirectUserStakingDecorator(next sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if err := walkTxMessages(tx.GetMsgs(), func(msg sdk.Msg) error {
			switch msg := msg.(type) {
			case *stakingtypes.MsgDelegate:
				return stakingpolicy.ValidateDelegate(stakingpolicy.DefaultDirectDelegationPolicy(), msg)
			case *stakingtypes.MsgBeginRedelegate:
				return stakingpolicy.ValidateBeginRedelegate(stakingpolicy.DefaultDirectDelegationPolicy(), msg)
			case *stakingtypes.MsgUndelegate:
				return stakingpolicy.ValidateUndelegate(stakingpolicy.DefaultDirectDelegationPolicy(), msg)
			default:
				return nil
			}
		}); err != nil {
			return ctx, err
		}
		return next(ctx, tx, simulate)
	}
}
