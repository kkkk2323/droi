package diag

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const DefaultRequestPageSize = 100
const MaxRequestPageSize = 500

var ErrInvalidCursor = errors.New("invalid or expired request history cursor")
var ErrHistoryUnavailable = errors.New("request history is not persisted")
var ErrInvalidPageSize = errors.New("request page size must be between 1 and 500")

type RequestPage struct {
	Requests   []RequestRecord `json:"requests"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

// A file identity survives rotation and daemon restarts. Offsets are anchored
// before the oldest returned line, so new appends never shift older pages.
type requestCursor struct {
	Device uint64 `json:"d"`
	Inode  uint64 `json:"i"`
	Offset int64  `json:"o"`
}

func fileCursor(f *os.File, info os.FileInfo, offset int64) (requestCursor, error) {
	device, inode, err := fileID(f, info)
	return requestCursor{Device: device, Inode: inode, Offset: offset}, err
}

func (r *Recorder) RequestHistory(limit int, cursor string) (RequestPage, error) {
	r.mu.Lock()
	sink := r.requestSink
	r.mu.Unlock()
	if sink == nil {
		return RequestPage{}, ErrHistoryUnavailable
	}
	return sink.requestHistory(limit, cursor)
}

// requestHistory reads only enough trailing blocks to fill one page (and find
// one more record). It does not load or count the complete log history.
func (s *FileSink) requestHistory(limit int, cursor string) (RequestPage, error) {
	page := RequestPage{Requests: make([]RequestRecord, 0)}
	if limit < 1 || limit > MaxRequestPageSize {
		return page, ErrInvalidPageSize
	}
	var before requestCursor
	if cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(data) > 256 {
			return page, ErrInvalidCursor
		}
		if json.Unmarshal(data, &before) != nil || before.Inode == 0 || before.Offset < 0 {
			return page, ErrInvalidCursor
		}
	}

	// Keep rotation and appends from changing files while this page is read.
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Dir(s.path))
	if err != nil {
		return page, err
	}
	var files []string
	base := filepath.Base(s.path)
	for _, entry := range entries {
		if entry.Type().IsRegular() && bytes.HasPrefix([]byte(entry.Name()), []byte(base+".")) {
			files = append(files, filepath.Join(filepath.Dir(s.path), entry.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	files = append([]string{s.path}, files...)
	found := cursor == ""
	var next requestCursor
	for _, path := range files {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return page, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return page, err
		}
		identity, err := fileCursor(f, info, info.Size())
		if err != nil {
			f.Close()
			return page, err
		}
		offset := info.Size()
		if !found {
			if identity.Device != before.Device || identity.Inode != before.Inode {
				f.Close()
				continue
			}
			found = true
			offset = before.Offset
			if offset > info.Size() {
				f.Close()
				return page, ErrInvalidCursor
			}
		}
		more, err := readRequestPage(f, identity, offset, limit, &page, &next)
		f.Close()
		if err != nil {
			return page, err
		}
		if more {
			data, _ := json.Marshal(next)
			page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
			return page, nil
		}
	}
	if !found {
		return page, ErrInvalidCursor
	}
	return page, nil
}

func readRequestPage(f *os.File, identity requestCursor, offset int64, limit int, page *RequestPage, next *requestCursor) (bool, error) {
	reader := reverseLines{file: f, offset: offset}
	// A crash may leave a final, unterminated line. Do not expose it as a
	// completed record, even if the fragment happens to be valid JSON.
	if offset > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], offset-1); err != nil {
			return false, err
		}
		if last[0] != '\n' {
			if offset != identity.Offset {
				return false, ErrInvalidCursor
			}
			if _, _, err := reader.next(); err != nil && err != io.EOF {
				return false, err
			}
		}
	}
	for {
		line, start, err := reader.next()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		var rec RequestRecord
		if json.Unmarshal(line, &rec) != nil || rec.Time.IsZero() {
			continue
		}
		if len(page.Requests) == limit {
			return true, nil
		}
		page.Requests = append(page.Requests, rec)
		*next = identity
		next.Offset = start
	}
}

// reverseLines is a bounded-memory, backwards JSONL reader.
type reverseLines struct {
	file   *os.File
	offset int64
	buf    []byte
}

func (r *reverseLines) next() ([]byte, int64, error) {
	for {
		if i := bytes.LastIndexByte(r.buf, '\n'); i >= 0 {
			line := r.buf[i+1:]
			start := r.offset + int64(i+1)
			r.buf = r.buf[:i]
			return line, start, nil
		}
		if r.offset == 0 {
			if len(r.buf) == 0 {
				return nil, 0, io.EOF
			}
			line := r.buf
			r.buf = nil
			return line, 0, nil
		}
		if len(r.buf) > 1<<20 {
			return nil, 0, errors.New("request history line exceeds 1 MiB")
		}
		n := min(r.offset, 32<<10)
		r.offset -= n
		block := make([]byte, int(n)+len(r.buf))
		if _, err := r.file.ReadAt(block[:n], r.offset); err != nil {
			return nil, 0, err
		}
		copy(block[n:], r.buf)
		r.buf = block
	}
}
