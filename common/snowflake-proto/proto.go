/* Snowflake client--server protocol

   This package implements SnowflakeConn, a net.Conn for use between a Snowflake
   client and server that implements a sequence and reliable Snowflake protocol.

   The protocol sends data in chunks, accompanied by a header:
   0               4               8
   +---------------+---------------+
   | Seq Number    | Ack Number    |
   +-------+-------+---------------+
   | Len   | sID                   |
   +-------+-----------------------+
   | sID   |
   +-------+

   With a 4 byte sequence number, a 4 byte acknowledgement number, a
   2 byte length, and an 8 byte session ID.

   Each SnowflakeConn is initialized with a call to NewSnowflakeConn() and
   an underlying connection is set with the call NewSnowflake(). Since Snowflakes
   are ephemeral, a new snowflake can be set at any time.

   This net.Conn is reliable, so any bytes sent as a call to SnowflakeConn's Write
   method will be buffered until they are acknowledged by the other end. If a new
   snowflake is provided, buffered bytes will be resent through the new connection
   and remain buffered until they are acknowledged.

   When a SnowflakeConn reads in bytes, it automatically sends an empty
   acknowledgement packet to the other end of the connection with the Ack number
   updated to reflect the most recently received data. Only when an endpoint
   receives a packet with an updated acknowledgement number will it remove that
   data from the stored buffer.

*/

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

	_, err := io.ReadFull(r, b)
	if err != nil {
		return err
	}

	if err := h.Parse(b); err != nil {
		return err
	}
	return nil
}

// SessionAddr implements the net.Addr interface and is set to the snowflake
//  sessionID by SnowflakeConn
type SessionAddr []byte

func (addr SessionAddr) Network() string {
	return "session"
}
func (addr SessionAddr) String() string {
	return string(addr)
}

type SnowflakeConn struct {
	seq       uint32
	ack       uint32
	SessionID SessionAddr

	conn      io.ReadWriteCloser
	pr        *io.PipeReader
	seqLock   sync.Mutex //lock for the seq and ack numbers
	connLock  sync.Mutex //lock for the underlying connection
	writeLock sync.Mutex //lock for writing to connection

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

func (s *SnowflakeConn) GenSessionID() error {
	buf := make([]byte, sessionIDLength)
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	s.SessionID = buf
	return nil
}

//Peak at header from a connection and return SessionAddr
func ReadSessionID(conn io.ReadWriteCloser) (SessionAddr, *snowflakeHeader, error) {
	var header snowflakeHeader
	if err := readHeader(conn, &header); err != nil {
		return nil, nil, err
	}

	return header.sessionID, &header, nil
}

func (s *SnowflakeConn) NewSnowflake(conn io.ReadWriteCloser, header *snowflakeHeader) error {

	s.connLock.Lock()
	if s.conn != nil {
		s.conn.Close()
	}
	s.conn = conn
	s.connLock.Unlock()
	pr, pw := io.Pipe()
	s.pr = pr

	// If header is not nil, ready snowflake body
	if header != nil {
		s.readBody(*header, pw)
	}
	go s.readLoop(pw)

	// Write out bytes in buffer
	if s.buf.Len() > 0 {
		s.seqLock.Lock()
		s.seq = s.acked
		s.seqLock.Unlock()
		_, err := s.Write(s.buf.Next(s.buf.Len()))
		if err != nil {
			return err
		}
	}

	return nil

}

func (s *SnowflakeConn) readBody(header snowflakeHeader, pw *io.PipeWriter) {
	var n int64
	s.seqLock.Lock()
	if header.seq == s.ack {
		s.connLock.Lock()
		n, _ = io.CopyN(pw, s.conn, int64(header.length))
		s.ack += uint32(header.length)
		s.connLock.Unlock()
	} else {
		s.connLock.Lock()
		io.CopyN(ioutil.Discard, s.conn, int64(header.length))
		s.connLock.Unlock()
	}
	if int32(header.ack-s.acked) > 0 {
		// remove newly acknowledged bytes from buffer
		s.buf.Next(int(int32(header.ack - s.acked)))
		s.acked = header.ack
	}
	//save session ID from client
	if s.SessionID == nil {
		s.SessionID = header.sessionID
	}
	s.seqLock.Unlock()

	if n > 0 {
		//send acknowledgement
		go func() {
			s.sendAck()
		}()
	}
}

func (s *SnowflakeConn) readLoop(pw *io.PipeWriter) {
	var err error
	for err == nil {
		// strip headers and write data into the pipe
		var header snowflakeHeader
		s.connLock.Lock()
		err = readHeader(s.conn, &header)
		s.connLock.Unlock()
		if err != nil {
			break
		}
		s.readBody(header, pw)
	}
	pw.CloseWithError(err)
}

func (s *SnowflakeConn) Read(b []byte) (int, error) {
	// read de-headered data from the pipe
	return s.pr.Read(b)
}

func (s *SnowflakeConn) sendAck() error {

	h := new(snowflakeHeader)
	h.length = 0
	h.seq = s.seq
	s.seqLock.Lock()
	h.ack = s.ack
	s.seqLock.Unlock()

	bytes, err := h.Marshal()
	if err != nil {
		return err
	}

	if len(bytes) != snowflakeHeaderLen {
		return fmt.Errorf("Error crafting acknowledgment packet")
	}
	s.writeLock.Lock()
	_, err = s.conn.Write(bytes)
	if err != nil {
		return err
	}
	s.writeLock.Unlock()

	return err
}

//Writes bytes to the underlying connection but saves them in a buffer first.
//These bytes will remain in the buffer until they are acknowledged by the
// other end of the connection.
func (s *SnowflakeConn) Write(b []byte) (n int, err error) {

	//need to append a header onto
	h := new(snowflakeHeader)
	if len(b) > maxLength {
		h.length = maxLength
		err = io.ErrShortWrite
	} else {
		h.length = uint16(len(b))
	}
	h.seq = s.seq
	s.seqLock.Lock()
	h.ack = s.ack
	s.seqLock.Unlock()

	bytes, err := h.Marshal()
	if err != nil {
		return 0, err
	}
	bytes = append(bytes, b...)
	s.seq += uint32(len(b))

	//save bytes to buffer until the have been acked
	s.seqLock.Lock()
	s.buf.Write(b)
	s.seqLock.Unlock()

	if s.conn == nil {
		return len(b), fmt.Errorf("No network connection to write to.")
	}

	s.writeLock.Lock()
	n, err2 := s.conn.Write(bytes)
	s.writeLock.Unlock()
	//prioritize underlying connection error
	if err2 != nil {
		return len(b), err2
	}

	//set a timer on the acknowledgement
	sentSeq := s.seq
	time.AfterFunc(s.timeout, func() {
		s.seqLock.Lock()
		if s.acked < sentSeq {
			s.Close()
		}
		s.seqLock.Unlock()
	})

	return len(b), err

}

func (s *SnowflakeConn) Close() error {
	return s.conn.Close()
}

func (s *SnowflakeConn) LocalAddr() net.Addr {
	return s.SessionID
}

func (s *SnowflakeConn) RemoteAddr() net.Addr {
	return s.SessionID
}

func (s *SnowflakeConn) SetDeadline(t time.Time) error {
	return fmt.Errorf("SetDeadline not implemented")
}

func (s *SnowflakeConn) SetReadDeadline(t time.Time) error {
	return fmt.Errorf("SetReadDeadline not implemented")
}

func (s *SnowflakeConn) SetWriteDeadline(t time.Time) error {
	return fmt.Errorf("SetWriteDeadline not implemented")
}
