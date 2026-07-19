package compiler

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
)

type codecFieldValue struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

func (c Codec) EncodeDefaults() ([]byte, error) {
	values := make([]codecFieldValue, 0, len(c.Fields))
	for _, field := range c.Fields {
		value, err := defaultValueForField(field)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		values = append(values, codecFieldValue{Name: field.Name, Type: field.Type.String(), Value: encoded})
	}
	out, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	if c.MaxBytes > 0 && len(out) > c.MaxBytes {
		return nil, fmt.Errorf("encoded default payload for %q is %d bytes, exceeds limit %d", c.Name, len(out), c.MaxBytes)
	}
	return out, nil
}

func (c Codec) Encode(value any) ([]byte, error) {
	if value == nil {
		return c.EncodeDefaults()
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return c.EncodeDefaults()
		}
		rv = rv.Elem()
	}
	values := make([]codecFieldValue, 0, len(c.Fields))
	for _, field := range c.Fields {
		v, ok := extractFieldValue(rv, field.Name)
		if !ok {
			return nil, fmt.Errorf("missing field %q", field.Name)
		}
		encoded, err := canonicalCodecValue(field.Type, v)
		if err != nil {
			return nil, fmt.Errorf("encode field %q: %w", field.Name, err)
		}
		raw, err := json.Marshal(encoded)
		if err != nil {
			return nil, err
		}
		values = append(values, codecFieldValue{Name: field.Name, Type: field.Type.String(), Value: raw})
	}
	out, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	if c.MaxBytes > 0 && len(out) > c.MaxBytes {
		return nil, fmt.Errorf("encoded payload for %q is %d bytes, exceeds limit %d", c.Name, len(out), c.MaxBytes)
	}
	return out, nil
}

func (c Codec) Decode(data []byte, out any) error {
	var values []codecFieldValue
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	if len(values) != len(c.Fields) {
		return fmt.Errorf("decode field count mismatch: expected %d, got %d", len(c.Fields), len(values))
	}
	if out == nil {
		return nil
	}
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("decode target must be a non-nil pointer")
	}
	rv = rv.Elem()
	if rv.Kind() == reflect.Map {
		if rv.IsNil() {
			rv.Set(reflect.MakeMap(rv.Type()))
		}
		for i, item := range values {
			field := c.Fields[i]
			if item.Name != field.Name {
				return fmt.Errorf("decode field %d name mismatch: expected %q, got %q", i, field.Name, item.Name)
			}
			if item.Type != field.Type.String() {
				return fmt.Errorf("decode field %d type mismatch: expected %q, got %q", i, field.Type.String(), item.Type)
			}
			elem := reflect.New(rv.Type().Elem())
			if err := assignDecodedValue(elem.Elem(), field.Type, item.Value); err != nil {
				return fmt.Errorf("decode field %q: %w", item.Name, err)
			}
			key := reflect.ValueOf(item.Name)
			value := elem.Elem()
			if !value.Type().AssignableTo(rv.Type().Elem()) {
				if value.Type().ConvertibleTo(rv.Type().Elem()) {
					value = value.Convert(rv.Type().Elem())
				} else if rv.Type().Elem().Kind() == reflect.Interface {
					value = reflect.ValueOf(value.Interface())
				} else {
					return fmt.Errorf("decode field %q: cannot assign %s to map value %s", item.Name, value.Type(), rv.Type().Elem())
				}
			}
			rv.SetMapIndex(key, value)
		}
		return nil
	}
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("decode target must be struct or map")
	}
	for i, item := range values {
		field := c.Fields[i]
		if item.Name != field.Name {
			return fmt.Errorf("decode field %d name mismatch: expected %q, got %q", i, field.Name, item.Name)
		}
		if item.Type != field.Type.String() {
			return fmt.Errorf("decode field %d type mismatch: expected %q, got %q", i, field.Type.String(), item.Type)
		}
		decodedField := rv.FieldByNameFunc(func(name string) bool { return strings.EqualFold(name, item.Name) })
		if !decodedField.IsValid() || !decodedField.CanSet() {
			return fmt.Errorf("decode field %q: cannot set target field", item.Name)
		}
		if err := assignDecodedValue(decodedField, field.Type, item.Value); err != nil {
			return fmt.Errorf("decode field %q: %w", item.Name, err)
		}
	}
	return nil
}

