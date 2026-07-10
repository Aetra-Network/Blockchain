package compiler

import "testing"

// TestBuildMessageAmountForms pins the three accepted spellings of a
// buildMessage `amount`: the aet("…") helper (a compile-time AET→naet literal),
// a bare naet integer, and a variable holding a naet value. All lower to the
// same runtime coin amount.
func TestBuildMessageAmountForms(t *testing.T) {
	base := func(amount string) string {
		return `
@storage
struct Storage { x: uint64 }
@message(0x1) struct M {}
type InternalMsg = M
type ExternalMsg = M
contract C {
    storage: Storage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg
    @store func Storage.load() { return Storage.fromChunk(contract.getData()) }
    @store func Storage.save(self) { contract.setData(self.toChunk()) }
    @internal func onInternalMessage(in: InMessage) {
        var amt = 6000000
        const out = buildMessage({ receiver: getAddress(), amount: ` + amount + `, body: M {} })
        out.send()
    }
    @external func onExternalMessage(inMsg: Segment) {}
}`
	}
	cases := map[string]string{
		"aet literal":  `aet("0.04")`,
		"naet integer": `6000000`,
		"variable":     `amt`,
	}
	for name, amount := range cases {
		t.Run(name, func(t *testing.T) {
			c, err := New(DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Compile([]byte(base(amount))); err != nil {
				t.Fatalf("amount %q failed to compile: %v", amount, err)
			}
		})
	}
}
