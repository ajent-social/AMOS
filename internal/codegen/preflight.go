package codegen

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v4"
)

type yamlFrame struct {
	node  *yaml.Node
	depth int
	path  []string
}

type referenceEdge struct {
	from string
	to   string
	loc  string
}

var supportedOperationFields = map[string]struct{}{
	"tags": {}, "summary": {}, "description": {}, "externalDocs": {},
	"operationId": {}, "parameters": {}, "requestBody": {}, "responses": {},
	"callbacks": {}, "deprecated": {}, "security": {}, "servers": {},
}

// validateSupportedFields rejects features the initial transport generator
// cannot represent and operation properties that libopenapi would otherwise
// preserve or ignore without AMOS codegen semantics.
func validateSupportedFields(root *yaml.Node) error {
	if root == nil || root.Kind != yaml.MappingNode {
		return errors.New("OpenAPI document root must be an object")
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "webhooks" {
			return errors.New("webhooks are unsupported by this generator version")
		}
	}
	stack := []yamlFrame{{node: root}}
	for len(stack) > 0 {
		last := len(stack) - 1
		frame := stack[last]
		stack = stack[:last]
		if frame.node.Kind != yaml.MappingNode {
			continue
		}
		if len(frame.path) == 3 && frame.path[0] == "paths" && isHTTPMethod(frame.path[2]) {
			for i := 0; i+1 < len(frame.node.Content); i += 2 {
				name := frame.node.Content[i].Value
				if name == "callbacks" {
					return fmt.Errorf("%s: callbacks are unsupported by this generator version", pointerName(appendPath(frame.path, name)))
				}
				if _, ok := supportedOperationFields[name]; !ok && !strings.HasPrefix(name, "x-") {
					return fmt.Errorf("%s: unsupported operation field", pointerName(appendPath(frame.path, name)))
				}
			}
		}
		if len(frame.path) == 2 && frame.path[0] == "paths" {
			for i := 0; i+1 < len(frame.node.Content); i += 2 {
				if frame.node.Content[i].Value == "$ref" {
					return fmt.Errorf("%s: Path Item references are unsupported by this generator version", pointerName(appendPath(frame.path, "$ref")))
				}
			}
		}
		for i := 0; i+1 < len(frame.node.Content); i += 2 {
			key, value := frame.node.Content[i], frame.node.Content[i+1]
			stack = append(stack, yamlFrame{node: value, path: appendPath(frame.path, key.Value)})
		}
	}
	return nil
}

func isHTTPMethod(value string) bool {
	switch value {
	case "get", "put", "post", "delete", "options", "head", "patch", "trace":
		return true
	default:
		return false
	}
}

// validateYAMLTree bounds parser work and rejects ambiguous YAML constructs
// before libopenapi builds or resolves a model.
func validateYAMLTree(root *yaml.Node) error {
	stack := []yamlFrame{{node: root}}
	for len(stack) > 0 {
		last := len(stack) - 1
		frame := stack[last]
		stack = stack[:last]
		if frame.depth > MaxYAMLDepth {
			return fmt.Errorf("%s: YAML nesting exceeds the %d level limit", pointerName(frame.path), MaxYAMLDepth)
		}
		node := frame.node
		if node == nil {
			return fmt.Errorf("%s: invalid YAML node", pointerName(frame.path))
		}
		if node.Kind == yaml.AliasNode {
			return fmt.Errorf("%s: YAML aliases are unsupported", pointerName(frame.path))
		}
		if node.Kind == yaml.MappingNode {
			seen := make(map[string]struct{}, len(node.Content)/2)
			for i := 0; i < len(node.Content); i += 2 {
				key := node.Content[i]
				value := node.Content[i+1]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
					return fmt.Errorf("%s: mapping keys must be strings", pointerName(frame.path))
				}
				name := key.Value
				if _, ok := seen[name]; ok {
					return fmt.Errorf("%s: duplicate YAML key", pointerName(appendPath(frame.path, name)))
				}
				seen[name] = struct{}{}
				if name == "<<" {
					return fmt.Errorf("%s: YAML merge keys are unsupported", pointerName(appendPath(frame.path, name)))
				}
				if name == "$ref" && (value.Kind != yaml.ScalarNode || value.Tag != "!!str") {
					return fmt.Errorf("%s: reference must be a string", pointerName(appendPath(frame.path, name)))
				}
				stack = append(stack, yamlFrame{node: value, depth: frame.depth + 1, path: appendPath(frame.path, name)})
			}
		} else {
			for i := len(node.Content) - 1; i >= 0; i-- {
				stack = append(stack, yamlFrame{node: node.Content[i], depth: frame.depth + 1, path: appendPath(frame.path, strconv.Itoa(i))})
			}
		}
	}
	return nil
}

