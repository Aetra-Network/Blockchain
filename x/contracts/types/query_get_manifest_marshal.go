package types

import (
	"fmt"
	"io"
)

// This file hand-implements the gogoproto binary wire-format methods --
// Marshal/MarshalTo/MarshalToSizedBuffer/Size/Unmarshal -- for
// QueryContractManifestRequest/QueryContractManifestResponse (types.go),
// the ContractManifest query RPC's request/response pair, following the
// exact conventions query_marshal.go/query_marshal_size.go/
// query_marshal_unmarshal.go already establish for the other x/contracts
// query messages (see query_marshal.go's own file-level doc comment). Kept
// in its own file, one message family at a time, rather than folded into
// the shared query_marshal*.go trio, since ContractManifest was added after
// those files' original message set was written.
//
// Field numbers are taken from the struct's own protobuf tags in types.go
// (CodeID=1 on the request; Found=1, ManifestBytes=2 on the response) --
// nothing here invents a new one.

// ---- QueryContractManifestRequest ----

func (m *QueryContractManifestRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractManifestRequest) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractManifestRequest) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.CodeID) > 0 {
		i -= len(m.CodeID)
		copy(dAtA[i:], m.CodeID)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeID)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

func (m *QueryContractManifestRequest) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.CodeID)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	return n
}

func (m *QueryContractManifestRequest) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowContractsQuery
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: QueryContractManifestRequest: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: QueryContractManifestRequest: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field CodeID", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowContractsQuery
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthContractsQuery
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return ErrInvalidLengthContractsQuery
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.CodeID = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipContractsQuery(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthContractsQuery
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}
	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// ---- QueryContractManifestResponse ----

func (m *QueryContractManifestResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractManifestResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractManifestResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.ManifestBytes) > 0 {
		i -= len(m.ManifestBytes)
		copy(dAtA[i:], m.ManifestBytes)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.ManifestBytes)))
		i--
		dAtA[i] = 0x12
	}
	if m.Found {
		i--
		if m.Found {
			dAtA[i] = 1
		} else {
			dAtA[i] = 0
		}
		i--
		dAtA[i] = 0x8
	}
	return len(dAtA) - i, nil
}

func (m *QueryContractManifestResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if m.Found {
		n += 2
	}
	l = len(m.ManifestBytes)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	return n
}

func (m *QueryContractManifestResponse) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowContractsQuery
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: QueryContractManifestResponse: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: QueryContractManifestResponse: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Found", wireType)
			}
			var v int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowContractsQuery
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				v |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			m.Found = bool(v != 0)
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field ManifestBytes", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowContractsQuery
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				byteLen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthContractsQuery
			}
			postIndex := iNdEx + byteLen
			if postIndex < 0 {
				return ErrInvalidLengthContractsQuery
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.ManifestBytes = append(m.ManifestBytes[:0], dAtA[iNdEx:postIndex]...)
			if m.ManifestBytes == nil {
				m.ManifestBytes = []byte{}
			}
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipContractsQuery(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthContractsQuery
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}
	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