func defaultValueForField(field CodecField) (any, error) {
	if field.Default.Kind != "" {
		return canonicalExprValue(field.Default)
	}
	return zeroValueForType(field.Type), nil
}

func canonicalCodecTypeName(name string) string {
	if canonical, ok := canonicalBareIntegerTypeName(name); ok {
		return canonical
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// financialFieldSpec names one field of a Stage-3 financial struct type, in
// the exact declaration order from examples/avm/finance/finance_types.atlx.
type financialFieldSpec struct {
	Name string
	Type TypeRef
}

// financialStructFieldSpecs is the closed ABI registry for the Stage-3
// financial struct types (BasisPoints/Ratio256/Decimal256/Decimal128/
// SignedDecimal256/SignedDecimal128). Registering these explicitly closes the
// codec gap where an unrecognized struct type name fell through to the
// generic/zero default (docs/architecture/avm-financial-abi.md documents the
// wire shape): encode/decode of these specific names now goes through
// canonicalFinancialStructValue / assignFinancialStructValue, which enforce
// the exact field set (name-keyed, matching the TagMap wire shape used by
// avm.CanonicalEncode for value structs) instead of accepting or silently
// zero-defaulting an arbitrary/malformed shape.
var financialStructFieldSpecs = map[string][]financialFieldSpec{
	"basispoints":      {{Name: "bps", Type: TypeRef{Name: "uint256"}}},
	"ratio256":         {{Name: "num", Type: TypeRef{Name: "uint256"}}, {Name: "den", Type: TypeRef{Name: "uint256"}}},
	"decimal256":       {{Name: "raw", Type: TypeRef{Name: "uint256"}}},
	"decimal128":       {{Name: "raw", Type: TypeRef{Name: "uint128"}}},
	"signeddecimal256": {{Name: "raw", Type: TypeRef{Name: "int256"}}},
	"signeddecimal128": {{Name: "raw", Type: TypeRef{Name: "int128"}}},
}

func zeroValueForType(typ TypeRef) any {
	if typ.Optional {
		return nil
	}
	canonical := canonicalCodecTypeName(typ.Name)
	if spec, ok := financialStructFieldSpecs[canonical]; ok {
		out := make(map[string]any, len(spec))
		for _, field := range spec {
			out[field.Name] = zeroValueForType(field.Type)
		}
		return out
	}
	switch canonical {
	case "bool":
		return false
	case "u2", "u4", "u8", "u16", "u32", "u64", "u128", "u256", "uint2", "uint4", "uint8", "uint16", "uint32", "uint64", "uint128", "uint256", "i2", "i4", "i8", "i16", "i32", "i64", "i128", "i256", "int2", "int4", "int8", "int16", "int32", "int64", "int128", "int256", "coins", "timestamp":
		return 0
	case "bytes", "hash", "hash32":
		return ""
	case "string", "address":
		return ""
	case "chunk", "code":
		return zeroCodeSnapshot()
	default:
		return map[string]any{}
	}
}

func canonicalExprValue(expr Expr) (any, error) {
	switch expr.Kind {
	case ExprNumber:
		return parseUintLiteral(expr.Text)
	case ExprString:
		return expr.Text, nil
	case ExprBytes:
		root, err := avm.ToChunkPayload(expr.Bytes, chunk.TypeNormal)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"hex":    hex.EncodeToString(expr.Bytes),
			"base64": base64.StdEncoding.EncodeToString(expr.Bytes),
			"hash":   fmt.Sprintf("%x", root.Hash()),
			"chunks": chunk.RenderSource(root),
		}, nil
	case ExprBool:
		return expr.Bool, nil
	case ExprIdent:
		switch strings.ToLower(expr.Text) {
		case "nil", "null":
			return nil, nil
		default:
			return expr.Text, nil
		}
	case ExprPath:
		return strings.Join(expr.Path, "."), nil
	case ExprCall:
		if snapshot, ok, err := canonicalChunkLikeExprValue(expr); err != nil {
			return nil, err
		} else if ok {
			return snapshot, nil
		}
		if hashText, ok, err := canonicalChunkLikeHashExprValue(expr); err != nil {
			return nil, err
		} else if ok {
			return hashText, nil
		}
		if strings.EqualFold(expr.Text, "aet") && len(expr.Args) == 1 {
			if expr.Args[0].Kind != ExprString {
				return nil, fmt.Errorf("aet literal must use a string argument")
			}
			value, err := parseAetLiteral(expr.Args[0].Text)
			if err != nil {
				return nil, err
			}
			return value, nil
		}
		if callNameIs(expr, "address") && len(expr.Args) == 1 {
			if expr.Args[0].Kind != ExprString {
				return nil, fmt.Errorf("address literal must use a string argument")
			}
			return parseAddressLiteral(expr.Args[0].Text)
		}
		args := make([]any, 0, len(expr.Args))
		for _, arg := range expr.Args {
			v, err := canonicalExprValue(arg)
			if err != nil {
				return nil, err
			}
			args = append(args, v)
		}
		return map[string]any{"call": expr.Text, "args": args}, nil
	case ExprStruct:
		fields := make(map[string]any, len(expr.Fields))
		for _, field := range expr.Fields {
			v, err := canonicalExprValue(field.Value)
			if err != nil {
				return nil, err
			}
			fields[field.Name] = v
		}
		if expr.Text != "" {
			return map[string]any{"type": expr.Text, "fields": fields}, nil
		}
		return fields, nil
	case ExprBinary:
		left, err := canonicalExprValue(*expr.Left)
		if err != nil {
			return nil, err
		}
		right, err := canonicalExprValue(*expr.Right)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": expr.Op, "left": left, "right": right}, nil
	default:
		return nil, fmt.Errorf("unsupported expression kind %q", expr.Kind)
	}
}

func canonicalChunkLikeExprValue(expr Expr) (map[string]any, bool, error) {
	data, ok, err := chunkLikeExprBytes(expr)
	if err != nil || !ok {
		return nil, ok, err
	}
	root, err := avm.ToChunkPayload(data, chunk.TypeNormal)
	if err != nil {
		return nil, false, err
	}
	return map[string]any{
		"hex":    hex.EncodeToString(data),
		"base64": base64.StdEncoding.EncodeToString(data),
		"hash":   fmt.Sprintf("%x", root.Hash()),
		"chunks": chunk.RenderSource(root),
	}, true, nil
}

func canonicalChunkLikeHashExprValue(expr Expr) (string, bool, error) {
	data, ok, err := chunkLikeExprBytes(expr)
	if err != nil || !ok {
		return "", ok, err
	}
	root, err := avm.ToChunkPayload(data, chunk.TypeNormal)
	if err != nil {
		return "", false, err
	}
	return fmt.Sprintf("%x", root.Hash()), true, nil
}

func chunkLikeExprBytes(expr Expr) ([]byte, bool, error) {
	switch expr.Kind {
	case ExprBytes:
		return append([]byte(nil), expr.Bytes...), true, nil
	case ExprCall:
		if len(expr.Path) < 2 {
			return nil, false, nil
		}
		receiver := strings.ToLower(expr.Path[0])
		method := strings.ToLower(expr.Path[len(expr.Path)-1])
		switch receiver {
		case "chunk", "code":
			switch method {
			case "fromhex":
				if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
					return nil, false, nil
				}
				data, err := hex.DecodeString(strings.TrimSpace(expr.Args[0].Text))
				if err != nil {
					return nil, false, err
				}
				return data, true, nil
			case "frombase64":
				if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
					return nil, false, nil
				}
				data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(expr.Args[0].Text))
				if err != nil {
					return nil, false, err
				}
				return data, true, nil
			case "fromchunk", "fromsegment", "fromstate":
				if len(expr.Args) != 1 {
					return nil, false, nil
				}
				return chunkLikeExprBytes(expr.Args[0])
			}
		}
	}
	return nil, false, nil
}

