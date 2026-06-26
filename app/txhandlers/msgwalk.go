package txhandlers

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

type nestedMsgProvider interface {
	GetMessages() ([]sdk.Msg, error)
}

type signerProvider interface {
	GetSigners() []sdk.AccAddress
}

func walkTxMessages(msgs []sdk.Msg, visit func(sdk.Msg) error) error {
	for _, msg := range msgs {
		if err := visit(msg); err != nil {
			return err
		}

		nested, ok := msg.(nestedMsgProvider)
		if !ok {
			continue
		}

		children, err := nested.GetMessages()
		if err != nil {
			return err
		}
		if err := walkTxMessages(children, visit); err != nil {
			return err
		}
	}

	return nil
}

func collectTxSigners(tx sdk.Tx) ([][]byte, error) {
	signerSet := make(map[string]struct{})
	signers := make([][]byte, 0)
	addSigner := func(signer []byte) {
		if len(signer) == 0 {
			return
		}
		key := string(signer)
		if _, found := signerSet[key]; found {
			return
		}
		signerSet[key] = struct{}{}
		signers = append(signers, signer)
	}

	if sigTx, ok := tx.(authsigning.SigVerifiableTx); ok {
		topLevelSigners, err := sigTx.GetSigners()
		if err != nil {
			return nil, err
		}
		for _, signer := range topLevelSigners {
			addSigner(signer)
		}
	}

	if err := walkTxMessages(tx.GetMsgs(), func(msg sdk.Msg) error {
		provider, ok := msg.(signerProvider)
		if !ok {
			return nil
		}
		for _, signer := range provider.GetSigners() {
			addSigner(signer)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return signers, nil
}
