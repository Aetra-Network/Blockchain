package compiler

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
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
	return json.Marshal(values)
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
	return json.Marshal(values)
}

func (c Codec) Decode(data []byte, out any) error {
	var values []codecFieldValue
	if err := json.Unmarshal(data, &values); err != nil {
		return err
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
		for _, item := range values {
			var fieldType TypeRef
			for _, field := range c.Fields {
				if strings.EqualFold(field.Name, item.Name) {
					fieldType = field.Type
					break
				}
			}
			elem := reflect.New(rv.Type().Elem())
			if err := assignDecodedValue(elem.Elem(), fieldType, item.Value); err != nil {
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
	for _, item := range values {
		var fieldType TypeRef
		for _, field := range c.Fields {
			if strings.EqualFold(field.Name, item.Name) {
				fieldType = field.Type
				break
			}
		}
		field := rv.FieldByNameFunc(func(name string) bool { return strings.EqualFold(name, item.Name) })
		if !field.IsValid() || !field.CanSet() {
			continue
		}
		if err := assignDecodedValue(field, fieldType, item.Value); err != nil {
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

func zeroValueForType(typ TypeRef) any {
	switch strings.ToLower(typ.Name) {
	case "bool":
		return false
	case "u8", "u16", "u32", "u64", "i64", "coins":
		return 0
	case "bytes", "hash32":
		return ""
	case "string", "address":
		return ""
	default:
		if typ.Optional {
			return nil
		}
		return map[string]any{}
	}
}

func canonicalExprValue(expr Expr) (any, error) {
	switch expr.Kind {
	case ExprNumber:
		return strconv.ParseUint(expr.Text, 10, 64)
	case ExprString:
		return expr.Text, nil
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
		args := make([]any, 0, len(expr.Args))
		for _, arg := range expr.Args {
			v, err := canonicalExprValue(arg)
			if err != nil {
				return nil, err
			}
			args = append(args, v)
		}
		return map[string]any{"call": expr.Text, "args": args}, nil
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
	switch strings.ToLower(typ.Name) {
	case "bool":
		return value.Bool(), nil
	case "u8", "u16", "u32", "u64", "i64", "coins":
		return value.Convert(reflect.TypeOf(uint64(0))).Uint(), nil
	case "bytes", "hash32":
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
			field.SetBytes([]byte(s))
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
