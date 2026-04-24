package label

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Unmarshaler is implemented by types that parse their own label string value.
// If a type (or its pointer) implements Unmarshaler, UnmarshalLabel is called
// instead of the default kind-based conversion — both for struct fields and
// map keys.
type Unmarshaler interface {
	UnmarshalLabel(s string) error
}

// Maps the label tree rooted at n onto dest using "label" struct tags.
// dest must be a non-nil pointer to a struct.
//
// Tag semantics (applied relative to the node n passed to each call):
//   - Primitive fields (string, bool, int*, time.Duration): the tag is a dot-separated
//     path navigated from n to reach the leaf's value.
//   - Nested struct fields: the tag navigates to a sub-node; Unmarshal recurses
//     into that sub-node for the nested struct's own tags.
//   - map[string]T fields where T is a struct: the tag is navigated to a node
//     whose direct children become the map keys, each recursed into as a T.
func (n *Node) Unmarshal(dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("label.Unmarshal: dest must be a non-nil pointer to a struct")
	}
	return n.unmarshalStruct(v.Elem())
}

// Unmarshal is a convenience wrapper: ParseTree followed by Unmarshal.
// An optional root path navigates to a subtree before unmarshalling, so struct
// tags can be written relative to that subtree rather than the full tree.
func Unmarshal(labels map[string]string, dest any, root ...string) error {
	n := ParseTree(labels)

	n, err := n.At(root...)
	if err != nil {
		return fmt.Errorf("invalid root path %s", strings.Join(root, "."))
	}

	return n.Unmarshal(dest)
}

var durationType = reflect.TypeFor[time.Duration]()

func (n *Node) unmarshalStruct(sv reflect.Value) error {
	st := sv.Type()
	for i := range st.NumField() {
		ft := st.Field(i)
		if !ft.IsExported() {
			continue
		}
		tag := ft.Tag.Get("label")
		if tag == "" || tag == "-" {
			continue
		}

		path := strings.Split(tag, ".")
		defaultVal := ft.Tag.Get("default")
		required := ft.Tag.Get("required") == "true"
		fv := sv.Field(i)

		node, err := n.At(path...)
		if err != nil {
			if required {
				return fmt.Errorf("%s: required label not set", tag)
			} else if defaultVal != "" {
				node = n.insert(defaultVal, path...)
			} else {
				continue
			}
		}

		// If the field's pointer implements Unmarshaler, delegate to it regardless of Kind.
		if fv.CanAddr() {
			_, ok := fv.Addr().Interface().(Unmarshaler)
			if ok {
				err := node.setScalar(fv)
				if err != nil {
					return err
				}
				continue
			}
		}

		switch fv.Kind() {
		case reflect.Map:
			err := n.unmarshalMap(fv, ft.Type, path...)
			if err != nil {
				return err
			}

		case reflect.Struct:
			if fv.Type() == durationType {
				// time.Duration looks like a struct kind but is stored as int64;
				// treat it as a scalar like other numeric types.
				err := node.setScalar(fv)
				if err != nil {
					return err
				}
			} else {
				err := node.unmarshalStruct(fv)
				if err != nil {
					return err
				}
			}

		default:
			err := node.setScalar(fv)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// unmarshalMap handles map[K]V fields.
//
// The tag navigates to a parent node; each child key becomes a map entry.
// K may be any type whose pointer implements Unmarshaler, or a plain string.
// V may be a struct (recursively unmarshalled) or any scalar type.
func (n *Node) unmarshalMap(mv reflect.Value, mt reflect.Type, path ...string) error {
	parent, err := n.At(path...)
	if err != nil || len(parent.Children) == 0 {
		return nil
	}

	if mv.IsNil() {
		mv.Set(reflect.MakeMap(mt))
	}

	keyType := mt.Key()
	valType := mt.Elem()

	for rawKey, child := range parent.Children {
		// Build the map key. Prefer Unmarshaler; fall back to plain string.
		kv := reflect.New(keyType).Elem()
		u, ok := kv.Addr().Interface().(Unmarshaler)
		if ok {
			err := u.UnmarshalLabel(rawKey)
			if err != nil {
				return fmt.Errorf("key %q: %w", rawKey, err)
			}
		} else if keyType.Kind() == reflect.String {
			kv.SetString(rawKey)
		} else {
			return fmt.Errorf("key %q: unsupported map key type %s", rawKey, keyType)
		}

		// Unmarshal the value: struct fields recurse; scalars read the node value.
		vv := reflect.New(valType).Elem()
		if valType.Kind() == reflect.Struct {
			err := child.unmarshalStruct(vv)
			if err != nil {
				return fmt.Errorf("key %q: %w", rawKey, err)
			}
		} else {
			err := child.setScalar(vv)
			if err != nil {
				return fmt.Errorf("key %q: %w", rawKey, err)
			}
		}

		mv.SetMapIndex(kv, vv)
	}
	return nil
}

// setScalar writes a value into fv, preferring the label node's value over defaultVal.
// If neither is present, the field is left at its Go zero value.
// tag is included in error messages for context.
func (n *Node) setScalar(fv reflect.Value) error {
	if fv.CanAddr() {
		u, ok := fv.Addr().Interface().(Unmarshaler)
		if ok {
			return u.UnmarshalLabel(n.Value)
		}
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(n.Value)

	case reflect.Bool:
		b, err := strconv.ParseBool(n.Value)
		if err != nil {
			return fmt.Errorf("want bool, got %q", n.Value)
		}
		fv.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == durationType {
			d, err := time.ParseDuration(n.Value)
			if err != nil {
				return fmt.Errorf("want duration (e.g. 24h), got %q", n.Value)
			}
			fv.SetInt(int64(d))
		} else {
			i, err := strconv.ParseInt(n.Value, 0, 64)
			if err != nil {
				return fmt.Errorf("want int, got %q", n.Value)
			}
			fv.SetInt(i)
		}

	default:
		return fmt.Errorf("unsupported field type %s", fv.Type())
	}
	return nil
}
