package persistence

import (
	"fmt"
	"log"
	"os"
)

// repairAOFTail removes only the incomplete bytes reported by scanAOF. The
// caller must hold the data directory lock and keep appends disabled until this
// returns successfully. A sync failure aborts startup even after truncation.
func repairAOFTail(file *os.File, validBytes, incompleteBytes int64) error {
	if err := file.Truncate(validBytes); err != nil {
		return fmt.Errorf("truncate incomplete AOF %q at offset %d: %w", file.Name(), validBytes, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync repaired AOF %q at offset %d: %w", file.Name(), validBytes, err)
	}
	log.Printf("repaired AOF %q at offset %d: discarded %d bytes from an incomplete final record", file.Name(), validBytes, incompleteBytes)
	return nil
}
