package async

import "testing"

// TestDefaultParamsSupport256MessageBatch proves the default limits admit a
// 256-message transaction (1 signature + 255 NFT mints) end-to-end: per tx,
// per single execution, and within the per-block throttle.
func TestDefaultParamsSupport256MessageBatch(t *testing.T) {
	p := DefaultParams()
	if err := p.Validate(); err != nil {
		t.Fatalf("default params must validate: %v", err)
	}
	if p.MaxMessagesPerTx < 256 {
		t.Fatalf("MaxMessagesPerTx = %d, want >= 256", p.MaxMessagesPerTx)
	}
	if p.MaxEmittedMessagesPerExec < 255 {
		t.Fatalf("MaxEmittedMessagesPerExec = %d, want >= 255 so one batch handler can fan out", p.MaxEmittedMessagesPerExec)
	}
	if p.MaxMessagesPerBlock < p.MaxMessagesPerTx {
		t.Fatalf("MaxMessagesPerBlock %d must be >= MaxMessagesPerTx %d", p.MaxMessagesPerBlock, p.MaxMessagesPerTx)
	}
	// One-contract-per-NFT batch mint needs 255 deploys behind one signature.
	if p.MaxContractDeploysPerTx < 256 {
		t.Fatalf("MaxContractDeploysPerTx = %d, want >= 256 for per-item NFT batch mint", p.MaxContractDeploysPerTx)
	}
}

// TestParamsRejectUnboundedMessageCaps proves governance cannot raise the caps
// past the absolute ceilings — defense-in-depth on top of gas metering.
func TestParamsRejectUnboundedMessageCaps(t *testing.T) {
	p := DefaultParams()
	p.MaxMessagesPerTx = AbsoluteMaxMessagesPerTx + 1
	if err := p.Validate(); err == nil {
		t.Fatal("MaxMessagesPerTx above the absolute ceiling must be rejected")
	}

	p = DefaultParams()
	p.MaxMessagesPerBlock = AbsoluteMaxMessagesPerBlock + 1
	if err := p.Validate(); err == nil {
		t.Fatal("MaxMessagesPerBlock above the absolute ceiling must be rejected")
	}

	p = DefaultParams()
	p.MaxEmittedMessagesPerExec = AbsoluteMaxEmittedPerExecution + 1
	if err := p.Validate(); err == nil {
		t.Fatal("per-exec emit cap above the absolute ceiling must be rejected")
	}
}
