package types

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
)

func TestStakingEventGoldenForEveryType(t *testing.T) {
	actor := eventAEAddress(0x11)
	pool := eventAEAddress(0x22)
	validator := eventAEAddress(0x33)
	proof, err := BuildStakingProofMetadata(StakingProofRequest{
		Kind:		StakingProofShare,
		Height:		77,
		PoolID:		"pool-a",
		Account:	actor,
		AppHash:	"app-root-ref",
		RootHash:	"nominator-pool-root-ref",
	})
	require.NoError(t, err)

	cases := []struct {
		name	string
		in	StakingEvent
		hash	string
	}{
		{
			name:	"account activated",
			in: StakingEvent{
				Type:			EventAccountActivated,
				Actor:			actor,
				Height:			77,
				Sequence:		0,
				StateKey:		AccountActivationEventStateKey(actor),
				ProofMetadataHash:	proof.MetadataHash,
			},
			hash:	"1948638abbacfcf0db2f4e8f9bf4d1ac40d9927ab1a233e443a613c08d677ecd",
		},
		{
			name:	"pool stake deposited",
			in: StakingEvent{
				Type:			EventPoolStakeDeposited,
				Actor:			actor,
				PoolContract:		pool,
				Amount:			1_000,
				Shares:			990,
				Height:			77,
				Epoch:			3,
				Sequence:		1,
				StateKey:		PoolDepositProofStateKey("pool-a", actor),
				ProofMetadataHash:	proof.MetadataHash,
			},
			hash:	"ab6b6ebde3b77619d694b4d6b45f145aef3cad47aee1178a2a11089e78cb85f1",
		},
		{
			name:	"pool shares minted",
			in: StakingEvent{
				Type:			EventPoolSharesMinted,
				Actor:			actor,
				PoolContract:		pool,
				Shares:			990,
				Height:			77,
				Epoch:			3,
				Sequence:		2,
				StateKey:		PoolShareProofStateKey("pool-a", actor),
				ProofMetadataHash:	proof.MetadataHash,
			},
			hash:	"c1affdbf34a5f6c00fa343adce2a40c3fa1bd0cde55f390557b48e223c53a492",
		},
		{
			name:	"pool allocation updated",
			in: StakingEvent{
				Type:			EventPoolAllocationUpdated,
				Actor:			pool,
				PoolContract:		pool,
				Validator:		validator,
				Amount:			700,
				Height:			77,
				Epoch:			3,
				Sequence:		3,
				StateKey:		PoolAllocationProofStateKey("pool-a", 3),
				ProofMetadataHash:	proof.MetadataHash,
			},
			hash:	"e8e3ec6fcffac49a287b160c150830d04dfd71b20d25d75eded9d667fbca42e9",
		},
		{
			name:	"pool unbonding requested",
			in: StakingEvent{
				Type:			EventPoolUnbondingRequested,
				Actor:			actor,
				PoolContract:		pool,
				Shares:			250,
				Height:			77,
				Epoch:			3,
				Sequence:		4,
				StateKey:		string(PoolUnbondingKey("pool-a", actor, "req-1")),
				ProofMetadataHash:	proof.MetadataHash,
			},
			hash:	"53402951330f6345ca66cadc97be001cb6105b1add159a1aa1c7341a3528da05",
		},
		{
			name:	"pool unbonding completed",
			in: StakingEvent{
				Type:			EventPoolUnbondingCompleted,
				Actor:			actor,
				PoolContract:		pool,
				Amount:			245,
				Height:			77,
				Epoch:			3,
				Sequence:		5,
				StateKey:		string(PoolUnbondingKey("pool-a", actor, "req-1")),
				ProofMetadataHash:	proof.MetadataHash,
			},
			hash:	"272b13827a49967c30b835543cbf4bef688b66224b9da83777b5c5c0c134125e",
		},
		{
			name:	"pool rewards claimed",
			in: StakingEvent{
				Type:			EventPoolRewardsClaimed,
				Actor:			actor,
				PoolContract:		pool,
				Amount:			42,
				Height:			77,
				Epoch:			3,
				Sequence:		6,
				StateKey:		PoolRewardProofStateKey("pool-a", actor),
				ProofMetadataHash:	proof.MetadataHash,
			},
			hash:	"360e07160ae624eadecaa559aa1c29ddceee1ffc4662f934bb6bb24fedfa5a1b",
		},
		{
			name:	"stake reputation claimed",
			in: StakingEvent{
				Type:			EventStakeReputationClaimed,
				Actor:			actor,
				PoolContract:		pool,
				Amount:			9,
				Height:			77,
				Epoch:			3,
				Sequence:		7,
				StateKey:		StakeReputationProofStateKey(actor),
				ProofMetadataHash:	proof.MetadataHash,
			},
			hash:	"2fab48cf0e70ee174e99b759c7dca2fd2a6855481332c9633eb05725cc8c9ce0",
		},
		{
			name:	"validator registered",
			in: StakingEvent{
				Type:		EventValidatorRegistered,
				Actor:		validator,
				Validator:	validator,
				Amount:		300_000,
				Height:		77,
				Epoch:		3,
				Sequence:	8,
				StateKey:	string(ValidatorKey(validator)),
			},
			hash:	"55dab42ead708c8d1d587448188732ddab2602a5ebb62469aaee2bd76f113c7b",
		},
		{
			name:	"validator updated",
			in: StakingEvent{
				Type:		EventValidatorUpdated,
				Actor:		validator,
				Validator:	validator,
				Height:		77,
				Epoch:		3,
				Sequence:	9,
				StateKey:	string(ValidatorKey(validator)),
			},
			hash:	"6dda5b94d6a7aeacf7384bd12701b6cbc800b1d3d4a2ff1924b883aa98cf31f0",
		},
		{
			name:	"advanced stake delegated",
			in: StakingEvent{
				Type:		EventAdvancedStakeDelegated,
				Actor:		actor,
				Validator:	validator,
				Amount:		500,
				Height:		77,
				Epoch:		3,
				Sequence:	10,
				StateKey:	AdvancedStakeEventStateKey(actor, validator),
			},
			hash:	"1a2b0a7669683bc1f9476601e691d042cdb22d0352417d63133ada852cc32f6d",
		},
		{
			name:	"advanced stake undelegated",
			in: StakingEvent{
				Type:		EventAdvancedStakeUndelegated,
				Actor:		actor,
				Validator:	validator,
				Amount:		200,
				Height:		77,
				Epoch:		3,
				Sequence:	11,
				StateKey:	AdvancedStakeEventStateKey(actor, validator),
			},
			hash:	"fc55734841c6c51d2ffcaec063d5a0e14f743e242c889947a2997e5a4d788054",
		},
		{
			name:	"advanced stake redelegated",
			in: StakingEvent{
				Type:		EventAdvancedStakeRedelegated,
				Actor:		actor,
				Validator:	validator,
				Amount:		150,
				Height:		77,
				Epoch:		3,
				Sequence:	12,
				StateKey:	AdvancedStakeRedelegationEventStateKey(actor, eventAEAddress(0x44), validator),
			},
			hash:	"99acff71ec4c71050819c0b83724e28844794c2906580a0343635d7b1f07a031",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := NewStakingEvent(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.hash, event.EventHash)
			require.Equal(t, tc.in.Actor, event.Actor)
			require.Equal(t, tc.in.PoolContract, event.PoolContract)
			require.Equal(t, tc.in.Validator, event.Validator)
			require.Equal(t, tc.in.Amount, event.Amount)
			require.Equal(t, tc.in.Shares, event.Shares)
			require.Equal(t, tc.in.Height, event.Height)
			require.Equal(t, tc.in.Epoch, event.Epoch)
			require.Equal(t, tc.in.StateKey, event.StateKey)
			require.NotContains(t, fmt.Sprint(event.OrderedAttributes()), "private_key")
			require.NotContains(t, fmt.Sprint(event.OrderedAttributes()), "seed phrase")
			require.NotContains(t, fmt.Sprint(event.OrderedAttributes()), "secret")
		})
	}
}

