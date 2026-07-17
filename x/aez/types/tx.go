package types

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"

	"github.com/cosmos/gogoproto/grpc"
	gogoproto "github.com/cosmos/gogoproto/proto"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	proto2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// x/aez's Msg service. Phase 2 replaces "no handler exists" with exactly ONE
// handler, gated on the governance authority.
//
// The descriptors here are HAND-WRITTEN for the same reason service.go's are:
// this tree has no protoc/buf toolchain, so a generated Msg surface could not be
// produced or re-verified (scripts/proto/verify-generated.ps1 would flag a
// checked-in *.pb.go with no buf output).
//
// The template is x/nominator-pool/types/tx.go, NOT x/config/types/service.go.
// That choice is load-bearing. x/config's descriptor builder emits FIELD-LESS
// messages (x/config/types/service.go:199 appends a DescriptorProto with a Name
// and no Field). A field-less descriptor cannot carry a signer field, so the
// x/tx signing context has nothing to read and every such message fails with "no
// cosmos.msg.v1.signer option found" -- or worse. x/nominator-pool learned this
// LIVE: broadcasting a field-less hand-rolled Msg crashed gogoproto's Unmarshal
// on every receiving node (see app/keeperconfig/tx.go:38-45). x/aez declares its
// fields from the start rather than rediscovering that.
//
// x/aez/types/service.go's own buildServiceFileDescriptor is NOT reusable here:
// it supports neither fields nor the cosmos.msg.v1.service option. It stays as
// it is -- Query messages need neither -- and this file carries its own builder.

// MsgUpdateRoutingTable stages a new routing table for a future routing-epoch
// boundary. It does NOT apply the table: the swap happens in the BeginBlocker at
// ActivationHeight (keeper/abci.go), never inside this handler.
//
// That separation is the whole point. SetPendingRoutingTable rejects an
// ActivationHeight at or before the current height precisely because a table
// applied part-way through a block would leave that block's earlier
// transactions resolved against the old table and its later ones against the
// new -- two tables inside one height. All of block H must see exactly one
// table.
//
// Field NUMBERS MUST match the protobuf struct tags below: the signing context
// decodes the authority field straight off the wire using these numbers (see
// signing.go, and the x/nominator-pool doc comment this rule comes from).
type MsgUpdateRoutingTable struct {
	// Authority must equal Params.Prototype.Authority -- the gov module
	// account on a real network.
	Authority	string	`protobuf:"bytes,1,opt,name=authority,proto3" json:"authority,omitempty"`
	// Version must strictly exceed the current table version (I-8).
	Version	uint64	`protobuf:"varint,2,opt,name=version,proto3" json:"version,omitempty"`
	// Epoch is the routing epoch this table belongs to.
	Epoch	uint64	`protobuf:"varint,3,opt,name=epoch,proto3" json:"epoch,omitempty"`
	// ActivationHeight must be an exact routing-epoch boundary, strictly in
	// the future (I-8).
	ActivationHeight	int64	`protobuf:"varint,4,opt,name=activation_height,json=activationHeight,proto3" json:"activation_height,omitempty"`
	// Buckets is the FULL bucket->zone map and must carry exactly BucketCount
	// entries, index-ordered.
	//
	// The whole map travels, never a delta. A delta message would be smaller
	// and strictly worse: governance would be voting on a diff whose meaning
	// depends on whatever table happens to be current when it executes, so the
	// same proposal bytes could produce different layouts depending on
	// execution order. The full map means the proposal's bytes ARE the
	// resulting layout.
	Buckets	[]uint32	`protobuf:"varint,5,rep,packed,name=buckets,proto3" json:"buckets,omitempty"`
}