func canonicalCodecValue(typ TypeRef, value reflect.Value) (any, error) {
	if !value.IsValid() {
		return zeroValueForType(typ), nil
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, nil
		}
		value = value.Elem()
	}
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return zeroValueForType(typ), nil
		}
		value = value.Elem()
	}
	canonical := canonicalCodecTypeName(typ.Name)
	if spec, ok := financialStructFieldSpecs[canonical]; ok {
		return canonicalFinancialStructValue(typ.Name, spec, value)
	}
	if canonical == "map" && len(typ.Args) == 2 {
		return canonicalMapCodecValue(typ, value)
	}
	switch canonical {
	case "bool":
		return value.Bool(), nil
	case "u2", "u4", "u8", "u16", "u32", "u64", "u128", "u256", "uint2", "uint4", "uint8", "uint16", "uint32", "uint64", "uint128", "uint256", "i2", "i4", "i8", "i16", "i32", "i64", "i128", "i256", "int2", "int4", "int8", "int16", "int32", "int64", "int128", "int256", "coins", "timestamp":
		return canonicalIntegerCodecValue(typ, value)
	case "bytes", "hash", "hash32":
		switch value.Kind() {
		case reflect.Slice:
			return hex.EncodeToString(value.Bytes()), nil
		case reflect.Array:
			b := make([]byte, value.Len())
			reflect.Copy(reflect.ValueOf(b), value)
			return hex.EncodeToString(b), nil
		case reflect.String:
			return value.String(), nil
		default:
			return fmt.Sprintf("%v", value.Interface()), nil
		}
	case "string", "address":
		return value.String(), nil
	case "chunk", "code":
		return canonicalChunkLikeValue(value)
	default:
		if value.Kind() == reflect.Struct {
			return structToMap(value)
		}
		if value.Kind() == reflect.Map {
			return mapToCanonical(value)
		}
		if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
			out := make([]any, 0, value.Len())
			for i := 0; i < value.Len(); i++ {
				item, err := canonicalCodecValue(TypeRef{Name: "bytes"}, value.Index(i))
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			}
			return out, nil
		}
		return fmt.Sprintf("%v", value.Interface()), nil
	}
}

