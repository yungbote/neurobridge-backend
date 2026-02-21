package qdrant

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	filterOpAnd = "$and"
	filterOpOr  = "$or"
	filterOpNot = "$not"
	filterOpIn  = "$in"
	filterOpEq  = "$eq"
	filterOpNe  = "$ne"
)

type translatedFilter struct {
	Must    []any
	Should  []any
	MustNot []any
}

func (f translatedFilter) asMap() map[string]any {
	out := map[string]any{}
	if len(f.Must) > 0 {
		out["must"] = f.Must
	}
	if len(f.Should) > 0 {
		out["should"] = f.Should
	}
	if len(f.MustNot) > 0 {
		out["must_not"] = f.MustNot
	}
	return out
}

func mergeTranslatedFilters(dst *translatedFilter, src translatedFilter) {
	if dst == nil {
		return
	}
	dst.Must = append(dst.Must, src.Must...)
	dst.Should = append(dst.Should, src.Should...)
	dst.MustNot = append(dst.MustNot, src.MustNot...)
}

func translateFilterMap(filter map[string]any) (translatedFilter, error) {
	out := translatedFilter{}
	if len(filter) == 0 {
		return out, nil
	}

	keys := make([]string, 0, len(filter))
	for key := range filter {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := filter[key]
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}

		if strings.HasPrefix(k, "$") {
			switch strings.ToLower(k) {
			case filterOpAnd:
				items, err := toObjectSlice(value)
				if err != nil {
					return translatedFilter{}, opErr(
						"filter_translate",
						OperationErrorValidation,
						fmt.Sprintf("operator %s expects array of objects", filterOpAnd),
						err,
					)
				}
				for _, item := range items {
					sub, err := translateFilterMap(item)
					if err != nil {
						return translatedFilter{}, err
					}
					out.Must = append(out.Must, sub.asMap())
				}
			case filterOpOr:
				items, err := toObjectSlice(value)
				if err != nil {
					return translatedFilter{}, opErr(
						"filter_translate",
						OperationErrorValidation,
						fmt.Sprintf("operator %s expects array of objects", filterOpOr),
						err,
					)
				}
				for _, item := range items {
					sub, err := translateFilterMap(item)
					if err != nil {
						return translatedFilter{}, err
					}
					out.Should = append(out.Should, sub.asMap())
				}
			case filterOpNot:
				item, ok := value.(map[string]any)
				if !ok {
					return translatedFilter{}, opErr(
						"filter_translate",
						OperationErrorValidation,
						fmt.Sprintf("operator %s expects an object", filterOpNot),
						nil,
					)
				}
				sub, err := translateFilterMap(item)
				if err != nil {
					return translatedFilter{}, err
				}
				out.MustNot = append(out.MustNot, sub.asMap())
			default:
				return translatedFilter{}, opErr(
					"filter_translate",
					OperationErrorUnsupportedFilter,
					fmt.Sprintf("unsupported top-level filter operator %q", k),
					nil,
				)
			}
			continue
		}

		fieldPart, err := translateFieldFilter(k, value)
		if err != nil {
			return translatedFilter{}, err
		}
		mergeTranslatedFilters(&out, fieldPart)
	}

	return out, nil
}

