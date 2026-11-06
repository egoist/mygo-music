package player

import "io"

// mp4Reader reads what a download has in, waiting for it to come, for package
// mp4 to read the file's header from.
type mp4Reader struct{ dl *download }

func (r mp4Reader) ReadAt(b []byte, off int64) (int, error) {
	end := off + int64(len(b))
	if r.dl.waitFor(end) < end {
		return 0, io.ErrUnexpectedEOF
	}
	r.dl.mu.Lock()
	defer r.dl.mu.Unlock()
	return r.dl.readAt(b, off)
}

// readPacket reads the bytes of a packet into buf, waiting for them.
func readPacket(dl *download, off int64, size int, buf []byte) ([]byte, error) {
	if dl.waitFor(off+int64(size)) < off+int64(size) {
		dl.mu.Lock()
		err := dl.err
		dl.mu.Unlock()
		if err == nil {
			err = io.EOF // cut short
		}
		return nil, err
	}
	if cap(buf) < size {
		buf = make([]byte, size)
	}
	buf = buf[:size]
	dl.mu.Lock()
	n, err := dl.readAt(buf, off)
	dl.mu.Unlock()
	if n < size {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf, nil
}