// canonicalMapCodecValue encodes a Map<K,V> field as a JSON object whose keys
// are the canonical text of the declared key type and whose values are
// encoded per the declared value type. The generic mapToCanonical fallback
// cannot serve here: it stringifies keys with %v and encodes every value as
// opaque bytes, which the runtime's message-body decoder (see
// runtimeMapFromJSONField in x/aetravm/avm/avm.go) cannot reconstruct into a
// typed map.
func canonicalMapCodecValue(typ TypeRef, value reflect.Value) (any, error) {
	if value.Kind() != reflect.Map {
		return nil, fmt.Errorf("encode %s: expected a map value, got %s", typ.String(), value.Kind())
	}
	keyType, valueType := typ.Args[0], typ.Args[1]
	out := make(map[string]any, value.Len())
	iter := value.MapRange()
	for iter.Next() {
		encodedKey, err := canonicalCodecValue(keyType, iter.Key())
		if err != nil {
			return nil, fmt.Errorf("encode %s key: %w", typ.String(), err)
		}
		// JSON object keys are always strings, so a numeric key encodes as
		// its decimal text — the form the runtime's integer decoders accept.
		keyText := fmt.Sprintf("%v", encodedKey)
		if _, dup := out[keyText]; dup {
			return nil, fmt.Errorf("encode %s: duplicate key %q", typ.String(), keyText)
		}
		encodedValue, err := canonicalCodecValue(valueType, iter.Value())
		if err != nil {
			return nil, fmt.Errorf("encode %s value for key %q: %w", typ.String(), keyText, err)
		}
		out[keyText] = encodedValue
	}
	return out, nil
}

// canonicalFinancialStructValue encodes a registered financial struct type
// (BasisPoints/Ratio256/DecimalNNN/SignedDecimalNNN) by requiring EXACTLY the
// registered field set to be present on the source value -- unlike the
// generic structToMap/mapToCanonical fallback used for arbitrary struct
// types, a missing field is rejected rather than silently encoded as absent.
func canonicalFinancialStructValue(typeName string, spec []financialFieldSpec, value reflect.Value) (any, error) {
	if value.Kind() != reflect.Struct && value.Kind() != reflect.Map {
		return nil, fmt.Errorf("encode %s: expected struct or map value, got %s", typeName, value.Kind())
	}
	out := make(map[string]any, len(spec))
	for _, field := range spec {
		fv, ok := extractFieldValue(value, field.Name)
		if !ok {
			return nil, fmt.Errorf("encode %s: missing field %q", typeName, field.Name)
		}
		encoded, err := canonicalCodecValue(field.Type, fv)
		if err != nil {
			return nil, fmt.Errorf("encode %s.%s: %w", typeName, field.Name, err)
		}
		out[field.Name] = encoded
	}
	return out, nil
}

