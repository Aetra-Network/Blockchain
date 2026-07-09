package compiler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Code must be constructible from hex/base64/chunk forms, storable in
// storage, and usable as the code of child contracts (the acceptance tests
// already deploy children from storage-held Code; this locks the surface
// forms themselves).
func TestCodeConstructorsCompile(t *testing.T) {
	src := `
@storage
struct Vault {
    childCode: Code
    packed: Chunk<Vault>?
}

@message(0x1401)
struct SetCode {
    blob: bytes
}

type VaultMsg = SetCode

contract CodeVault {
    storage: Vault
    incomingMessages: VaultMsg

    @store
    func Vault.load() {
        return Vault.fromChunk(contract.getData())
    }

    @store
    func Vault.save(self) {
        contract.setData(self.toChunk())
    }

    @pure
    func fixedCode() {
        return Code.fromHex("00ff10")
    }

    @pure
    func fixedCodeB64() {
        return Code.fromBase64("AP8Q")
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy VaultMsg.fromSegment(in.body)

        match (msg) {
            SetCode => {
                var st = lazy Vault.load()
                st.childCode = Code.fromChunk(in.body)
                st.save()
            }
            else => {
                assert (in.body.isEmpty()) throw 0xFFFF
            }
        }
    }
}
`
	c, err := New(Options{})
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.NoError(t, err)
}

// Strings must round-trip through message codecs and enforce the payload
// limit instead of accepting unbounded data.
func TestStringFieldsRoundTripAndLimit(t *testing.T) {
	src := `
@storage
struct Notes {
    text: string
}

@message(0x1402)
struct SetText {
    text: string
}

type NotesMsg = SetText

contract NoteBox {
    storage: Notes
    incomingMessages: NotesMsg

    @store
    func Notes.load() {
        return Notes.fromChunk(contract.getData())
    }

    @store
    func Notes.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy NotesMsg.fromSegment(in.body)

        match (msg) {
            SetText => {
                var st = lazy Notes.load()
                st.text = msg.text
                st.save()
            }
            else => {
                assert (in.body.isEmpty()) throw 0xFFFF
            }
        }
    }

    @get
    func text(): string {
        const st = lazy Notes.load()
        return st.text
    }
}
`
	c, err := New(Options{MaxPayloadBytes: 1024})
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)

	body, err := res.MessageBodies["SetText"].Encode(map[string]any{
		"text": "hello, Aetralis — юникод и кавычки \" ок",
	})
	require.NoError(t, err)
	require.NotEmpty(t, body)

	decoded := map[string]any{}
	require.NoError(t, res.MessageBodies["SetText"].Decode(body, &decoded))
	require.Equal(t, "hello, Aetralis — юникод и кавычки \" ок", decoded["text"])

	// Oversized strings must not silently pass the payload limit.
	_, err = res.MessageBodies["SetText"].Encode(map[string]any{
		"text": strings.Repeat("x", 64*1024),
	})
	require.Error(t, err)
}
