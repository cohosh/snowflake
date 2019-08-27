package proto

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"sync"
	"time"
)

// Fixed size window used for sequencing and reliability layer
const windowSize = 1500
const snowflakeHeaderLen = 18
const maxLength = 65535
const sessionIDLength = 8
const snowflakeTimeout = 10 * time.Second

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

type SnowflakeConn struct {
	seq       uint32
	ack       uint32
	sessionID []byte

	Conn io.ReadWriteCloser
	pr   *io.PipeReader
	lock sync.Mutex //need a lock on the acknowledgement since multiple goroutines access it

	timeout time.Duration
	acked   uint32
	buf     bytes.Buffer
}

func NewSnowflakeConn() *SnowflakeConn {
	s := &SnowflakeConn{
		timeout: snowflakeTimeout,
	}
	return s
}

func (s *SnowflakeConn) genSessionID() error {
	buf := make([]byte, sessionIDLength)
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	s.sessionID = buf
	return nil
}

func (s *SnowflakeConn) NewSnowflake(conn io.ReadWriteCloser) error {
	s.Conn = conn
	pr, pw := io.Pipe()
	s.pr = pr
	go s.readLoop(pw)

	//write out bytes in buffer
	if s.buf.Len() > 0 {
		s.lock.Lock()
		s.seq = s.acked
		_, err := s.Conn.Write(s.buf.Bytes())
		s.lock.Unlock()
		if err != nil {
			return err
		}
		//TODO: check to make sure we wrote out all of the bytes
	}
	return nil

}

func (s *SnowflakeConn) readLoop(pw *io.PipeWriter) {
	var err error
	for err == nil {
		// strip headers and write data into the pipe
		var header snowflakeHeader
		var n int64
		err = readHeader(s.Conn, &header)
		if err != nil {
			break
		}
		s.lock.Lock()
		if header.seq == s.ack {
			n, err = io.CopyN(pw, s.Conn, int64(header.length))
			s.ack += uint32(header.length)
		} else {
			_, err = io.CopyN(ioutil.Discard, s.Conn, int64(header.length))
		}
		if header.ack > s.acked {
			// remove newly acknowledged bytes from buffer
			s.buf.Next(int(header.ack - s.acked))
			s.acked = header.ack
		}
		//save session ID from client
		if s.sessionID == nil {
			s.sessionID = header.sessionID
		}
		s.lock.Unlock()

		if n > 0 {
			//send acknowledgement
			s.sendAck()
		}
	}
	pw.CloseWithError(err)
}

func (s *SnowflakeConn) Read(b []byte) (int, error) {
	// read de-headered data from the pipe
	if s.Conn == nil {
		return 0, fmt.Errorf("No network connection to read from ")
	}
	return s.pr.Read(b)
}

func (s *SnowflakeConn) sendAck() error {

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

//Writes bytes to the underlying connection but saves them in a buffer first.
//These bytes will remain in the buffer until they are acknowledged by the
// other end of the connection.
func (c *SnowflakeConn) Write(b []byte) (n int, err error) {

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

	//save bytes to buffer until the have been acked
	c.lock.Lock()
	c.buf.Write(b)
	c.lock.Unlock()

	if c.Conn == nil {
		return len(b), fmt.Errorf("No network connection to write to.")
	}

	n, err2 := c.Conn.Write(bytes)
	//prioritize underlying connection error
	if err2 != nil {
		return len(b), err2
	}

	//set a timer on the acknowledgement
	sentSeq := c.seq
	time.AfterFunc(c.timeout, func() {
		c.lock.Lock()
		if c.acked < sentSeq {
			c.Close()
		}
		c.lock.Unlock()
	})

	return len(b), err

}

func (c *SnowflakeConn) Close() error {
	return c.Conn.Close()
}

func (c *SnowflakeConn) LocalAddr() net.Addr {
	return nil
}

func (c *SnowflakeConn) RemoteAddr() net.Addr {
	return nil
}

func (c *SnowflakeConn) SetDeadline(t time.Time) error {
	return fmt.Errorf("SetDeadline not implemented")
}

func (c *SnowflakeConn) SetReadDeadline(t time.Time) error {
	return fmt.Errorf("SetReadDeadline not implemented")
}

func (c *SnowflakeConn) SetWriteDeadline(t time.Time) error {
	return fmt.Errorf("SetWriteDeadline not implemented")
}