// assignFinancialStructValue decodes a wire value for a registered financial
// struct type. The raw JSON object must contain EXACTLY the registered field
// set -- no missing field (silently zero-defaulted) and no extra/unknown
// field -- or decode is rejected deterministically. This closes the codec
// gap where an unrecognized/malformed struct payload fell through to a
// generic, unvalidated map decode.
func assignFinancialStructValue(field reflect.Value, typeName string, spec []financialFieldSpec, raw json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("decode %s: expected a JSON object: %w", typeName, err)
	}
	if len(obj) != len(spec) {
		return fmt.Errorf("decode %s: expected %d field(s), got %d", typeName, len(spec), len(obj))
	}
	for _, f := range spec {
		if _, ok := obj[f.Name]; !ok {
			return fmt.Errorf("decode %s: missing field %q", typeName, f.Name)
		}
	}
	for field.Kind() == reflect.Pointer {
		if field.IsNil() {
			if !field.CanSet() {
				return fmt.Errorf("decode %s: nil pointer target cannot be allocated", typeName)
			}
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}
	switch field.Kind() {
	case reflect.Map:
		if field.IsNil() {
			field.Set(reflect.MakeMap(field.Type()))
		}
		for _, f := range spec {
			elem := reflect.New(field.Type().Elem())
			if err := assignDecodedValue(elem.Elem(), f.Type, obj[f.Name]); err != nil {
				return fmt.Errorf("decode %s.%s: %w", typeName, f.Name, err)
			}
			v := elem.Elem()
			if !v.Type().AssignableTo(field.Type().Elem()) {
				if field.Type().Elem().Kind() == reflect.Interface {
					v = reflect.ValueOf(v.Interface())
				} else if v.Type().ConvertibleTo(field.Type().Elem()) {
					v = v.Convert(field.Type().Elem())
				} else {
					return fmt.Errorf("decode %s.%s: cannot assign %s to %s", typeName, f.Name, v.Type(), field.Type().Elem())
				}
			}
			field.SetMapIndex(reflect.ValueOf(f.Name), v)
		}
		return nil
	case reflect.Struct:
		for _, f := range spec {
			target := field.FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, f.Name) })
			if !target.IsValid() || !target.CanSet() {
				return fmt.Errorf("decode %s: target struct has no settable field %q", typeName, f.Name)
			}
			if err := assignDecodedValue(target, f.Type, obj[f.Name]); err != nil {
				return fmt.Errorf("decode %s.%s: %w", typeName, f.Name, err)
			}
		}
		return nil
	case reflect.Interface:
		built := make(map[string]any, len(spec))
		for _, f := range spec {
			elem := reflect.New(reflect.TypeOf((*any)(nil)).Elem())
			if err := assignDecodedValue(elem.Elem(), f.Type, obj[f.Name]); err != nil {
				return fmt.Errorf("decode %s.%s: %w", typeName, f.Name, err)
			}
			built[f.Name] = elem.Elem().Interface()
		}
		field.Set(reflect.ValueOf(built))
		return nil
	default:
		return fmt.Errorf("decode %s: unsupported target kind %s", typeName, field.Kind())
	}
}

// integerTypeRange returns the inclusive value range for bounded integer
// types (widths up to 64 bits). 128/256-bit and coins values are unbounded
// here and validated by their own big-integer paths.
func integerTypeRange(name string) (minVal int64, maxVal uint64, signed bool, bounded bool) {
	switch canonicalCodecTypeName(name) {
	case "u2", "uint2":
		return 0, 3, false, true
	case "u4", "uint4":
		return 0, 15, false, true
	case "u8", "uint8":
		return 0, 1<<8 - 1, false, true
	case "u16", "uint16":
		return 0, 1<<16 - 1, false, true
	case "u32", "uint32":
		return 0, 1<<32 - 1, false, true
	case "i2", "int2":
		return -2, 1, true, true
	case "i4", "int4":
		return -8, 7, true, true
	case "i8", "int8":
		return -1 << 7, 1<<7 - 1, true, true
	case "i16", "int16":
		return -1 << 15, 1<<15 - 1, true, true
	case "i32", "int32":
		return -1 << 31, 1<<31 - 1, true, true
	default:
		return 0, 0, false, false
	}
}

