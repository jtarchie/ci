package resources

import (
	"fmt"
	"sort"
	"sync"
)

// Factory is a function that creates a new instance of a resource.
type Factory func() Resource

var (
	registry   = make(map[string]Factory)
	registryMu sync.RWMutex
)

// Register adds a new resource type to the registry.
// This is typically called from init() in each resource package.
func Register(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("resource type %q already registered", name))
	}

	registry[name] = factory
}

// Get retrieves a resource by name and creates a new instance.
// Returns an error if the resource type is not registered.
func Get(name string) (Resource, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown resource type: %s", name)
	}

	return factory(), nil
}

// IsNative returns true if a resource type is registered as a native resource.
func IsNative(name string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()

	_, ok := registry[name]

	return ok
}

// List returns a sorted list of all registered resource type names.
func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}
