/* Snowflake client--server protocol

   This package implements SnowflakeConn, a net.Conn for use between a Snowflake
   client and server that implements a sequence and reliable Snowflake protocol.

   The first 8 bytes sent from the client to the server at the start of every
   connection is the session ID. This is meant to be read by the server
   and mapped to a long-lived SnowflakeConn.

   The protocol sends data in chunks, accompanied by a header:
   0               4               8
   +---------------+---------------+
   | Seq Number    | Ack Number    |
   +-------+-------+---------------+
   | Len   |
   +-------+

   With a 4 byte sequence number, a 4 byte acknowledgement number, a
   2 byte length.

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
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

const snowflakeHeaderLen = 18
const maxLength = 65535
const sessionIDLength = 8
const snowflakeTimeout = 10 * time.Second

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
func (h *snowflakeHeader) marshal() []byte {
	b := make([]byte, snowflakeHeaderLen, snowflakeHeaderLen)
	binary.BigEndian.PutUint32(b[0:4], h.seq)
	binary.BigEndian.PutUint32(b[4:8], h.ack)
	binary.BigEndian.PutUint16(b[8:10], h.length)
	return b

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

type snowflakeTimer struct {
	t   *time.Timer
	seq uint32
}

func newSnowflakeTimer(seq uint32, s *SnowflakeConn) *snowflakeTimer {

	timer := &snowflakeTimer{seq: seq}
	timer.t = time.AfterFunc(snowflakeTimeout, func() {
		s.seqLock.Lock()
		if s.acked < timer.seq {
			log.Println("Closing WebRTC connection, timed out waiting for ACK")
			s.Close()
		}
		s.seqLock.Unlock()
	})
	return timer
}

// updates the timer by resetting the duration and
// the sequence number to be acknowledged
func (timer *snowflakeTimer) update(seq uint32) error {
	if !timer.t.Stop() {
		// timer has already been fired
		return fmt.Errorf("timer has already stopped")
	}

	timer.seq = seq
	timer.t.Reset(snowflakeTimeout)
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

	conn io.ReadWriteCloser
	pr   *io.PipeReader

	seqLock   sync.Mutex //lock for the seq and ack numbers
	connLock  sync.Mutex //lock for the underlying connection
	writeLock sync.Mutex //lock for writing to connection
	timerLock sync.Mutex //lock for timers

	timer *snowflakeTimer

	acked uint32
	buf   bytes.Buffer
}

func NewSnowflakeConn() *SnowflakeConn {
	s := &SnowflakeConn{}
	s.genSessionID()
	return s
}

func SetLog(w io.Writer) {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetOutput(w)
}

func (s *SnowflakeConn) genSessionID() error {
	buf := make([]byte, sessionIDLength)
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	s.SessionID = buf
	return nil
}

//Peak at header from a connection and return SessionAddr
func ReadSessionID(conn io.ReadWriteCloser) (string, error) {
	id := make([]byte, sessionIDLength)
	_, err := io.ReadFull(conn, id)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(base64.StdEncoding.EncodeToString(id), "="), nil
}

func (s *SnowflakeConn) sendSessionID() (int, error) {
	return s.conn.Write(s.SessionID)
}

func (s *SnowflakeConn) NewSnowflake(conn io.ReadWriteCloser, isClient bool) error {

	s.connLock.Lock()
	if s.conn != nil {
		s.conn.Close()
	}
	s.conn = conn
	s.connLock.Unlock()
	pr, pw := io.Pipe()
	s.pr = pr

	go s.readLoop(pw)

	// if this is a client connection, send the session ID as the first 8 bytes
	if isClient {
		n, err := s.sendSessionID()
		if err != nil {
			return err
		}
		if n != sessionIDLength {
			return fmt.Errorf("failed to write session id")
		}
	}

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
	var err error
	s.seqLock.Lock()
	if header.seq == s.ack {
		s.connLock.Lock()
		n, err = io.CopyN(pw, s.conn, int64(header.length))
		s.connLock.Unlock()
		if err != nil {
			log.Printf("Error copying bytes from WebRTC connection to pipe: %s", err.Error())
		}
		s.ack += uint32(header.length)
	} else {
		s.connLock.Lock()
		_, err = io.CopyN(ioutil.Discard, s.conn, int64(header.length))
		s.connLock.Unlock()
		if err != nil {
			log.Printf("Error discarding bytes from WebRTC connection to pipe: %s", err.Error())
		}
	}
	if int32(header.ack-s.acked) > 0 {
		// remove newly acknowledged bytes from buffer
		s.buf.Next(int(int32(header.ack - s.acked)))
		s.acked = header.ack
	}
	s.seqLock.Unlock()

	if n > 0 {
		//send acknowledgement
		go func() {
			if err := s.sendAck(); err != nil {
				log.Printf("Error sending acknowledgement")
			}
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

	bytes := h.marshal()

	if len(bytes) != snowflakeHeaderLen {
		return fmt.Errorf("Error crafting acknowledgment packet")
	}
	s.writeLock.Lock()
	_, err := s.conn.Write(bytes)
	if err != nil {
		return err
	}
	s.writeLock.Unlock()

	return err
}

//Writes bytes to the underlying connection but saves them in a buffer first.
//These bytes will remain in the buffer until they are acknowledged by the
// other end of the connection.
// Note: Write will not return an error if the underlying connection has been closed
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

	bytes := h.marshal()
	bytes = append(bytes, b...)
	s.seq += uint32(len(b))

	//save bytes to buffer until the have been acked
	s.seqLock.Lock()
	s.buf.Write(b)
	s.seqLock.Unlock()

	if s.conn == nil {
		log.Printf("Buffering %d bytes, no connection yet.", len(b))
		return len(b), nil
	}

	s.writeLock.Lock()
	n, err2 := s.conn.Write(bytes)
	s.writeLock.Unlock()
	if err2 != nil {
		log.Printf("Error writing to connection: %s", err.Error())
		return len(b), nil
	}

	if s.timer == nil {
		s.timer = newSnowflakeTimer(s.seq, s)
	} else {
		s.timer.update(s.seq)
	}

	return len(b), err

}

func (s *SnowflakeConn) Close() error {
	err := s.conn.Close()
	//terminate all waiting timers
	s.timerLock.Lock()
	s.timer.t.Stop()
	s.timerLock.Unlock()
	return err
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

// Functions similarly to io.Copy, except return a bool with value
// true if the call to src.Read caused the error and a value of false
// if the call to dst.Write caused the error
func Proxy(dst io.WriteCloser, src io.ReadCloser) (bool, error) {
	buf := make([]byte, 32*1024)
	var err error
	var readClose bool
	for {
		nr, er := src.Read(buf)
		if er != nil {
			err = er
			readClose = true
			break
		}
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if ew != nil {
				err = ew
				break
			}
			if nw != nr {
				err = io.ErrShortWrite
				break
			}
		}
	}
	return readClose, err
}
