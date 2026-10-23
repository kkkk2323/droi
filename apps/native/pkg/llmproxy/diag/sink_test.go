package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFileSink_RetentionAcrossReopens(t *testing.T) {
	for _, tc := range []struct {
		name string
		keep int
		want int
	}{
		{name: "forever", keep: -1, want: 8},
		{name: "default", keep: 0, want: 4},
		{name: "events", keep: 3, want: 4},
		{name: "custom", keep: 2, want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "requests.jsonl")
			legacy := path + ".20000101-000000"
			if err := os.WriteFile(legacy, []byte("{\"id\":0}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			// Reopen between writes to verify append and retention across restarts.
			// A one-byte cap forces rapid rotations without sleeps.
			for i := 1; i <= 7; i++ {
				sink, err := OpenFileSink(path, 1, tc.keep)
				if err != nil {
					t.Fatal(err)
				}
				err = sink.Write([]byte(fmt.Sprintf("{\"id\":%d}", i)))
				closeErr := sink.Close()
				if err != nil {
					t.Fatal(err)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			archives, err := filepath.Glob(path + ".*")
			if err != nil {
				t.Fatal(err)
			}
			if len(archives) != tc.want-1 {
				t.Fatalf("expected %d archives, got %d", tc.want-1, len(archives))
			}
			slices.Sort(archives)
			var got []string
			for _, file := range append(archives, path) {
				got = append(got, readLines(t, file)...)
			}
			var want []string
			for i := 8 - tc.want; i <= 7; i++ {
				want = append(want, fmt.Sprintf("{\"id\":%d}", i))
			}
			if !slices.Equal(got, want) {
				t.Fatalf("persisted records = %v, want %v", got, want)
			}
		})
	}
}
