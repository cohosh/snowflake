package proto

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"sync"
)

// Fixed size window used for sequencing and reliability layer
const windowSize = 1500
const snowflakeHeaderLen = 18
const maxLength = 65535
const sessionIDLength = 8

type snowflakeHeader struct {
	seq       uint32
	ack       uint32
	length    uint16 //length of the accompanying data (excluding header length)
	sessionID []byte
}

func (h *snowflakeHeader) Parse(b []byte) error {
	h.seq = binary.BigEndian.Uint32(b[0:4])
	h.ack = binary.BigEndian.Uint32(b[4:8])
	h.length = binary.BigEndian.Uint16(b[8:10])
	h.sessionID = b[10:18]

	return nil
}

// Converts a header to bytes
func (h *snowflakeHeader) Marshal() ([]byte, error) {
	if h == nil {
		return nil, fmt.Errorf("nil header")
	}
	b := make([]byte, snowflakeHeaderLen, snowflakeHeaderLen)
	binary.BigEndian.PutUint32(b[0:4], h.seq)
	binary.BigEndian.PutUint32(b[4:8], h.ack)
	binary.BigEndian.PutUint16(b[8:10], h.length)
	copy(b[10:18], h.sessionID)

	return b, nil

}

// Parses a webRTC header from bytes received on the
// webRTC connection
func readHeader(r io.Reader, h *snowflakeHeader) error {

	b := make([]byte, snowflakeHeaderLen, snowflakeHeaderLen)

	_, err := io.ReadAtLeast(r, b, snowflakeHeaderLen)
	if err != nil {
		return err
	}

	if err := h.Parse(b); err != nil {
		return err
	}
	return nil
}

type SnowflakeReadWriter struct {
	seq       uint32
	ack       uint32
	sessionID []byte

	Conn io.ReadWriteCloser
	pr   *io.PipeReader
	lock sync.Mutex //need a lock on the acknowledgement since multiple goroutines access it
}

func NewSnowflakeReadWriter(sConn io.ReadWriteCloser) *SnowflakeReadWriter {
	pr, pw := io.Pipe()
	s := &SnowflakeReadWriter{
		Conn: sConn,
		pr:   pr,
	}
	go s.readLoop(pw)
	return s
}

func (s *SnowflakeReadWriter) genSessionID() error {
	buf := make([]byte, sessionIDLength)
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	s.sessionID = buf
	return nil
}

func (s *SnowflakeReadWriter) readLoop(pw *io.PipeWriter) {
	var err error
	for err == nil {
		// strip headers and write data into the pipe
		var header snowflakeHeader
		err = readHeader(s.Conn, &header)
		if err != nil {
			break
		}
		s.lock.Lock()
		if header.seq == s.ack {
			_, err = io.CopyN(pw, s.Conn, int64(header.length))
			s.ack += uint32(header.length)
		} else {
			_, err = io.CopyN(ioutil.Discard, s.Conn, int64(header.length))
		}
		//save session ID from client
		if s.sessionID == nil {
			s.sessionID = header.sessionID
		}
		s.lock.Unlock()
	}
	pw.CloseWithError(err)
}

func (s *SnowflakeReadWriter) Read(b []byte) (int, error) {
	// read de-headered data from the pipe
	return s.pr.Read(b)
}

func (s *SnowflakeReadWriter) sendAck() error {

	h := new(snowflakeHeader)
	h.length = 0
	h.seq = s.seq
	s.lock.Lock()
	h.ack = s.ack
	s.lock.Unlock()

	bytes, err := h.Marshal()
	if err != nil {
		return err
	}

	if len(bytes) != snowflakeHeaderLen {
		return fmt.Errorf("Error crafting acknowledgment packet")
	}
	s.Conn.Write(bytes)

	return err
}

func (c *SnowflakeReadWriter) Write(b []byte) (n int, err error) {

	//need to append a header onto
	h := new(snowflakeHeader)
	if len(b) > maxLength {
		h.length = maxLength
		err = io.ErrShortWrite
	} else {
		h.length = uint16(len(b))
	}
	h.seq = c.seq
	c.lock.Lock()
	h.ack = c.ack
	c.lock.Unlock()

	bytes, err := h.Marshal()
	if err != nil {
		return 0, err
	}
	bytes = append(bytes, b...)
	c.seq += uint32(len(b))

	n, err2 := c.Conn.Write(bytes)
	//prioritize underlying connection error
	if err2 != nil {
		err = err2
	}
	return n, err

}

func (c *SnowflakeReadWriter) Close() error {
	return c.Conn.Close()
}