func checkIntegerRange(typName string, signedValue int64, unsignedValue uint64, isSigned bool) error {
	minVal, maxVal, rangeSigned, bounded := integerTypeRange(typName)
	if !bounded {
		return nil
	}
	if rangeSigned {
		if !isSigned {
			if unsignedValue > uint64(maxVal) {
				return fmt.Errorf("value %d out of range for type %q", unsignedValue, typName)
			}
			return nil
		}
		if signedValue < minVal || (signedValue >= 0 && uint64(signedValue) > maxVal) {
			return fmt.Errorf("value %d out of range for type %q", signedValue, typName)
		}
		return nil
	}
	if isSigned {
		if signedValue < 0 || uint64(signedValue) > maxVal {
			return fmt.Errorf("value %d out of range for type %q", signedValue, typName)
		}
		return nil
	}
	if unsignedValue > maxVal {
		return fmt.Errorf("value %d out of range for type %q", unsignedValue, typName)
	}
	return nil
}

func canonicalIntegerCodecValue(typ TypeRef, value reflect.Value) (any, error) {
	typName := canonicalCodecTypeName(typ.Name)
	signed := strings.HasPrefix(typName, "i") || strings.HasPrefix(typName, "int")
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if signed {
			if err := checkIntegerRange(typName, value.Int(), 0, true); err != nil {
				return nil, err
			}
			return value.Int(), nil
		}
		if value.Int() < 0 {
			return nil, fmt.Errorf("negative value for unsigned type %q", typName)
		}
		if err := checkIntegerRange(typName, 0, uint64(value.Int()), false); err != nil {
			return nil, err
		}
		return uint64(value.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if signed {
			if err := checkIntegerRange(typName, int64(value.Uint()), 0, true); err != nil {
				return nil, err
			}
			return int64(value.Uint()), nil
		}
		if err := checkIntegerRange(typName, 0, value.Uint(), false); err != nil {
			return nil, err
		}
		return value.Uint(), nil
	default:
		if value.CanInt() {
			if signed {
				return value.Int(), nil
			}
			if value.Int() < 0 {
				return nil, fmt.Errorf("negative value for unsigned type %q", typ.Name)
			}
			return uint64(value.Int()), nil
		}
		if value.CanUint() {
			if signed {
				return int64(value.Uint()), nil
			}
			return value.Uint(), nil
		}
		return fmt.Sprintf("%v", value.Interface()), nil
	}
}

func zeroCodeSnapshot() map[string]any {
	return map[string]any{
		"hex":    "",
		"base64": "",
		"hash":   "",
		"chunks": "",
	}
}

func canonicalChunkLikeValue(value reflect.Value) (any, error) {
	data, root, err := chunkLikeBytesAndRoot(value)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return zeroCodeSnapshot(), nil
	}
	return map[string]any{
		"hex":    hex.EncodeToString(data),
		"base64": base64.StdEncoding.EncodeToString(data),
		"hash":   fmt.Sprintf("%x", root.Hash()),
		"chunks": chunk.RenderSource(root),
	}, nil
}

func chunkLikeBytesAndRoot(value reflect.Value) ([]byte, *chunk.Chunk, error) {
	if !value.IsValid() {
		return nil, nil, nil
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, nil, nil
		}
		if chunkValue, ok := value.Interface().(*chunk.Chunk); ok {
			data, err := avm.FromChunkPayload(chunkValue)
			if err != nil {
				return nil, nil, err
			}
			return data, chunkValue, nil
		}
		value = value.Elem()
	}
	if value.CanInterface() {
		if chunkValue, ok := value.Interface().(chunk.Chunk); ok {
			data, err := avm.FromChunkPayload(&chunkValue)
			if err != nil {
				return nil, nil, err
			}
			return data, &chunkValue, nil
		}
		if bytesValue, ok := value.Interface().([]byte); ok {
			root, err := avm.ToChunkPayload(bytesValue, chunk.TypeNormal)
			if err != nil {
				return nil, nil, err
			}
			return append([]byte(nil), bytesValue...), root, nil
		}
		if strValue, ok := value.Interface().(string); ok {
			decoded, err := hex.DecodeString(strings.TrimSpace(strValue))
			if err != nil {
				decoded = []byte(strValue)
			}
			root, err := avm.ToChunkPayload(decoded, chunk.TypeNormal)
			if err != nil {
				return nil, nil, err
			}
			return decoded, root, nil
		}
	}
	return nil, nil, fmt.Errorf("invalid payload for chunk-like value: %s", value.Type())
}

