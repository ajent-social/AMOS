package gotypes

import (
	"errors"
	"fmt"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"

	base "github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	yaml "go.yaml.in/yaml/v4"
)

var propertyNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,119}$`)

var supportedSchemaFields = map[string]struct{}{
	"$ref": {}, "type": {}, "format": {}, "title": {}, "description": {},
	"properties": {}, "required": {}, "items": {}, "additionalProperties": {},
}

func parseSchema(proxy *base.SchemaProxy, location string, depth int) (*schemaIR, error) {
	if proxy == nil {
		return nil, fmt.Errorf("%s: schema is required", location)
	}
	if depth > MaxSchemaDepth {
		return nil, fmt.Errorf("%s: schema nesting exceeds %d levels", location, MaxSchemaDepth)
	}
	if proxy.IsReference() {
		ref := proxy.GetReference()
		component, ok := componentSchemaReference(ref)
		if !ok {
			return nil, fmt.Errorf("%s: schema reference must target a local components.schemas entry", location)
		}
		if node := proxy.GetReferenceNode(); node != nil && node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				if node.Content[i].Value != "$ref" {
					return nil, fmt.Errorf("%s: reference siblings are unsupported", location)
				}
			}
		}
		return &schemaIR{ref: component}, nil
	}
	schema := proxy.Schema()
	if schema == nil {
		return nil, fmt.Errorf("%s: schema could not be resolved", location)
	}
	if node := proxy.GetValueNode(); node != nil {
		if err := checkSupportedSchemaFields(node, location); err != nil {
			return nil, err
		}
	}
	if schema.Nullable != nil && *schema.Nullable {
		return nil, fmt.Errorf("%s: OpenAPI 3.0 nullable is unsupported; use a 3.1 type union with null", location)
	}
	if len(schema.Type) == 0 {
		return nil, fmt.Errorf("%s: schema must declare a supported type", location)
	}
	var kind string
	nullable := false
	for _, candidate := range schema.Type {
		if candidate == "null" {
			if nullable {
				return nil, fmt.Errorf("%s: duplicate null type is unsupported", location)
			}
			nullable = true
			continue
		}
		if kind != "" {
			return nil, fmt.Errorf("%s: multiple non-null type alternatives are unsupported", location)
		}
		kind = candidate
	}
	if kind == "" {
		return nil, fmt.Errorf("%s: null-only schemas are unsupported", location)
	}
	result := &schemaIR{kind: kind, format: schema.Format, nullable: nullable, required: make(map[string]bool)}
	switch kind {
	case "string":
		if schema.Properties != nil || schema.Items != nil || schema.Required != nil || schema.AdditionalProperties != nil {
			return nil, fmt.Errorf("%s: string schema contains object or array fields", location)
		}
		if !oneOf(schema.Format, "", "date", "date-time", "uuid", "email", "uri") {
			return nil, fmt.Errorf("%s: string format is unsupported", location)
		}
	case "integer":
		if !oneOf(schema.Format, "", "int32", "int64") {
			return nil, fmt.Errorf("%s: integer format is unsupported", location)
		}
		if schema.Properties != nil || schema.Items != nil || schema.Required != nil || schema.AdditionalProperties != nil {
			return nil, fmt.Errorf("%s: integer schema contains object or array fields", location)
		}
	case "number":
		if !oneOf(schema.Format, "", "float", "double") {
			return nil, fmt.Errorf("%s: number format is unsupported", location)
		}
		if schema.Properties != nil || schema.Items != nil || schema.Required != nil || schema.AdditionalProperties != nil {
			return nil, fmt.Errorf("%s: number schema contains object or array fields", location)
		}
	case "boolean":
		if schema.Format != "" || schema.Properties != nil || schema.Items != nil || schema.Required != nil || schema.AdditionalProperties != nil {
			return nil, fmt.Errorf("%s: boolean schema contains unsupported fields", location)
		}
	case "object":
		if schema.Properties == nil {
			return nil, fmt.Errorf("%s: object schemas require an explicit properties object", location)
		}
		if schema.AdditionalProperties == nil || !schema.AdditionalProperties.IsB() || schema.AdditionalProperties.B {
			return nil, fmt.Errorf("%s: only explicitly closed objects (additionalProperties: false) are supported", location)
		}
		for _, required := range schema.Required {
			result.required[required] = true
		}
		goNames := make(map[string]string)
		jsonNames := make(map[string]struct{}, schema.Properties.Len())
		for property, child := range schema.Properties.FromOldest() {
			if !propertyNamePattern.MatchString(property) {
				return nil, fmt.Errorf("%s.properties: property name cannot be represented as a Go field", location)
			}
			goName, err := goIdentifier("", property)
			if err != nil {
				return nil, fmt.Errorf("%s.properties: property name cannot be represented as a Go field", location)
			}
			if prior, exists := goNames[goName]; exists {
				return nil, fmt.Errorf("%s.properties: names collide after Go identifier normalization (%s and %s)", location, prior, property)
			}
			goNames[goName] = property
			jsonNames[property] = struct{}{}
			childIR, err := parseSchema(child, location+".properties["+property+"]", depth+1)
			if err != nil {
				return nil, err
			}
			result.fields = append(result.fields, schemaField{jsonName: property, goName: goName, schema: childIR})
		}
		for required := range result.required {
			if _, exists := jsonNames[required]; !exists {
				return nil, fmt.Errorf("%s.required: entry does not name a declared property", location)
			}
		}
		if schema.Format != "" {
			return nil, fmt.Errorf("%s: object format is unsupported", location)
		}
		sort.Slice(result.fields, func(i, j int) bool { return result.fields[i].jsonName < result.fields[j].jsonName })
	case "array":
		if schema.Format != "" {
			return nil, fmt.Errorf("%s: array format is unsupported", location)
		}
		if schema.Items == nil || !schema.Items.IsA() || schema.Items.A == nil {
			return nil, fmt.Errorf("%s: arrays require a homogeneous schema-valued items field", location)
		}
		if schema.Properties != nil || schema.Required != nil || schema.AdditionalProperties != nil {
			return nil, fmt.Errorf("%s: array schema contains object fields", location)
		}
		item, err := parseSchema(schema.Items.A, location+".items", depth+1)
		if err != nil {
			return nil, err
		}
		result.item = item
	default:
		return nil, fmt.Errorf("%s: schema type is unsupported", location)
	}
	return result, nil
}

func checkSupportedSchemaFields(node *yaml.Node, location string) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: schema must be an object", location)
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		if _, ok := supportedSchemaFields[name]; !ok {
			return fmt.Errorf("%s: schema keyword is unsupported", location)
		}
	}
	return nil
}

func componentSchemaReference(ref string) (string, bool) {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(ref, prefix)
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	name = strings.ReplaceAll(name, "~1", "/")
	name = strings.ReplaceAll(name, "~0", "~")
	if !componentNamePattern.MatchString(name) {
		return "", false
	}
	return name, true
}

func (g *generator) resolveComponent(name string) (typeInfo, error) {
	if info, ok := g.componentInfo[name]; ok {
		return info, nil
	}
	if g.emitting[name] {
		return typeInfo{}, fmt.Errorf("components.schemas[%q]: cyclic component reference is unsupported", name)
	}
	ir, ok := g.componentIR[name]
	if !ok {
		return typeInfo{}, fmt.Errorf("component schema reference has no target")
	}
	g.emitting[name] = true
	defer delete(g.emitting, name)
	goName := g.componentNames[name]
	var info typeInfo
	if ir.ref != "" {
		target, err := g.resolveComponent(ir.ref)
		if err != nil {
			return typeInfo{}, err
		}
		info = typeInfo{typeName: goName, baseName: target.baseName, nullable: target.nullable}
	} else if ir.nullable {
		info = typeInfo{typeName: goName, baseName: goName + "Value", nullable: true}
	} else {
		info = typeInfo{typeName: goName, baseName: goName}
	}
	g.componentInfo[name] = info
	return info, nil
}

func (g *generator) emitComponent(name string) error {
	ir := g.componentIR[name]
	goName := g.componentNames[name]
	info := g.componentInfo[name]
	if ir.ref != "" {
		target := g.componentNames[ir.ref]
		return g.define(goName, "component alias "+name, "type "+goName+" = "+target+"\n")
	}
	if info.nullable {
		baseIR := *ir
		baseIR.nullable = false
		if _, err := g.ensureType(&baseIR, info.baseName, 0); err != nil {
			return err
		}
		return g.define(goName, "component alias "+name, "type "+goName+" = *"+info.baseName+"\n")
	}
	_, err := g.ensureType(ir, goName, 0)
	return err
}

func (g *generator) ensureType(ir *schemaIR, name string, depth int) (typeInfo, error) {
	if ir == nil {
		return typeInfo{}, errors.New("schema model is missing")
	}
	if ir.ref != "" {
		info, ok := g.componentInfo[ir.ref]
		if !ok {
			return typeInfo{}, fmt.Errorf("referenced component schema is unknown")
		}
		return info, nil
	}
	if depth > MaxSchemaDepth {
		return typeInfo{}, fmt.Errorf("schema nesting exceeds %d levels", MaxSchemaDepth)
	}
	if ir.nullable {
		baseIR := *ir
		baseIR.nullable = false
		baseName := name + "Value"
		if _, err := g.ensureType(&baseIR, baseName, depth); err != nil {
			return typeInfo{}, err
		}
		if err := g.define(name, "nullable schema", "type "+name+" = *"+baseName+"\n"); err != nil {
			return typeInfo{}, err
		}
		return typeInfo{typeName: name, baseName: baseName, nullable: true}, nil
	}
	if info, ok := g.componentInfo[name]; ok {
		return info, nil
	}
	if _, exists := g.types[name]; exists {
		return typeInfo{}, fmt.Errorf("generated Go type name collision")
	}
	var declaration string
	switch ir.kind {
	case "string":
		declaration = "type " + name + " string\n"
	case "integer":
		underlying := "int64"
		if ir.format == "int32" {
			underlying = "int32"
		}
		declaration = "type " + name + " " + underlying + "\n"
	case "number":
		underlying := "float64"
		if ir.format == "float" {
			underlying = "float32"
		}
		declaration = "type " + name + " " + underlying + "\n"
	case "boolean":
		declaration = "type " + name + " bool\n"
	case "array":
		itemName, err := goIdentifier(name, "item")
		if err != nil {
			return typeInfo{}, err
		}
		item, err := g.ensureType(ir.item, itemName, depth+1)
		if err != nil {
			return typeInfo{}, err
		}
		itemType := item.typeName
		if item.nullable {
			itemType = "*" + item.baseName
		}
		declaration = "type " + name + " []" + itemType + "\n"
	case "object":
		body, err := g.objectDefinition(name, ir.fields, ir.required)
		if err != nil {
			return typeInfo{}, err
		}
		declaration = body
	default:
		return typeInfo{}, errors.New("unsupported schema type")
	}
	if err := g.define(name, "schema type", declaration); err != nil {
		return typeInfo{}, err
	}
	return typeInfo{typeName: name, baseName: name}, nil
}

func (g *generator) objectDefinition(name string, fields []schemaField, required map[string]bool) (string, error) {
	var source strings.Builder
	source.WriteString("type " + name + " struct {\n")
	fieldNames := make(map[string]struct{}, len(fields))
	infos := make([]typeInfo, len(fields))
	for index, field := range fields {
		if _, exists := fieldNames[field.goName]; exists {
			return "", errors.New("schema properties collide as Go fields")
		}
		fieldNames[field.goName] = struct{}{}
		fieldTypeName, err := goIdentifier(name, field.goName)
		if err != nil {
			return "", err
		}
		info, err := g.ensureType(field.schema, fieldTypeName, 1)
		if err != nil {
			return "", err
		}
		infos[index] = info
		fieldType := info.typeName
		isRequired := required[field.jsonName]
		if info.nullable {
			if isRequired {
				fieldType = "*" + info.baseName
			} else {
				fieldType = "OptionalNullable[" + info.baseName + "]"
			}
		} else if !isRequired {
			fieldType = "*" + info.typeName
		}
		tag := strconv.Quote(field.jsonName)
		if !isRequired && !info.nullable {
			tag += ",omitempty"
		}
		source.WriteString(field.goName + " " + fieldType + " `json:" + tag + "`\n")
	}
	source.WriteString("}\n")

	source.WriteString("func (value " + name + ") MarshalJSON() ([]byte, error) {\n")
	source.WriteString("members := make([]jsonMember, 0, " + strconv.Itoa(len(fields)) + ")\n")
	for index, field := range fields {
		info := infos[index]
		isRequired := required[field.jsonName]
		quoted := strconv.Quote(field.jsonName)
		encodedName := "encoded" + strconv.Itoa(index)
		switch {
		case info.nullable && !isRequired:
			source.WriteString("if value." + field.goName + ".Present { if value." + field.goName + ".Null { members = append(members, jsonMember{" + quoted + ", []byte(\"null\")}) } else { " + encodedName + ", err := json.Marshal(value." + field.goName + ".Value); if err != nil { return nil, err }; members = append(members, jsonMember{" + quoted + ", " + encodedName + "}) } }\n")
		case !isRequired && !info.nullable:
			source.WriteString("if value." + field.goName + " != nil { " + encodedName + ", err := json.Marshal(value." + field.goName + "); if err != nil { return nil, err }; members = append(members, jsonMember{" + quoted + ", " + encodedName + "}) }\n")
		default:
			source.WriteString(encodedName + ", err := json.Marshal(value." + field.goName + "); if err != nil { return nil, err }; members = append(members, jsonMember{" + quoted + ", " + encodedName + "})\n")
		}
	}
	source.WriteString("return marshalObject(members)\n}\n")

	source.WriteString("func (value *" + name + ") UnmarshalJSON(data []byte) error {\n")
	source.WriteString("if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' { return errors.New(\"expected JSON object\") }\n")
	source.WriteString("type plain " + name + "\nvar decoded plain\ndecoder := json.NewDecoder(bytes.NewReader(data))\ndecoder.DisallowUnknownFields()\nif err := decoder.Decode(&decoded); err != nil { return err }\nvar trailing struct{}\nif err := decoder.Decode(&trailing); err != io.EOF { return errors.New(\"trailing JSON data\") }\nvar members map[string]json.RawMessage\nif err := json.Unmarshal(data, &members); err != nil { return err }\n")
	for index, field := range fields {
		info := infos[index]
		quoted := strconv.Quote(field.jsonName)
		if required[field.jsonName] {
			source.WriteString("if _, ok := members[" + quoted + "]; !ok { return errors.New(\"required JSON field is missing\") }\n")
		}
		if !info.nullable {
			source.WriteString("if raw, ok := members[" + quoted + "]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte(\"null\")) { return errors.New(\"non-nullable JSON field is null\") }\n")
		}
	}
	source.WriteString("*value = " + name + "(decoded)\nreturn nil\n}\n")
	return source.String(), nil
}

func (g *generator) define(name, source, declaration string) error {
	if !token.IsIdentifier(name) || token.Lookup(name).IsKeyword() {
		return errors.New("generated name is not a valid Go identifier")
	}
	if prior, exists := g.decls[name]; exists {
		if (prior != source && prior != "component schema") || g.types[name] != "" {
			return fmt.Errorf("generated Go name collision between %s and %s", prior, source)
		}
	} else {
		g.decls[name] = source
	}
	g.types[name] = declaration
	return nil
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func successResponse(operation *v3.Operation, operationID string) (*v3.Response, string, error) {
	if operation.Responses == nil || operation.Responses.Codes == nil {
		return nil, "", fmt.Errorf("operation %s: at least one typed 2xx response is required", operationID)
	}
	type responseEntry struct {
		status   string
		code     int
		response *v3.Response
	}
	var success []responseEntry
	for status, response := range operation.Responses.Codes.FromOldest() {
		code, err := strconv.Atoi(status)
		if err == nil && code >= 200 && code < 300 {
			success = append(success, responseEntry{status: status, code: code, response: response})
		}
	}
	if len(success) != 1 {
		return nil, "", fmt.Errorf("operation %s: exactly one explicit 2xx response is supported", operationID)
	}
	if success[0].response == nil || success[0].response.IsReference() {
		return nil, "", fmt.Errorf("operation %s: successful response reference could not be resolved", operationID)
	}
	return success[0].response, success[0].status, nil
}

func onlyJSONMedia(content *orderedmap.Map[string, *v3.MediaType], kind, operationID string) (*v3.MediaType, string, error) {
	if content == nil || content.Len() != 1 {
		return nil, "", fmt.Errorf("operation %s: %s must contain exactly application/json", operationID, kind)
	}
	var mediaType string
	var media *v3.MediaType
	for key, value := range content.FromOldest() {
		mediaType, media = key, value
	}
	if mediaType != "application/json" || media == nil || media.Schema == nil {
		return nil, "", fmt.Errorf("operation %s: %s must contain an application/json schema", operationID, kind)
	}
	return media, mediaType, nil
}