// rejectReferenceCycles rejects cycles in contract references and rejects
// non-fragment references before the parser has any opportunity to resolve a
// file or network location.
func rejectReferenceCycles(root *yaml.Node) error {
	root = documentRoot(root)
	var refs []referenceEdge
	stack := []yamlFrame{{node: root}}
	for len(stack) > 0 {
		last := len(stack) - 1
		frame := stack[last]
		stack = stack[:last]
		node := frame.node
		if node.Kind == yaml.MappingNode {
			for i := 0; i < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				childPath := appendPath(frame.path, key.Value)
				if key.Value == "$ref" {
					if len(refs) >= MaxReferenceCount {
						return fmt.Errorf("%s: reference count exceeds the %d reference limit", pointerName(childPath), MaxReferenceCount)
					}
					if !strings.HasPrefix(value.Value, "#/") {
						return fmt.Errorf("%s: only internal JSON Pointer references are supported", pointerName(childPath))
					}
					target, err := parsePointer(value.Value)
					if err != nil {
						return fmt.Errorf("%s: reference is not a canonical JSON Pointer", pointerName(childPath))
					}
					if _, ok := resolvePointer(root, target); !ok {
						return fmt.Errorf("%s: reference target does not exist", pointerName(childPath))
					}
					refs = append(refs, referenceEdge{
						from: referenceOwner(frame.path),
						to:   referenceOwner(target),
						loc:  pointerName(childPath),
					})
				}
				stack = append(stack, yamlFrame{node: value, path: childPath})
			}
		} else {
			for i := len(node.Content) - 1; i >= 0; i-- {
				stack = append(stack, yamlFrame{node: node.Content[i], path: appendPath(frame.path, strconv.Itoa(i))})
			}
		}
	}

	graph := make(map[string][]referenceEdge)
	for _, edge := range refs {
		graph[edge.from] = append(graph[edge.from], edge)
	}
	state := make(map[string]uint8)
	var visit func(string) error
	visit = func(vertex string) error {
		if state[vertex] == 1 {
			return fmt.Errorf("%s: cyclic OpenAPI references are unsupported", vertex)
		}
		if state[vertex] == 2 {
			return nil
		}
		state[vertex] = 1
		for _, edge := range graph[vertex] {
			if err := visit(edge.to); err != nil {
				return fmt.Errorf("%s: %w", edge.loc, err)
			}
		}
		state[vertex] = 2
		return nil
	}
	vertices := make([]string, 0, len(graph))
	for vertex := range graph {
		vertices = append(vertices, vertex)
	}
	sort.Strings(vertices)
	for _, vertex := range vertices {
		if err := visit(vertex); err != nil {
			return err
		}
	}
	return nil
}

func documentRoot(root *yaml.Node) *yaml.Node {
	if root != nil && root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		return root.Content[0]
	}
	return root
}

func parsePointer(value string) ([]string, error) {
	if !strings.HasPrefix(value, "#/") {
		return nil, errors.New("not an internal pointer")
	}
	parts := strings.Split(value[2:], "/")
	for i, part := range parts {
		var decoded strings.Builder
		for index := 0; index < len(part); index++ {
			if part[index] != '~' {
				decoded.WriteByte(part[index])
				continue
			}
			if index+1 >= len(part) {
				return nil, errors.New("invalid JSON Pointer escape")
			}
			index++
			switch part[index] {
			case '0':
				decoded.WriteByte('~')
			case '1':
				decoded.WriteByte('/')
			default:
				return nil, errors.New("invalid JSON Pointer escape")
			}
		}
		parts[i] = decoded.String()
	}
	return parts, nil
}

func resolvePointer(root *yaml.Node, parts []string) (*yaml.Node, bool) {
	current := root
	for _, part := range parts {
		switch current.Kind {
		case yaml.MappingNode:
			found := false
			for i := 0; i+1 < len(current.Content); i += 2 {
				if current.Content[i].Value == part {
					current = current.Content[i+1]
					found = true
					break
				}
			}
			if !found {
				return nil, false
			}
		case yaml.SequenceNode:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(current.Content) {
				return nil, false
			}
			current = current.Content[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func referenceOwner(pointer []string) string {
	if len(pointer) >= 3 && pointer[0] == "components" {
		return pointerName(pointer[:3])
	}
	if len(pointer) >= 2 && pointer[0] == "paths" {
		return pointerName(pointer[:2])
	}
	if len(pointer) >= 2 {
		return pointerName(pointer[:2])
	}
	return "#"
}

func appendPath(path []string, token string) []string {
	result := make([]string, len(path)+1)
	copy(result, path)
	result[len(path)] = token
	return result
}

func pointerName(path []string) string {
	if len(path) == 0 {
		return "#"
	}
	var result strings.Builder
	result.WriteByte('#')
	for _, token := range path {
		token = strings.ReplaceAll(token, "~", "~0")
		token = strings.ReplaceAll(token, "/", "~1")
		result.WriteByte('/')
		result.WriteString(token)
	}
	return result.String()
}
