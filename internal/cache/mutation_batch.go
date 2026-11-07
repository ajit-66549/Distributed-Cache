package cache

import "github.com/ajit-66549/distributed-cache/internal/persistence"

// buildSetMutations assembles eviction DELETEs followed by the replacement SET.
// expiresAtUnixNano is an absolute deadline; nil means no expiration.
func buildSetMutations(key, value string, expiresAtUnixNano *int64, victims []string) []persistence.Mutation {
	mutations := make([]persistence.Mutation, 0, len(victims)+1)
	for _, victim := range victims {
		mutations = append(mutations, persistence.Mutation{
			Op:  persistence.OpDelete,
			Key: victim,
		})
	}

	return append(mutations, persistence.Mutation{
		Op:                persistence.OpSet,
		key:               key,
		Value:             value,
		ExpiresAtUnixNano: expiresAtUnixNano,
	})
}