func structToMap(value reflect.Value) (map[string]any, error) {
	out := make(map[string]any, value.NumField())
	typ := value.Type()
	for i := 0; i < value.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		encoded, err := canonicalCodecValue(TypeRef{Name: "bytes"}, value.Field(i))
		if err != nil {
			return nil, err
		}
		out[field.Name] = encoded
	}
	return out, nil
}

func mapToCanonical(value reflect.Value) (map[string]any, error) {
	out := map[string]any{}
	iter := value.MapRange()
	for iter.Next() {
		key := fmt.Sprintf("%v", iter.Key().Interface())
		encoded, err := canonicalCodecValue(TypeRef{Name: "bytes"}, iter.Value())
		if err != nil {
			return nil, err
		}
		out[key] = encoded
	}
	return out, nil
}

func extractFieldValue(rv reflect.Value, name string) (reflect.Value, bool) {
	switch rv.Kind() {
	case reflect.Struct:
		field := rv.FieldByNameFunc(func(fieldName string) bool { return strings.EqualFold(fieldName, name) })
		if field.IsValid() {
			return field, true
		}
		return reflect.Value{}, false
	case reflect.Map:
		iter := rv.MapRange()
		for iter.Next() {
			if fmt.Sprintf("%v", iter.Key().Interface()) == name {
				return iter.Value(), true
			}
		}
		return reflect.Value{}, false
	default:
		return reflect.Value{}, false
	}
}

func assignDecodedValue(field reflect.Value, typ TypeRef, raw json.RawMessage) error {
	if !field.CanSet() {
		return fmt.Errorf("field cannot be set")
	}
	if spec, ok := financialStructFieldSpecs[canonicalCodecTypeName(typ.Name)]; ok {
		return assignFinancialStructValue(field, typ.Name, spec, raw)
	}
	switch field.Kind() {
	case reflect.String:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		field.SetString(s)
		return nil
	case reflect.Bool:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return err
		}
		field.SetBool(b)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var u uint64
		if err := json.Unmarshal(raw, &u); err != nil {
			return err
		}
		field.SetUint(u)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var i int64
		if err := json.Unmarshal(raw, &i); err != nil {
			return err
		}
		field.SetInt(i)
		return nil
	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.Uint8 {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return err
			}
			// bytes/hash/hash32 values are hex-encoded on the Encode side
			// (canonicalCodecValue -> hex.EncodeToString), so decode
			// symmetrically. Treating the hex text as raw bytes -- the prior
			// behaviour -- doubled the length and reinterpreted the value as
			// the ASCII of its own hex string, so the field did not round-trip.
			decoded, err := hex.DecodeString(s)
			if err != nil {
				return fmt.Errorf("decode bytes field: %w", err)
			}
			field.SetBytes(decoded)
			return nil
		}
	case reflect.Array:
		if field.Type().Elem().Kind() == reflect.Uint8 {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return err
			}
			// Fixed-size byte arrays (e.g. hash32 -> [32]byte) are likewise
			// hex-encoded on Encode; hex-decode and length-check so the value
			// round-trips exactly.
			decoded, err := hex.DecodeString(s)
			if err != nil {
				return fmt.Errorf("decode bytes field: %w", err)
			}
			if len(decoded) != field.Len() {
				return fmt.Errorf("decode bytes field: expected %d bytes, got %d", field.Len(), len(decoded))
			}
			reflect.Copy(field, reflect.ValueOf(decoded))
			return nil
		}
	}
	if typ.Optional && raw == nil {
		field.SetZero()
		return nil
	}
	return decodeInto(raw, field.Addr().Interface())
}

func decodeInto(raw json.RawMessage, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}
