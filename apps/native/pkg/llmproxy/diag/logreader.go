package diag

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// LogReader reads a request log written by a FileSink, its archives first,
// oldest first, and on later reads only the lines appended since. A file is
// known by its identity, so the current file read part way is read on from
// there once it has been rotated into an archive. Only whole lines are read.
type LogReader struct {
	path string
	// read is how far each file was read, by its identity; done holds the
	// archives read to their end.
	read map[fileKey]int64
	done map[fileKey]bool
}

type fileKey struct{ device, inode uint64 }

func NewLogReader(path string) *LogReader {
	return &LogReader{path: path, read: map[fileKey]int64{}, done: map[fileKey]bool{}}
}

// Read calls fn with each record not read before. A missing log reads as
// empty.
func (r *LogReader) Read(fn func(RequestRecord)) error {
	archives, _ := filepath.Glob(r.path + ".*")
	sort.Strings(archives)
	seen := map[fileKey]bool{}
	for _, path := range append(archives, r.path) {
		key, err := r.readFile(path, path != r.path, fn)
		if err != nil {
			return err
		}
		seen[key] = true
	}
	// Forget the files that are gone, so a reused inode is read anew.
	for k := range r.read {
		if !seen[k] {
			delete(r.read, k)
		}
	}
	for k := range r.done {
		if !seen[k] {
			delete(r.done, k)
		}
	}
	return nil
}

func (r *LogReader) readFile(path string, archive bool, fn func(RequestRecord)) (fileKey, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return fileKey{}, nil
	}
	if err != nil {
		return fileKey{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fileKey{}, err
	}
	device, inode, err := fileID(f, info)
	if err != nil {
		return fileKey{}, err
	}
	key := fileKey{device, inode}
	if r.done[key] {
		return key, nil
	}
	offset := r.read[key]
	if offset > info.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return key, err
	}
	br := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// A line longer than the buffer: read the rest of it.
			long := append([]byte(nil), line...)
			for err == bufio.ErrBufferFull {
				line, err = br.ReadSlice('\n')
				long = append(long, line...)
			}
			line = long
		}
		if err != nil {
			// What is left has no newline yet: a line still being written.
			break
		}
		offset += int64(len(line))
		var rec RequestRecord
		if json.Unmarshal(bytes.TrimSpace(line), &rec) == nil && !rec.Time.IsZero() {
			fn(rec)
		}
	}
	r.read[key] = offset
	// An empty archive may be the name a rotation reserved before the log
	// is renamed onto it.
	if archive && info.Size() > 0 && offset == info.Size() {
		r.done[key] = true
	}
	return key, nil
}
