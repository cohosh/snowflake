package proto

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Fixed size window used for sequencing and reliability layer
const windowSize = 1500
const snowflakeHeaderLen = 12

type snowflakeHeader struct {
	seq    int
	ack    int
	length int //length of the accompanying data (including header length)
}

func (h *snowflakeHeader) Parse(b []byte) error {
	h.seq = int(binary.LittleEndian.Uint32(b[0:4]))
	h.ack = int(binary.LittleEndian.Uint32(b[4:8]))
	h.length = int(binary.LittleEndian.Uint32(b[8:12]))

	return nil
}

// Converts a header to bytes
func (h *snowflakeHeader) Marshal() ([]byte, error) {
	if h == nil {
		return nil, fmt.Errorf("nil header")
	}
	b := make([]byte, 12, 12)
	binary.LittleEndian.PutUint32(b[0:4], uint32(h.seq))
	binary.LittleEndian.PutUint32(b[4:8], uint32(h.ack))
	binary.LittleEndian.PutUint32(b[8:12], uint32(h.length))

	return b, nil

}

// Parses a webRTC header from bytes received on the
// webRTC connection
func ParseHeader(b []byte) (*snowflakeHeader, error) {

	h := new(snowflakeHeader)
	if err := h.Parse(b); err != nil {
		return nil, err
	}
	return h, nil
}

type SnowflakeReadWriter struct {
	seq int
	ack int

	Conn io.ReadWriteCloser

	buffer    []byte
	remaining int
	out       []byte
}

func (s *SnowflakeReadWriter) Read(b []byte) (int, error) {
	length, err := s.Conn.Read(b)
	if err != nil {
		return length, err
	}
	s.buffer = append(s.buffer, b...)

	n := copy(b, s.out)
	s.out = s.out[n:]
	if n == len(b) {
		return n, err
	}

	for len(s.buffer) > 0 {
		if len(s.buffer) < snowflakeHeaderLen {
			//we don't have enough data for a full header yet
			return n, err
		}

		// first read in the header and update the sequence and acknowledgement numbers
		header, err := ParseHeader(s.buffer)
		if err != nil {
			return n, err
		}
		if len(s.buffer) < header.length {
			//we don't have a full chunk yet
			return n, err
		}
		//for now, drop all data with an incorrect sequence number
		if header.seq == s.ack {

			s.out = append(s.out, s.buffer[snowflakeHeaderLen:header.length]...)
			s.ack += header.length - snowflakeHeaderLen
			s.sendAck() // write an empty length header acknowledging data
		}
		s.buffer = s.buffer[header.length:]

		n += copy(b[n:], s.out)
		s.out = s.out[n:]
	}
	return n, err
}

func (s *SnowflakeReadWriter) sendAck() error {

	h := new(snowflakeHeader)
	h.length = snowflakeHeaderLen
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

func (c *SnowflakeReadWriter) Write(b []byte) (int, error) {

	//need to append a header onto
	h := new(snowflakeHeader)
	h.length = len(b) + snowflakeHeaderLen
	h.seq = c.seq
	h.ack = c.ack

	bytes, err := h.Marshal()
	if err != nil {
		return 0, err
	}
	bytes = append(bytes, b...)
	c.seq += len(b)

	return c.Conn.Write(bytes)
}

func (c *SnowflakeReadWriter) Close() error {
	return c.Conn.Close()
}
