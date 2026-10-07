package memory

import (
	"path/filepath"
	"sync"
	"testing"
)

// The Daemon starts a Session's Memory Server and its hook together; on a
// new database the one that loses the race to set it up must not fail.
func TestManyOpenANewDatabaseAtOnce(t *testing.T) {
	for round := 0; round < 5; round++ {
		dir := filepath.Join(t.TempDir(), "memory")
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s, err := OpenStore(dir)
				if err != nil {
					errs <- err
					return
				}
				s.Close()
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("round %d: %v", round, err)
		}
	}
}
