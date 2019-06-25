package proto

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
)

// Fixed size window used for sequencing and reliability layer
const windowSize = 1500
const snowflakeHeaderLen = 10
const maxLength = 65535

type snowflakeHeader struct {
	seq    uint32
	ack    uint32
	length uint16 //length of the accompanying data (excluding header length)
}

func (h *snowflakeHeader) Parse(b []byte) error {
	h.seq = binary.BigEndian.Uint32(b[0:4])
	h.ack = binary.BigEndian.Uint32(b[4:8])
	h.length = binary.BigEndian.Uint16(b[8:10])

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
	seq uint32
	ack uint32

	Conn io.ReadWriteCloser
	pr   *io.PipeReader
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

func (s *SnowflakeReadWriter) readLoop(pw *io.PipeWriter) {
	var err error
	for err == nil {
		// strip headers and write data into the pipe
		var header snowflakeHeader
		err = readHeader(s.Conn, &header)
		if err != nil {
			break
		}
		if header.seq == s.ack {
			_, err = io.CopyN(pw, s.Conn, int64(header.length))
			s.ack += uint32(header.length)
		} else {
			_, err = io.CopyN(ioutil.Discard, s.Conn, int64(header.length))
		}
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
	h.ack = s.ack

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
	h.ack = c.ack

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