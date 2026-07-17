package types

import (
	"bytes"
	"compress/gzip"
	"context"

	"github.com/cosmos/gogoproto/grpc"
	gogoproto "github.com/cosmos/gogoproto/proto"
	grpcgo "google.golang.org/grpc"
	proto2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// x/aez's Query service.
//
// Phase 2 note: the Msg service now exists and lives in tx.go. Phase 1's "no
// handler exists" is gone -- MsgUpdateRoutingTable can move the table -- so
// these queries are what make the resulting state auditable: the current table,
// the pending table, and the zone any entity resolves to.
//
// The Msg descriptors could NOT reuse buildServiceFileDescriptor below: it emits
// field-less messages and has no cosmos.msg.v1.service branch. See tx.go for why
// both are mandatory there and why neither is needed here.
//
// The descriptors below are HAND-WRITTEN, following x/config/types/service.go
// and x/contracts/types/service.go. The alternative in-tree pattern
// (x/actor-registry, via api/l1/actorregistry/v1) is buf-generated from
// proto/l1/actorregistry/v1/*.proto, and this tree has no protoc/buf toolchain
// available, so a generated Query surface could not be produced or re-verified
// here (scripts/proto/verify-generated.ps1 would flag a checked-in *.pb.go with
// no buf output). Hand-written descriptors keep x/aez buildable and verifiable
// with the Go toolchain alone.

type QueryParamsRequest struct{}
type QueryParamsResponse struct {
	Params Params `protobuf:"bytes,1,opt,name=params,proto3" json:"params"`
}

type QueryRoutingTableRequest struct {
	// Version selects a specific routing table version. Zero means "the
	// current table".
	Version uint64 `protobuf:"varint,1,opt,name=version,proto3" json:"version,omitempty"`
}
type QueryRoutingTableResponse struct {
	Version			uint64	`protobuf:"varint,1,opt,name=version,proto3" json:"version,omitempty"`
	Epoch			uint64	`protobuf:"varint,2,opt,name=epoch,proto3" json:"epoch,omitempty"`
	ActivationHeight	int64	`protobuf:"varint,3,opt,name=activation_height,proto3" json:"activation_height,omitempty"`
	// Buckets is the full bucket->zone map, index-ordered.
	Buckets		[]uint32	`protobuf:"varint,4,rep,packed,name=buckets,proto3" json:"buckets,omitempty"`
	TableHash	string		`protobuf:"bytes,5,opt,name=table_hash,proto3" json:"table_hash,omitempty"`
}

// QueryPendingRoutingTableRequest asks for the table scheduled to activate.
type QueryPendingRoutingTableRequest struct{}

// QueryPendingRoutingTableResponse reports the pending table, if any.
//
// Found is an explicit field rather than a gRPC NotFound error because "no table
// is pending" is the NORMAL state of the chain, not a failure. An error would
// make every routine poll look like a fault in logs and force callers to
// pattern-match on a status code to read a boolean.
type QueryPendingRoutingTableResponse struct {
	Found	bool	`protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
	Table	QueryRoutingTableResponse	`protobuf:"bytes,2,opt,name=table,proto3" json:"table"`
	// BlocksUntilActivation is ActivationHeight - current height. It is
	// derived, not stored: the swap is driven by the committed
	// ActivationHeight alone, and this is a convenience for operators
	// watching a scheduled swap approach.
	BlocksUntilActivation	int64	`protobuf:"varint,3,opt,name=blocks_until_activation,proto3" json:"blocks_until_activation,omitempty"`
}

type QueryZonesRequest struct{}
type QueryZonesResponse struct {
	Zones []QueryZone `protobuf:"bytes,1,rep,name=zones,proto3" json:"zones"`
}
type QueryZone struct {
	ID	uint32	`protobuf:"varint,1,opt,name=id,proto3" json:"id,omitempty"`
	Kind	string	`protobuf:"bytes,2,opt,name=kind,proto3" json:"kind,omitempty"`
}

// QueryZoneOfRequest asks which zone an entity resolves to.
type QueryZoneOfRequest struct {
	// Kind is an EntityKind: "address", "contract" or "name".
	Kind	string	`protobuf:"bytes,1,opt,name=kind,proto3" json:"kind,omitempty"`
	// Entity is the address in either encoding ("AE..." or "ae1..."), or a
	// normalized FQDN when Kind is "name".
	Entity	string	`protobuf:"bytes,2,opt,name=entity,proto3" json:"entity,omitempty"`
}
type QueryZoneOfResponse struct {
	Zone		uint32	`protobuf:"varint,1,opt,name=zone,proto3" json:"zone,omitempty"`
	Namespace	string	`protobuf:"bytes,2,opt,name=namespace,proto3" json:"namespace,omitempty"`
	// Pinned reports that the entity is Core-pinned and bypassed the table.
	Pinned	bool	`protobuf:"varint,3,opt,name=pinned,proto3" json:"pinned,omitempty"`
	// Hashed reports that the bucket hash was entered. False for pinned
	// entities.
	Hashed		bool	`protobuf:"varint,4,opt,name=hashed,proto3" json:"hashed,omitempty"`
	Bucket		uint32	`protobuf:"varint,5,opt,name=bucket,proto3" json:"bucket,omitempty"`
	TableVersion	uint64	`protobuf:"varint,6,opt,name=table_version,proto3" json:"table_version,omitempty"`
}

type QueryServer interface {
	Params(context.Context, *QueryParamsRequest) (*QueryParamsResponse, error)
	RoutingTable(context.Context, *QueryRoutingTableRequest) (*QueryRoutingTableResponse, error)
	PendingRoutingTable(context.Context, *QueryPendingRoutingTableRequest) (*QueryPendingRoutingTableResponse, error)
	Zones(context.Context, *QueryZonesRequest) (*QueryZonesResponse, error)
	ZoneOf(context.Context, *QueryZoneOfRequest) (*QueryZoneOfResponse, error)
}

func RegisterQueryServer(s grpc.Server, srv QueryServer) {
	s.RegisterService(&Query_serviceDesc, srv)
}

type serviceCall func(context.Context, interface{}, interface{}) (interface{}, error)

var Query_serviceDesc = grpcgo.ServiceDesc{
	ServiceName:	"l1.aez.v1.Query",
	HandlerType:	(*QueryServer)(nil),
	Methods: []grpcgo.MethodDesc{
		methodDesc("Params", serviceHandler("Params", func() interface{} { return new(QueryParamsRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).Params(ctx, req.(*QueryParamsRequest))
		})),
		methodDesc("RoutingTable", serviceHandler("RoutingTable", func() interface{} { return new(QueryRoutingTableRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).RoutingTable(ctx, req.(*QueryRoutingTableRequest))
		})),
		methodDesc("PendingRoutingTable", serviceHandler("PendingRoutingTable", func() interface{} { return new(QueryPendingRoutingTableRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).PendingRoutingTable(ctx, req.(*QueryPendingRoutingTableRequest))
		})),
		methodDesc("Zones", serviceHandler("Zones", func() interface{} { return new(QueryZonesRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).Zones(ctx, req.(*QueryZonesRequest))
		})),
		methodDesc("ZoneOf", serviceHandler("ZoneOf", func() interface{} { return new(QueryZoneOfRequest) }, func(ctx context.Context, srv interface{}, req interface{}) (interface{}, error) {
			return srv.(QueryServer).ZoneOf(ctx, req.(*QueryZoneOfRequest))
		})),
	},
	Metadata:	"l1/aez/v1/query.proto",
}

func methodDesc(name string, handler grpcgo.MethodHandler) grpcgo.MethodDesc {
	return grpcgo.MethodDesc{MethodName: name, Handler: handler}
}

func serviceHandler(method string, newReq func() interface{}, call serviceCall) grpcgo.MethodHandler {
	return func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpcgo.UnaryServerInterceptor) (interface{}, error) {
		req := newReq()
		if err := dec(req); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return call(ctx, srv, req)
		}
		return interceptor(ctx, req, &grpcgo.UnaryServerInfo{Server: srv, FullMethod: method}, func(ctx context.Context, request interface{}) (interface{}, error) {
			return call(ctx, srv, request)
		})
	}
}

func init() {
	registerServiceTypes()
	gogoproto.RegisterFile("l1/aez/v1/query.proto", buildServiceFileDescriptor("l1/aez/v1/query.proto", "l1.aez.v1", "Query",
		[]string{
			"QueryParamsRequest", "QueryParamsResponse",
			"QueryRoutingTableRequest", "QueryRoutingTableResponse",
			"QueryPendingRoutingTableRequest", "QueryPendingRoutingTableResponse",
			"QueryZonesRequest", "QueryZonesResponse", "QueryZone",
			"QueryZoneOfRequest", "QueryZoneOfResponse",
		},
		[][3]string{
			{"Params", "QueryParamsRequest", "QueryParamsResponse"},
			{"RoutingTable", "QueryRoutingTableRequest", "QueryRoutingTableResponse"},
			{"PendingRoutingTable", "QueryPendingRoutingTableRequest", "QueryPendingRoutingTableResponse"},
			{"Zones", "QueryZonesRequest", "QueryZonesResponse"},
			{"ZoneOf", "QueryZoneOfRequest", "QueryZoneOfResponse"},
		}))
}

func buildServiceFileDescriptor(path, pkg, service string, messageNames []string, methods [][3]string) []byte {
	messages := make([]*descriptorpb.DescriptorProto, 0, len(messageNames))
	for _, name := range messageNames {
		messages = append(messages, &descriptorpb.DescriptorProto{Name: stringPtr(name)})
	}
	md := make([]*descriptorpb.MethodDescriptorProto, 0, len(methods))
	for _, method := range methods {
		md = append(md, &descriptorpb.MethodDescriptorProto{Name: stringPtr(method[0]), InputType: stringPtr("." + pkg + "." + method[1]), OutputType: stringPtr("." + pkg + "." + method[2])})
	}
	svc := &descriptorpb.ServiceDescriptorProto{Name: stringPtr(service), Method: md}
	fd := &descriptorpb.FileDescriptorProto{Name: stringPtr(path), Package: stringPtr(pkg), Syntax: stringPtr("proto3"), MessageType: messages, Service: []*descriptorpb.ServiceDescriptorProto{svc}}
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

func stringPtr(value string) *string	{ return &value }

func registerServiceTypes() {
	for _, item := range []struct {
		msg	gogoproto.Message
		name	string
	}{
		{(*QueryParamsRequest)(nil), "l1.aez.v1.QueryParamsRequest"},
		{(*QueryParamsResponse)(nil), "l1.aez.v1.QueryParamsResponse"},
		{(*QueryRoutingTableRequest)(nil), "l1.aez.v1.QueryRoutingTableRequest"},
		{(*QueryRoutingTableResponse)(nil), "l1.aez.v1.QueryRoutingTableResponse"},
		{(*QueryPendingRoutingTableRequest)(nil), "l1.aez.v1.QueryPendingRoutingTableRequest"},
		{(*QueryPendingRoutingTableResponse)(nil), "l1.aez.v1.QueryPendingRoutingTableResponse"},
		{(*QueryZonesRequest)(nil), "l1.aez.v1.QueryZonesRequest"},
		{(*QueryZonesResponse)(nil), "l1.aez.v1.QueryZonesResponse"},
		{(*QueryZone)(nil), "l1.aez.v1.QueryZone"},
		{(*QueryZoneOfRequest)(nil), "l1.aez.v1.QueryZoneOfRequest"},
		{(*QueryZoneOfResponse)(nil), "l1.aez.v1.QueryZoneOfResponse"},
	} {
		gogoproto.RegisterType(item.msg, item.name)
	}
}

func (m *QueryParamsRequest) Reset()		{ *m = QueryParamsRequest{} }
func (m *QueryParamsResponse) Reset()		{ *m = QueryParamsResponse{} }
func (m *QueryRoutingTableRequest) Reset()	{ *m = QueryRoutingTableRequest{} }
func (m *QueryRoutingTableResponse) Reset()	{ *m = QueryRoutingTableResponse{} }
func (m *QueryPendingRoutingTableRequest) Reset() {
	*m = QueryPendingRoutingTableRequest{}
}
func (m *QueryPendingRoutingTableResponse) Reset() {
	*m = QueryPendingRoutingTableResponse{}
}
func (m *QueryZonesRequest) Reset()		{ *m = QueryZonesRequest{} }
func (m *QueryZonesResponse) Reset()		{ *m = QueryZonesResponse{} }
func (m *QueryZone) Reset()			{ *m = QueryZone{} }
func (m *QueryZoneOfRequest) Reset()		{ *m = QueryZoneOfRequest{} }
func (m *QueryZoneOfResponse) Reset()		{ *m = QueryZoneOfResponse{} }

func (m *QueryParamsRequest) String() string		{ return gogoproto.CompactTextString(m) }
func (m *QueryParamsResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *QueryRoutingTableRequest) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryRoutingTableResponse) String() string	{ return gogoproto.CompactTextString(m) }
func (m *QueryPendingRoutingTableRequest) String() string {
	return gogoproto.CompactTextString(m)
}
func (m *QueryPendingRoutingTableResponse) String() string {
	return gogoproto.CompactTextString(m)
}
func (m *QueryZonesRequest) String() string		{ return gogoproto.CompactTextString(m) }
func (m *QueryZonesResponse) String() string		{ return gogoproto.CompactTextString(m) }
func (m *QueryZone) String() string			{ return gogoproto.CompactTextString(m) }
func (m *QueryZoneOfRequest) String() string		{ return gogoproto.CompactTextString(m) }
func (m *QueryZoneOfResponse) String() string		{ return gogoproto.CompactTextString(m) }

func (*QueryParamsRequest) ProtoMessage()		{}
func (*QueryParamsResponse) ProtoMessage()		{}
func (*QueryRoutingTableRequest) ProtoMessage()			{}
func (*QueryRoutingTableResponse) ProtoMessage()		{}
func (*QueryPendingRoutingTableRequest) ProtoMessage()		{}
func (*QueryPendingRoutingTableResponse) ProtoMessage()	{}
func (*QueryZonesRequest) ProtoMessage()		{}
func (*QueryZonesResponse) ProtoMessage()		{}
func (*QueryZone) ProtoMessage()			{}
func (*QueryZoneOfRequest) ProtoMessage()		{}
func (*QueryZoneOfResponse) ProtoMessage()		{}
