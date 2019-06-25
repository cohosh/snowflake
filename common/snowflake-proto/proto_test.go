package proto

import (
	"bytes"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

type stubConn struct {
	Buf *bytes.Buffer
}

func (c *stubConn) Read(b []byte) (int, error) {
	return c.Buf.Read(b)
}

func (c *stubConn) Write(b []byte) (int, error) {
	return c.Buf.Write(b)
}

func (c *stubConn) Close() error {
	return nil
}

func TestSnowflakeProto(t *testing.T) {
	Convey("Connection set up", t, func() {

		buffer := new(bytes.Buffer)
		sConn := &stubConn{Buf: buffer}

		s := NewSnowflakeReadWriter(sConn)

		Convey("Create correct headers", func() {
			var sent, received, wire []byte
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			wire = []byte{
				0x00, 0x00, 0x00, 0x00, //seq
				0x00, 0x00, 0x00, 0x00, //ack
				0x00, 0x05, //len
				'H', 'E', 'L', 'L', 'O',
			}
			received = make([]byte, len(wire), len(wire))

			n, err := s.Write(sent)

			So(n, ShouldEqual, len(sent)+snowflakeHeaderLen)
			So(err, ShouldEqual, nil)

			n, err = s.Read(received)

			So(err, ShouldEqual, nil)
			So(n, ShouldEqual, len(sent))
			So(received[:n], ShouldResemble, sent)

			//Make sure seq and ack have been updated
			So(s.seq, ShouldEqual, 5)
			So(s.ack, ShouldEqual, 5)

			// Check that acknowledgement packet was written
			//n, err = s.Read(received)
			//So(err, ShouldEqual, nil)
			//So(n, ShouldEqual, 0)

		})

		Convey("Partial reads work correctly", func() {
			var sent, received []byte
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			received = make([]byte, 3, 3)

			n, err := s.Write(sent)

			//Read in first part of message
			n, err = s.Read(received)

			So(err, ShouldEqual, nil)
			So(n, ShouldEqual, 3)
			So(received[:n], ShouldResemble, sent[:n])

			//Read in rest of message
			n2, err := s.Read(received)

			So(err, ShouldEqual, nil)
			So(n2, ShouldEqual, 2)
			So(received[:n2], ShouldResemble, sent[n:n+n2])
			So(s.ack, ShouldEqual, 5)
		})

		Convey("Test reading multiple chunks", func() {
			var sent, received, buffer []byte
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			received = make([]byte, 3, 3)

			n, err := s.Write(sent)
			n, err = s.Write(sent)

			n, err = s.Read(received)
			buffer = append(buffer, received[:n]...)
			So(err, ShouldEqual, nil)
			So(n, ShouldEqual, 3)
			So(buffer, ShouldResemble, sent[:3])

			n, err = s.Read(received)
			buffer = append(buffer, received[:n]...)
			So(err, ShouldEqual, nil)
			So(n, ShouldEqual, 2)
			So(buffer, ShouldResemble, sent)

			n, err = s.Read(received)
			buffer = append(buffer, received[:n]...)
			So(err, ShouldEqual, nil)
			So(n, ShouldEqual, 3)
			So(buffer, ShouldResemble, append(sent, sent[:3]...))

			n, err = s.Read(received)
			buffer = append(buffer, received[:n]...)
			So(err, ShouldEqual, nil)
			So(n, ShouldEqual, 2)
			So(buffer, ShouldResemble, append(sent, sent...))

			So(s.ack, ShouldEqual, 2*5)

		})
	})

}