func translateFieldFilter(field string, value any) (translatedFilter, error) {
	out := translatedFilter{}

	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			return translatedFilter{}, opErr(
				"filter_translate",
				OperationErrorValidation,
				fmt.Sprintf("field %q has empty operator map", field),
				nil,
			)
		}
		ops := make([]string, 0, len(typed))
		for op := range typed {
			ops = append(ops, op)
		}
		sort.Strings(ops)

		for _, op := range ops {
			opVal := typed[op]
			switch strings.ToLower(strings.TrimSpace(op)) {
			case filterOpEq:
				scalar, ok := toScalarValue(opVal)
				if !ok {
					return translatedFilter{}, opErr(
						"filter_translate",
						OperationErrorValidation,
						fmt.Sprintf("operator %s for field %q expects scalar value", filterOpEq, field),
						nil,
					)
				}
				out.Must = append(out.Must, qdrantMatchCondition(field, scalar))
			case filterOpNe:
				scalar, ok := toScalarValue(opVal)
				if !ok {
					return translatedFilter{}, opErr(
						"filter_translate",
						OperationErrorValidation,
						fmt.Sprintf("operator %s for field %q expects scalar value", filterOpNe, field),
						nil,
					)
				}
				out.MustNot = append(out.MustNot, qdrantMatchCondition(field, scalar))
			case filterOpIn:
				values, err := toScalarSlice(opVal)
				if err != nil {
					return translatedFilter{}, opErr(
						"filter_translate",
						OperationErrorValidation,
						fmt.Sprintf("operator %s for field %q expects scalar array", filterOpIn, field),
						err,
					)
				}
				if len(values) == 0 {
					return translatedFilter{}, opErr(
						"filter_translate",
						OperationErrorValidation,
						fmt.Sprintf("operator %s for field %q cannot be empty", filterOpIn, field),
						nil,
					)
				}
				out.Must = append(out.Must, map[string]any{
					"key": field,
					"match": map[string]any{
						"any": values,
					},
				})
			default:
				return translatedFilter{}, opErr(
					"filter_translate",
					OperationErrorUnsupportedFilter,
					fmt.Sprintf("unsupported filter operator %q for field %q", op, field),
					nil,
				)
			}
		}
		return out, nil

	default:
		scalar, ok := toScalarValue(value)
		if !ok {
			return translatedFilter{}, opErr(
				"filter_translate",
				OperationErrorValidation,
				fmt.Sprintf("field %q expects scalar value or operator object", field),
				nil,
			)
		}
		out.Must = append(out.Must, qdrantMatchCondition(field, scalar))
		return out, nil
	}
}

func qdrantMatchCondition(key string, value any) map[string]any {
	return map[string]any{
		"key": key,
		"match": map[string]any{
			"value": value,
		},
	}
}

func toObjectSlice(value any) ([]map[string]any, error) {
	rawSlice, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("expected []any, got %T", value)
	}
	out := make([]map[string]any, 0, len(rawSlice))
	for _, item := range rawSlice {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected map[string]any in array, got %T", item)
		}
		out = append(out, obj)
	}
	return out, nil
}

func toScalarSlice(value any) ([]any, error) {
	switch typed := value.(type) {
	case []any:
		out := make([]any, 0, len(typed))
		for _, v := range typed {
			scalar, ok := toScalarValue(v)
			if !ok {
				return nil, fmt.Errorf("expected scalar, got %T", v)
			}
			out = append(out, scalar)
		}
		return out, nil
	case []string:
		out := make([]any, 0, len(typed))
		for _, v := range typed {
			out = append(out, v)
		}
		return out, nil
	case []int:
		out := make([]any, 0, len(typed))
		for _, v := range typed {
			out = append(out, v)
		}
		return out, nil
	case []int64:
		out := make([]any, 0, len(typed))
		for _, v := range typed {
			out = append(out, v)
		}
		return out, nil
	case []float64:
		out := make([]any, 0, len(typed))
		for _, v := range typed {
			out = append(out, v)
		}
		return out, nil
	case []bool:
		out := make([]any, 0, len(typed))
		for _, v := range typed {
			out = append(out, v)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected scalar array, got %T", value)
	}
}

func toScalarValue(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		return typed, true
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return typed, true
	case uint:
		return typed, true
	case uint8:
		return uint(typed), true
	case uint16:
		return uint(typed), true
	case uint32:
		return uint(typed), true
	case uint64:
		return typed, true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		if i, err := typed.Int64(); err == nil {
			return i, true
		}
		if f, err := typed.Float64(); err == nil {
			return f, true
		}
		return nil, false
	default:
		return nil, false
	}
}
