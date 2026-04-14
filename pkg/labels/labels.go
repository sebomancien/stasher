package labels

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Node is one node in the label tree.
// It can hold a direct value (leaf), named children (interior), or both.
type Node struct {
	Value    *string
	Children map[string]*Node
}

// ParseTree converts a flat map of dot-separated label keys into a tree.
// For example, the key "a.b.c" with value "v" becomes root["a"]["b"]["c"].Value = &"v".
func ParseTree(labels map[string]string) *Node {
	root := &Node{}
	for key, val := range labels {
		insert(root, strings.Split(key, "."), val)
	}
	return root
}

// insert recursively creates nodes along parts and stores val at the leaf.
func insert(n *Node, parts []string, val string) {
	if len(parts) == 0 {
		n.Value = &val
		return
	}
	if n.Children == nil {
		n.Children = make(map[string]*Node)
	}
	child, ok := n.Children[parts[0]]
	if !ok {
		child = &Node{}
		n.Children[parts[0]] = child
	}
	insert(child, parts[1:], val)
}

// At navigates to the node at the given dot-separated path and returns it.
// Returns nil if any segment along the path is absent.
// An empty path returns the receiver unchanged.
func (n *Node) At(path string) *Node { return navigate(n, path) }

// navigate walks n along the dot-separated path, returning nil if any segment is absent.
// An empty path returns n unchanged.
func navigate(n *Node, path string) *Node {
	if n == nil || path == "" {
		return n
	}
	for _, part := range strings.Split(path, ".") {
		if n.Children == nil {
			return nil
		}
		n = n.Children[part]
		if n == nil {
			return nil
		}
	}
	return n
}

// Unmarshal maps the label tree rooted at n onto dest using "label" struct tags.
// dest must be a non-nil pointer to a struct.
//
// Tag semantics (applied relative to the node n passed to each call):
//   - Primitive fields (string, bool, int*, time.Duration): the tag is a dot-separated
//     path navigated from n to reach the leaf's value.
//   - Nested struct fields: the tag navigates to a sub-node; Unmarshal recurses
//     into that sub-node for the nested struct's own tags.
//   - map[string]T fields where T is a struct: the tag is navigated to a node
//     whose direct children become the map keys, each recursed into as a T.
func Unmarshal(n *Node, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("labels.Unmarshal: dest must be a non-nil pointer to a struct")
	}
	return unmarshalStruct(n, v.Elem())
}

// Parse is a convenience wrapper: ParseTree followed by Unmarshal.
// An optional root path navigates to a subtree before unmarshalling, so struct
// tags can be written relative to that subtree rather than the full tree.
// At most one root may be provided.
func Parse(labels map[string]string, dest any, root ...string) error {
	n := ParseTree(labels)
	switch len(root) {
	case 0:
	case 1:
		n = n.At(root[0])
	default:
		return fmt.Errorf("labels.Parse: at most one root may be specified")
	}
	return Unmarshal(n, dest)
}

var durationType = reflect.TypeFor[time.Duration]()

func unmarshalStruct(n *Node, sv reflect.Value) error {
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

		defaultVal := ft.Tag.Get("default")
		required := ft.Tag.Get("required") == "true"
		fv := sv.Field(i)

		switch fv.Kind() {
		case reflect.Map:
			if err := unmarshalMap(n, fv, ft.Type, tag); err != nil {
				return err
			}

		case reflect.Struct:
			node := navigate(n, tag)
			if fv.Type() == durationType {
				// time.Duration looks like a struct kind but is stored as int64;
				// treat it as a scalar like other numeric types.
				if required && !valueProvided(node, defaultVal) {
					return fmt.Errorf("%s: required label not set", tag)
				}
				if err := setScalar(node, fv, tag, defaultVal); err != nil {
					return err
				}
			} else {
				if err := unmarshalStruct(node, fv); err != nil {
					return err
				}
			}

		default:
			node := navigate(n, tag)
			if required && !valueProvided(node, defaultVal) {
				return fmt.Errorf("%s: required label not set", tag)
			}
			if err := setScalar(node, fv, tag, defaultVal); err != nil {
				return err
			}
		}
	}
	return nil
}

// unmarshalMap handles map[string]T fields.
// The tag is navigated to a parent node; each of that node's children becomes one map entry.
func unmarshalMap(n *Node, mv reflect.Value, mt reflect.Type, tag string) error {
	parent := navigate(n, tag)
	if parent == nil || len(parent.Children) == 0 {
		return nil
	}

	if mv.IsNil() {
		mv.Set(reflect.MakeMap(mt))
	}

	valType := mt.Elem()
	for key, child := range parent.Children {
		val := reflect.New(valType).Elem()
		if err := unmarshalStruct(child, val); err != nil {
			return fmt.Errorf("key %q: %w", key, err)
		}
		mv.SetMapIndex(reflect.ValueOf(key), val)
	}
	return nil
}

// valueProvided reports whether a scalar will receive a value — either from the
// label node or from a declared default. Used to enforce required:"true" fields.
func valueProvided(n *Node, defaultVal string) bool {
	return (n != nil && n.Value != nil) || defaultVal != ""
}

// setScalar writes a value into fv, preferring the label node's value over defaultVal.
// If neither is present, the field is left at its Go zero value.
// tag is included in error messages for context.
func setScalar(n *Node, fv reflect.Value, tag, defaultVal string) error {
	raw := defaultVal
	if n != nil && n.Value != nil {
		raw = *n.Value
	}
	if raw == "" {
		return nil
	}
	if err := setValue(fv, raw); err != nil {
		return fmt.Errorf("%s: %w", tag, err)
	}
	return nil
}

// setValue converts raw into the Go type of fv and stores it.
// Supported kinds: string, bool, int* (including time.Duration).
func setValue(fv reflect.Value, raw string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)

	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("want bool, got %q", raw)
		}
		fv.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == durationType {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("want duration (e.g. 24h), got %q", raw)
			}
			fv.SetInt(int64(d))
		} else {
			i, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return fmt.Errorf("want int, got %q", raw)
			}
			fv.SetInt(i)
		}

	default:
		return fmt.Errorf("unsupported field type %s", fv.Type())
	}
	return nil
}
