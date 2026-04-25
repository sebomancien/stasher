package label

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func Unmarshal(labels map[string]string, v any, root ...string) error {
	n := ParseTree(labels)

	n, err := n.At(root...)
	if err != nil {
		return fmt.Errorf("invalid root path %s", strings.Join(root, "."))
	}

	return n.unmarshal(reflect.ValueOf(v))
}

type Unmarshaler interface {
	UnmarshalLabel(s string) error
}

func (n *Node) unmarshal(v reflect.Value) error {
	u, ok := v.Interface().(Unmarshaler)
	if ok {
		return u.UnmarshalLabel(n.Value)
	}

	switch v.Kind() {
	// Pointer
	case reflect.Pointer:
		if v.IsNil() {
			return fmt.Errorf("nil pointer")
		}
		return n.unmarshal(v.Elem())
	// Scalar
	case reflect.Bool:
		b, err := strconv.ParseBool(n.Value)
		if err != nil {
			return err
		}
		v.SetBool(b)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(n.Value, 0, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetUint(u)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v.Type() {
		case reflect.TypeFor[time.Duration]():
			d, err := time.ParseDuration(n.Value)
			if err != nil {
				return err
			}
			v.SetInt(int64(d))
		default:
			i, err := strconv.ParseInt(n.Value, 0, v.Type().Bits())
			if err != nil {
				return err
			}
			v.SetInt(i)
		}
		return nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(n.Value, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetFloat(f)
		return nil
	case reflect.String:
		v.SetString(n.Value)
		return nil
	// Non scalar
	case reflect.Struct:
		return n.unmarshalStruct(v)
	case reflect.Map:
		return n.unmarshalMap(v)
	case reflect.Array:
		return fmt.Errorf("array are not supported yet")
	case reflect.Interface:
		return fmt.Errorf("interface are not supported yet")
	default:
		return fmt.Errorf("unsupported type %v", reflect.ValueOf(v).Kind())
	}
}

func (n *Node) unmarshalStruct(v reflect.Value) error {
	for field := range v.Fields() {
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("label")
		if tag == "" || tag == "-" {
			continue
		}

		defaultVal := field.Tag.Get("default")
		required := field.Tag.Get("required") == "true"

		path := strings.Split(tag, ".")
		child, err := n.At(path...)
		if err != nil {
			if required {
				return fmt.Errorf("%s: required label not set", path)
			} else if defaultVal != "" {
				child = n.insert(defaultVal, path...)
			} else {
				continue
			}
		}

		err = child.unmarshal(v.FieldByIndex(field.Index))
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) unmarshalMap(v reflect.Value) error {
	if v.IsNil() {
		v.Set(reflect.MakeMap(v.Type()))
	}

	for label, child := range n.Children {
		// Unmarshal the key
		key := reflect.New(v.Type().Key())
		node := &Node{Value: label}
		err := node.unmarshal(key)
		if err != nil {
			return fmt.Errorf("invalid key: %w", err)
		}

		// Unmarshal the value
		value := reflect.New(v.Type().Elem())
		err = child.unmarshal(value)
		if err != nil {
			return fmt.Errorf("invalid value: %w", err)
		}

		// Insert map[key] = value
		v.SetMapIndex(key.Elem(), value.Elem())
	}

	return nil
}