// MsgUpdateRoutingTableResponse reports the staged table.
//
// TableHash is DERIVED from the message's own buckets, never supplied by the
// caller: RoutingTable.TableHash is an output of ComputeTableHash, so a message
// carrying its own hash could only ever agree or disagree with its contents, and
// a "disagree" case is a failure mode with no legitimate use. Returning the
// derived hash lets a proposer confirm what actually got staged.
type MsgUpdateRoutingTableResponse struct {
	Version			uint64	`protobuf:"varint,1,opt,name=version,proto3" json:"version,omitempty"`
	Epoch			uint64	`protobuf:"varint,2,opt,name=epoch,proto3" json:"epoch,omitempty"`
	ActivationHeight	int64	`protobuf:"varint,3,opt,name=activation_height,json=activationHeight,proto3" json:"activation_height,omitempty"`
	TableHash		string	`protobuf:"bytes,4,opt,name=table_hash,json=tableHash,proto3" json:"table_hash,omitempty"`
}

// RoutingTableFromMsg converts the message into a RoutingTable with its
// canonical hash filled in, rejecting a bucket vector of the wrong length.
//
// The length check is what turns a []uint32 back into the [BucketCount]ZoneID
// array the type system elsewhere relies on for totality (I-7). It is the only
// place that conversion happens, so a short or long vector cannot reach the
// keeper.
func RoutingTableFromMsg(msg *MsgUpdateRoutingTable) (RoutingTable, error) {
	if msg == nil {
		return RoutingTable{}, ErrInvalidRoutingTable
	}
	if len(msg.Buckets) != BucketCount {
		return RoutingTable{}, fmt.Errorf("%w: table must carry exactly %d buckets, got %d", ErrInvalidRoutingTable, BucketCount, len(msg.Buckets))
	}
	var buckets [BucketCount]ZoneID
	for i := 0; i < BucketCount; i++ {
		buckets[i] = ZoneID(msg.Buckets[i])
	}
	return NewRoutingTable(msg.Version, msg.Epoch, msg.ActivationHeight, buckets), nil
}

// BucketsFromTable renders a table's bucket map onto the wire shape.
func BucketsFromTable(table RoutingTable) []uint32 {
	out := make([]uint32, 0, BucketCount)
	for i := 0; i < BucketCount; i++ {
		out = append(out, uint32(table.Buckets[i]))
	}
	return out
}

type MsgServer interface {
	UpdateRoutingTable(context.Context, *MsgUpdateRoutingTable) (*MsgUpdateRoutingTableResponse, error)
}

type UnimplementedMsgServer struct{}

func (UnimplementedMsgServer) UpdateRoutingTable(context.Context, *MsgUpdateRoutingTable) (*MsgUpdateRoutingTableResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateRoutingTable not implemented")
}

func RegisterMsgServer(s grpc.Server, srv MsgServer)	{ s.RegisterService(&Msg_serviceDesc, srv) }

var Msg_serviceDesc = grpcgo.ServiceDesc{
	ServiceName:	"l1.aez.v1.Msg",
	HandlerType:	(*MsgServer)(nil),
	Methods: []grpcgo.MethodDesc{
		{MethodName: "UpdateRoutingTable", Handler: _Msg_UpdateRoutingTable_Handler},
	},
	Streams:	[]grpcgo.StreamDesc{},
	Metadata:	"l1/aez/v1/tx.proto",
}

func _Msg_UpdateRoutingTable_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
	req := new(MsgUpdateRoutingTable)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).UpdateRoutingTable(ctx, req)
	}
	info := &grpcgo.UnaryServerInfo{Server: srv, FullMethod: "/l1.aez.v1.Msg/UpdateRoutingTable"}
	handler := func(ctx context.Context, request interface{}) (interface{}, error) {
		return srv.(MsgServer).UpdateRoutingTable(ctx, request.(*MsgUpdateRoutingTable))
	}
	return interceptor(ctx, req, info, handler)
}

func init() {
	registerTxTypes()
	gogoproto.RegisterFile("l1/aez/v1/tx.proto", fileDescriptorAEZTx)
}

var fileDescriptorAEZTx = buildAEZTxFileDescriptor()

