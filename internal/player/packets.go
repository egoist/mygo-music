package player

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sort"
	"sync"

	aacpcm "github.com/tphakala/go-aac/pcm"

	"mygo-music/internal/mp4"
)

// A packetDecoder decodes a codec's packets, each on its own but for what a
// packet's sound overlaps the one before's.
type packetDecoder interface {
	// decode appends a packet's sound to dst, as stereo.
	decode(dst []float32, packet []byte) ([]float32, error)
	// reset forgets the packets before, as after a seek.
	reset()
	// preroll is how many packets before the one sought to are decoded,
	// for their sound to overlap its.
	preroll() int
}

// packetStream decodes a stream of packets whose places are known, an MP4's
// from its sample table and ADTS's as its headers are read, for package
// audio. A seek goes to the packet holding the frame asked for.
type packetStream struct {
	dl    *download
	dec   packetDecoder
	rate  int
	offs  []int64
	sizes []int
	ends  []int64 // where each packet ends, in frames
	// more finds the next packet, and says if there was one: for ADTS,
	// whose packets aren't all known up front. Nil once they are.
	more   func() bool
	prime  int64 // the encoder's priming, in frames, to drop
	frames int64 // the length, or -1 if unknown
	exact  bool  // whether it is: then what's past it is padding, dropped
	next   int   // the packet decoded next
	pos    int64 // the frame Read returns next
	skip   int64 // frames to drop before it
	packet []byte
	out    []float32
	pend   []float32
	spec   *spectrum
	drop   func() // lets the download go, as the stream closes
	once   sync.Once
}

func (s *packetStream) SampleRate() int { return s.rate }
func (s *packetStream) Channels() int   { return 2 }
func (s *packetStream) Len() int64      { return s.frames }

// packetStart is the frame packet i starts at.
func (s *packetStream) packetStart(i int) int64 {
	if i == 0 {
		return 0
	}
	return s.ends[i-1]
}

// have says whether packet i is known, finding more of them if need be.
func (s *packetStream) have(i int) bool {
	for i >= len(s.offs) {
		if !s.findMore() {
			return false
		}
	}
	return true
}

// findMore finds the next packet, if there's one left to find. Once all are
// found, the length is known.
func (s *packetStream) findMore() bool {
	if s.more == nil {
		return false
	}
	if s.more() {
		return true
	}
	s.more = nil
	if len(s.ends) > 0 {
		s.frames, s.exact = s.ends[len(s.ends)-1]-s.prime, true
	}
	return false
}

func (s *packetStream) Read(dst []float32) (int, error) {
	for len(s.pend) == 0 {
		if !s.have(s.next) {
			return 0, io.EOF
		}
		var err error
		s.packet, err = readPacket(s.dl, s.offs[s.next], s.sizes[s.next], s.packet)
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				err = io.EOF
			}
			return 0, err
		}
		s.next++
		out, err := s.dec.decode(s.out[:0], s.packet)
		s.out = out
		if err != nil {
			continue // a packet gone bad: on to the next
		}
		drop := min(s.skip, int64(len(out)/2))
		s.skip -= drop
		s.pend = out[drop*2:]
	}
	n := copy(dst[:len(dst)/2*2], s.pend)
	if s.exact {
		// What an encoder padded the last packet with.
		n = int(min(int64(n), (s.frames-s.pos)*2))
		if n <= 0 {
			s.pend = nil
			return 0, io.EOF
		}
	}
	s.pend = s.pend[n:]
	s.spec.feed(s.pos, dst[:n])
	s.pos += int64(n / 2)
	return n, nil
}

// SetPosition goes to the packet holding frame, from those before it its
// sound overlaps, and drops what comes before frame.
func (s *packetStream) SetPosition(frame int64) error {
	frame = max(frame, 0)
	if s.frames >= 0 {
		frame = min(frame, s.frames)
	}
	at := frame + s.prime
	for (len(s.ends) == 0 || s.ends[len(s.ends)-1] <= at) && s.findMore() {
	}
	i := sort.Search(len(s.ends), func(i int) bool { return s.ends[i] > at })
	start := max(0, i-s.dec.preroll())
	s.dec.reset()
	s.next, s.pos = start, frame
	s.skip = at - s.packetStart(start)
	s.pend = nil
	return nil
}

func (s *packetStream) Close() error {
	s.once.Do(s.drop)
	return nil
}

func (s *packetStream) source() (*download, *spectrum) { return s.dl, s.spec }

