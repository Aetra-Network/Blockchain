package stdlib

import (
	"errors"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/messageabi"
)

type MessageBuilder struct {
	msg messageabi.Message
	err error
}

func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{msg: messageabi.Message{Kind: messageabi.KindExternal}}
}

func (b *MessageBuilder) Kind(kind messageabi.Kind) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.Kind = kind
	return b
}

func (b *MessageBuilder) External() *MessageBuilder { return b.Kind(messageabi.KindExternal) }
func (b *MessageBuilder) Internal() *MessageBuilder { return b.Kind(messageabi.KindInternal) }
func (b *MessageBuilder) Bounced() *MessageBuilder  { return b.Kind(messageabi.KindBounced) }

func (b *MessageBuilder) Opcode(opcode uint64) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.Opcode = opcode
	return b
}

func (b *MessageBuilder) QueryID(queryID uint64) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.QueryID = queryID
	return b
}

func (b *MessageBuilder) Sender(addr Address) *MessageBuilder {
	if b.err != nil {
		return b
	}
	pair, err := addr.MessagePair()
	if err != nil {
		b.err = err
		return b
	}
	b.msg.Sender = pair
	return b
}

func (b *MessageBuilder) Destination(addr Address) *MessageBuilder {
	if b.err != nil {
		return b
	}
	pair, err := addr.MessagePair()
	if err != nil {
		b.err = err
		return b
	}
	b.msg.Destination = pair
	return b
}

func (b *MessageBuilder) ValueNAET(value uint64) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.ValueNAET = value
	return b
}

func (b *MessageBuilder) Bounce(enabled bool) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.Bounce = enabled
	return b
}

func (b *MessageBuilder) Deadline(block uint64) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.DeadlineBlock = block
	return b
}

func (b *MessageBuilder) GasLimit(limit uint64) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.GasLimit = limit
	return b
}

func (b *MessageBuilder) Body(body []byte) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.Body = append([]byte(nil), body...)
	return b
}

func (b *MessageBuilder) StateInit(init []byte) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.StateInit = append([]byte(nil), init...)
	return b
}

func (b *MessageBuilder) Metadata(metadata []byte) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.Metadata = append([]byte(nil), metadata...)
	return b
}

func (b *MessageBuilder) Signature(signature []byte) *MessageBuilder {
	if b.err != nil {
		return b
	}
	b.msg.Signature = append([]byte(nil), signature...)
	return b
}

func (b *MessageBuilder) Build() (messageabi.Message, error) {
	if b.err != nil {
		return messageabi.Message{}, b.err
	}
	if b.msg.GasLimit == 0 {
		return messageabi.Message{}, errors.New("message gas limit must be positive")
	}
	if b.msg.Sender.Raw == "" || b.msg.Destination.Raw == "" {
		return messageabi.Message{}, errors.New("message sender and destination are required")
	}
	return b.msg.Clone(), nil
}

func (a Address) MessagePair() (messageabi.AddressPair, error) {
	if len(a.raw) == 0 {
		return messageabi.AddressPair{}, errors.New("address is empty")
	}
	user, err := aetraaddress.FormatUserFriendly(a.raw)
	if err != nil {
		return messageabi.AddressPair{}, err
	}
	return messageabi.AddressPair{User: user, Raw: aetraaddress.Format(a.raw)}, nil
}