func TestStakingReceiptEventOrderDeterministicForMultiMessageTx(t *testing.T) {
	actor := eventAEAddress(0x55)
	pool := eventAEAddress(0x66)
	first := mustEvent(t, StakingEvent{
		Type:		EventPoolStakeDeposited,
		Actor:		actor,
		PoolContract:	pool,
		Amount:		1_000,
		Shares:		1_000,
		Height:		90,
		Epoch:		4,
		Sequence:	0,
		StateKey:	PoolDepositProofStateKey("pool-b", actor),
	})
	second := mustEvent(t, StakingEvent{
		Type:		EventPoolSharesMinted,
		Actor:		actor,
		PoolContract:	pool,
		Shares:		1_000,
		Height:		90,
		Epoch:		4,
		Sequence:	1,
		StateKey:	PoolShareProofStateKey("pool-b", actor),
	})

	a, err := NewStakingReceipt("TX-ABC", 90, []StakingEvent{second, first})
	require.NoError(t, err)
	b, err := NewStakingReceipt("tx-abc", 90, []StakingEvent{first, second})
	require.NoError(t, err)

	require.Equal(t, first.EventHash, a.Events[0].EventHash)
	require.Equal(t, second.EventHash, a.Events[1].EventHash)
	require.Equal(t, b.ReceiptHash, a.ReceiptHash)
	require.Equal(t, "5afb58c0257d5b43c65fa657555682875543a262759f34f54957707938f3d94f", a.ReceiptHash)
}