// openMP4Stream opens an .m4a, of ALAC or AAC.
func openMP4Stream(dl *download) (*packetStream, error) {
	size := dl.size
	if size <= 0 {
		size = math.MaxInt64
	}
	a, err := mp4.Read(mp4Reader{dl}, size, true)
	if err != nil {
		return nil, err
	}
	s := &packetStream{dl: dl, drop: dl.Close}
	switch a.Codec {
	case "alac":
		d, err := newALACDecoder(a.Config)
		if err != nil {
			return nil, err
		}
		s.dec, s.rate = d, int(d.rate)
	case "mp4a":
		d, err := newAACDecoder(a.Config)
		if err != nil {
			return nil, err
		}
		s.dec, s.rate = d, d.fd.SampleRate()
	}
	if s.rate <= 0 {
		return nil, errors.New("mp4: no sample rate")
	}
	// In frames, from the track's timescale: most often the same.
	frames := func(t int64) int64 { return t * int64(s.rate) / a.Timescale }
	s.offs = a.Offsets
	s.sizes = make([]int, len(a.Sizes))
	s.ends = make([]int64, len(a.Ends))
	for i := range a.Sizes {
		s.sizes[i] = int(a.Sizes[i])
		s.ends[i] = frames(a.Ends[i])
	}
	s.prime = frames(a.Prime)
	s.skip = s.prime
	s.frames, s.exact = s.ends[len(s.ends)-1]-s.prime, true
	s.spec = newSpectrum(s.rate, s.frames)
	return s, nil
}

// aacDecoder decodes AAC-LC, with go-aac: HE-AAC it refuses.
type aacDecoder struct {
	fd  *aacpcm.FrameDecoder
	pcm []byte
}

func newAACDecoder(asc []byte) (*aacDecoder, error) {
	fd, err := aacpcm.NewRawDecoder(asc)
	if err != nil {
		return nil, err
	}
	return &aacDecoder{fd: fd}, nil
}

func (d *aacDecoder) decode(dst []float32, packet []byte) ([]float32, error) {
	pcm, n, err := d.fd.DecodeFrame(d.pcm[:0], packet)
	if err != nil {
		return dst, err
	}
	d.pcm = pcm
	ch := d.fd.Channels()
	right := 0
	if ch > 1 {
		right = 2
	}
	for i := range n {
		f := pcm[i*ch*2:]
		dst = append(dst,
			float32(int16(binary.LittleEndian.Uint16(f)))/(1<<15),
			float32(int16(binary.LittleEndian.Uint16(f[right:])))/(1<<15))
	}
	return dst, nil
}

func (d *aacDecoder) reset()       { d.fd.Reset() }
func (d *aacDecoder) preroll() int { return 1 }

func (d *alacDecoder) reset()       {}
func (d *alacDecoder) preroll() int { return 0 }

// aacFrame is an AAC frame's samples a channel.
const aacFrame = 1024

var adtsRates = [...]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}

// adtsHeader reads an ADTS frame's header: its length in bytes, and its
// sample rate.
func adtsHeader(h []byte) (length, rate int, ok bool) {
	if len(h) < 7 || h[0] != 0xff || h[1]&0xf6 != 0xf0 {
		return 0, 0, false
	}
	i := int(h[2] >> 2 & 15)
	length = int(h[3]&3)<<11 | int(h[4])<<3 | int(h[5]>>5)
	if i >= len(adtsRates) || length < 7 {
		return 0, 0, false
	}
	return length, adtsRates[i], true
}

// openADTSStream opens an .aac: AAC in ADTS frames, each with a header saying
// how long it is. A whole file's frames are all found up front, for its
// length; one coming in plays as they come, with the length its source says.
func openADTSStream(dl *download, start, durationMs int64) (*packetStream, error) {
	dl.waitFor(start + 7)
	_, rate, ok := adtsHeader(dl.peek(start, 7))
	if !ok {
		return nil, errors.New("aac: not an ADTS stream")
	}
	d := &aacDecoder{fd: aacpcm.NewADTSDecoder()}
	s := &packetStream{dl: dl, dec: d, rate: rate, frames: -1, drop: dl.Close}
	w := window{dl: dl}
	off := start
	s.more = func() bool {
		if dl.waitFor(off+7) < off+7 {
			return false
		}
		length, _, ok := adtsHeader(w.at(off, 7))
		if !ok {
			return false
		}
		s.offs = append(s.offs, off)
		s.sizes = append(s.sizes, length)
		s.ends = append(s.ends, int64(len(s.ends)+1)*aacFrame)
		off += int64(length)
		return true
	}
	dl.mu.Lock()
	whole := dl.done || dl.onDisk
	dl.mu.Unlock()
	if whole {
		for s.findMore() {
		}
	} else if durationMs > 0 {
		s.frames = durationMs * int64(rate) / 1000
	}
	s.spec = newSpectrum(rate, max(s.frames, 0))
	return s, nil
}
