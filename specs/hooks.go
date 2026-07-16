package specs

import (
	"context"
	"fmt"
	"sync"
)

type OperationCaller interface {
	CallRaw(ctx context.Context, operation string, request map[string]any) (map[string]any, error)
}

type BeforeOperation func(ctx context.Context, caller OperationCaller, request map[string]any) (map[string]any, error)
type AfterOperationError func(ctx context.Context, caller OperationCaller, request map[string]any, err error) error
type FieldMapper func(value any) any
type BindingItemNormalizer func(value map[string]any, index int) (map[string]any, error)

type hookKey struct {
	product  string
	resource string
	name     string
}

var (
	mu               sync.RWMutex
	beforeOperations = map[hookKey]BeforeOperation{}
	afterErrors      = map[hookKey]AfterOperationError{}
	fieldMappers     = map[hookKey]FieldMapper{}
	itemNormalizers  = map[hookKey]BindingItemNormalizer{}
)

func init() {
	RegisterFieldMapper("", "", "tags", normalizeTagMap)
}

func RegisterBeforeOperation(product, resource, name string, hook BeforeOperation) {
	mu.Lock()
	defer mu.Unlock()
	beforeOperations[hookKey{product: product, resource: resource, name: name}] = hook
}

func BeforeOperationHook(product, resource, name string) (BeforeOperation, bool) {
	mu.RLock()
	defer mu.RUnlock()
	hook, ok := beforeOperations[hookKey{product: product, resource: resource, name: name}]
	return hook, ok
}

func RegisterAfterOperationError(product, resource, name string, hook AfterOperationError) {
	mu.Lock()
	defer mu.Unlock()
	afterErrors[hookKey{product: product, resource: resource, name: name}] = hook
}

func AfterOperationErrorHook(product, resource, name string) (AfterOperationError, bool) {
	mu.RLock()
	defer mu.RUnlock()
	hook, ok := afterErrors[hookKey{product: product, resource: resource, name: name}]
	return hook, ok
}

func RegisterFieldMapper(product, resource, field string, mapper FieldMapper) {
	mu.Lock()
	defer mu.Unlock()
	fieldMappers[hookKey{product: product, resource: resource, name: field}] = mapper
}

func MapField(product, resource, field string, value any) (any, bool) {
	mu.RLock()
	defer mu.RUnlock()
	mapper, ok := fieldMappers[hookKey{product: product, resource: resource, name: field}]
	if ok {
		return mapper(value), true
	}
	mapper, ok = fieldMappers[hookKey{name: field}]
	if ok {
		return mapper(value), true
	}
	return nil, false
}

func RegisterBindingItemNormalizer(product, resource, name string, normalizer BindingItemNormalizer) {
	mu.Lock()
	defer mu.Unlock()
	itemNormalizers[hookKey{product: product, resource: resource, name: name}] = normalizer
}

func NormalizeBindingItem(product, resource, name string, value map[string]any, index int) (map[string]any, bool, error) {
	mu.RLock()
	defer mu.RUnlock()
	normalizer, ok := itemNormalizers[hookKey{product: product, resource: resource, name: name}]
	if !ok {
		normalizer, ok = itemNormalizers[hookKey{name: name}]
	}
	if !ok {
		return nil, false, nil
	}
	normalized, err := normalizer(value, index)
	return normalized, true, err
}

func normalizeTagMap(value any) any {
	tags := map[string]string{}
	for _, rawTag := range hookAnySlice(value) {
		source, ok := rawTag.(map[string]any)
		if !ok {
			continue
		}
		key := hookStringValue(source["Key"])
		if key == "" {
			key = hookStringValue(source["TagKey"])
		}
		if key == "" {
			continue
		}
		value := hookStringValue(source["Value"])
		if _, ok := source["Value"]; !ok {
			value = hookStringValue(source["TagValue"])
		}
		tags[key] = value
	}
	return tags
}

func hookAnySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

func hookStringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}