func TestStakingEventsRejectSecretsAndMisplacedValidator(t *testing.T) {
	actor := eventAEAddress(0x77)
	pool := eventAEAddress(0x88)
	validator := eventAEAddress(0x99)

	_, err := NewStakingEvent(StakingEvent{
		Type:		EventPoolRewardsClaimed,
		Actor:		actor,
		PoolContract:	pool,
		Validator:	validator,
		Amount:		10,
		Height:		1,
		StateKey:	PoolRewardProofStateKey("pool-c", actor),
	})
	require.ErrorContains(t, err, "validator is only allowed")

	_, err = NewStakingEvent(StakingEvent{
		Type:		EventPoolRewardsClaimed,
		Actor:		actor,
		PoolContract:	pool,
		Amount:		10,
		Height:		1,
		StateKey:	"staking/rewards/private_key/leak",
	})
	require.ErrorContains(t, err, "secret material")

	_, err = NewStakingEvent(StakingEvent{
		Type:		EventPoolRewardsClaimed,
		Actor:		"ae1zyg3zyg3zyg3zyg3zyg3zyg3zyg3zyg3zyg3zyg3zyg3zyg3zygs4ezt5k",
		PoolContract:	pool,
		Amount:		10,
		Height:		1,
		StateKey:	PoolRewardProofStateKey("pool-c", actor),
	})
	require.ErrorContains(t, err, "AE")
}

func TestAccountActivationAndRewardClaimEventsAreStable(t *testing.T) {
	actor := eventAEAddress(0xaa)
	pool := eventAEAddress(0xbb)
	account := mustEvent(t, StakingEvent{
		Type:		EventAccountActivated,
		Actor:		actor,
		Height:		12,
		Sequence:	0,
		StateKey:	AccountActivationEventStateKey(actor),
	})
	reward := mustEvent(t, StakingEvent{
		Type:		EventPoolRewardsClaimed,
		Actor:		actor,
		PoolContract:	pool,
		Amount:		777,
		Height:		12,
		Epoch:		2,
		Sequence:	1,
		StateKey:	PoolRewardProofStateKey("pool-reward", actor),
	})

	accountAgain := mustEvent(t, account)
	rewardAgain := mustEvent(t, reward)
	require.Equal(t, account.EventHash, accountAgain.EventHash)
	require.Equal(t, reward.EventHash, rewardAgain.EventHash)
	require.Equal(t, "5cc8a8f9ae9f1bba0a9dda81aee1552aad4b5f1f9e8ff724a9c188c4762f5ca7", account.EventHash)
	require.Equal(t, "bf3f88e0e05eb1ca8b825ee47197424fc1ef03efcaba18f305b91486ab2b4b96", reward.EventHash)
}

func mustEvent(t *testing.T, event StakingEvent) StakingEvent {
	t.Helper()
	out, err := NewStakingEvent(event)
	require.NoError(t, err)
	return out
}

func eventAEAddress(fill byte) string {
	bz := make([]byte, 20)
	for i := range bz {
		bz[i] = fill
	}
	return aetraaddress.FormatAccAddress(sdk.AccAddress(bz))
}