// buildAEZTxFileDescriptor emits the tx.proto descriptor with real FIELDS and
// the cosmos.msg.v1.service option.
//
// The service option is not decoration: msgservice.RegisterMsgServiceDesc
// (called from codec.go) walks the registered file descriptor and REQUIRES the
// cosmos.msg.v1.service extension to be set to true, otherwise it rejects the
// service outright. Because the descriptor is hand-built, the extension is
// carried as an UninterpretedOption -- the same shape x/nominator-pool uses,
// which is the only in-tree hand-written Msg descriptor that actually works.
func buildAEZTxFileDescriptor() []byte {
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING
	u32 := descriptorpb.FieldDescriptorProto_TYPE_UINT32
	u64 := descriptorpb.FieldDescriptorProto_TYPE_UINT64
	i64 := descriptorpb.FieldDescriptorProto_TYPE_INT64

	messages := []*descriptorpb.DescriptorProto{
		{
			Name: txDescriptorString("MsgUpdateRoutingTable"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("authority", 1, str),
				txField("version", 2, u64),
				txField("epoch", 3, u64),
				txField("activation_height", 4, i64),
				txRepeatedField("buckets", 5, u32),
			},
		},
		{
			Name: txDescriptorString("MsgUpdateRoutingTableResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				txField("version", 1, u64),
				txField("epoch", 2, u64),
				txField("activation_height", 3, i64),
				txField("table_hash", 4, str),
			},
		},
	}
	fd := &descriptorpb.FileDescriptorProto{
		Name:		txDescriptorString("l1/aez/v1/tx.proto"),
		Package:	txDescriptorString("l1.aez.v1"),
		Syntax:		txDescriptorString("proto3"),
		MessageType:	messages,
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:	txDescriptorString("Msg"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:		txDescriptorString("UpdateRoutingTable"),
				InputType:	txDescriptorString(".l1.aez.v1.MsgUpdateRoutingTable"),
				OutputType:	txDescriptorString(".l1.aez.v1.MsgUpdateRoutingTableResponse"),
			}},
			Options: &descriptorpb.ServiceOptions{
				UninterpretedOption: []*descriptorpb.UninterpretedOption{{
					Name: []*descriptorpb.UninterpretedOption_NamePart{{
						NamePart:	txDescriptorString("cosmos.msg.v1.service"),
						IsExtension:	txDescriptorBool(true),
					}},
					IdentifierValue: txDescriptorString("true"),
				}},
			},
		}},
	}
	raw, err := proto2.Marshal(fd)
	if err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func txField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return txDescriptorField(name, number, typ, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL)
}

// txRepeatedField declares a repeated field. The Buckets vector needs
// LABEL_REPEATED: declared LABEL_OPTIONAL it would decode as a single scalar and
// silently drop 255 of the 256 assignments.
func txRepeatedField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return txDescriptorField(name, number, typ, descriptorpb.FieldDescriptorProto_LABEL_REPEATED)
}

func txDescriptorField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, label descriptorpb.FieldDescriptorProto_Label) *descriptorpb.FieldDescriptorProto {
	num := number
	fieldType := typ
	fieldLabel := label
	return &descriptorpb.FieldDescriptorProto{
		Name:	txDescriptorString(name),
		Number:	&num,
		Label:	&fieldLabel,
		Type:	&fieldType,
	}
}

func txDescriptorString(value string) *string	{ return &value }
func txDescriptorBool(value bool) *bool		{ return &value }

func registerTxTypes() {
	gogoproto.RegisterType((*MsgUpdateRoutingTable)(nil), "l1.aez.v1.MsgUpdateRoutingTable")
	gogoproto.RegisterType((*MsgUpdateRoutingTableResponse)(nil), "l1.aez.v1.MsgUpdateRoutingTableResponse")
}

func (m *MsgUpdateRoutingTable) Reset()		{ *m = MsgUpdateRoutingTable{} }
func (m *MsgUpdateRoutingTableResponse) Reset()	{ *m = MsgUpdateRoutingTableResponse{} }

func (m *MsgUpdateRoutingTable) String() string		{ return gogoproto.CompactTextString(m) }
func (m *MsgUpdateRoutingTableResponse) String() string	{ return gogoproto.CompactTextString(m) }

func (*MsgUpdateRoutingTable) ProtoMessage()		{}
func (*MsgUpdateRoutingTableResponse) ProtoMessage()	{}
