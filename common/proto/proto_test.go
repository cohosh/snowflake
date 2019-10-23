package proto

import (
	"io"
	"io/ioutil"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSnowflakeProto(t *testing.T) {
	Convey("Connection set up", t, func(ctx C) {

		client, server := net.Pipe()

		c := NewSnowflakeConn()
		c.NewSnowflake(client, false)

		s := NewSnowflakeConn()
		s.NewSnowflake(server, false)

		Convey("Create correct headers", func(ctx C) {
			var sent, received []byte
			var wg sync.WaitGroup
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			received = make([]byte, len(sent), len(sent))

			wg.Add(2)
			go func() {
				n, err := c.Write(sent)
				ctx.So(n, ShouldEqual, len(sent))
				ctx.So(err, ShouldEqual, nil)
				ctx.So(c.seq, ShouldEqual, 5)
				wg.Done()
			}()

			go func() {
				n, err := s.Read(received)

				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, len(sent))
				ctx.So(received[:n], ShouldResemble, sent)
				s.seqLock.Lock()
				ctx.So(s.ack, ShouldEqual, 5)
				s.seqLock.Unlock()
				wg.Done()
			}()

			wg.Wait()

		})

		Convey("Partial reads work correctly", func(ctx C) {
			var sent, received []byte
			var wg sync.WaitGroup
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			received = make([]byte, 3, 3)

			wg.Add(2)
			go func() {
				n, err := c.Write(sent)
				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, 5)
				wg.Done()
			}()

			//Read in first part of message
			go func() {
				n, err := s.Read(received)

				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, 3)
				ctx.So(received[:n], ShouldResemble, sent[:n])

				//Read in rest of message
				n2, err := s.Read(received)

				ctx.So(err, ShouldEqual, nil)
				ctx.So(n2, ShouldEqual, 2)
				ctx.So(received[:n2], ShouldResemble, sent[n:n+n2])

				s.seqLock.Lock()
				ctx.So(s.ack, ShouldEqual, 5)
				s.seqLock.Unlock()
				wg.Done()
			}()

			wg.Wait()

		})

		Convey("Test reading multiple chunks", func(ctx C) {
			var sent, received, buffer []byte
			var wg sync.WaitGroup
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			received = make([]byte, 3)

			var n int
			var err error

			wg.Add(2)
			go func() {
				c.Write(sent)
				c.Write(sent)
				wg.Done()
			}()

			go func() {
				for i := 0; i < 3; i++ {
					n, err = io.ReadFull(s, received)
					buffer = append(buffer, received[:n]...)
					ctx.So(err, ShouldEqual, nil)
					ctx.So(n, ShouldEqual, 3)
				}

				n, err = s.Read(received)
				buffer = append(buffer, received[:n]...)
				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, 1)
				ctx.So(buffer, ShouldResemble, append(sent, sent...))

				s.seqLock.Lock()
				ctx.So(s.ack, ShouldEqual, 2*5)
				s.seqLock.Unlock()
				wg.Done()
			}()
			wg.Wait()

		})
		Convey("Check out-of-order sequence numbers", func(ctx C) {
			go func() {
				hb := (&snowflakeHeader{
					seq:    5,
					ack:    0,
					length: 5,
				}).marshal()
				// Write future packet
				client.Write(hb)
				client.Write([]byte("HELLO"))

				hb = (&snowflakeHeader{
					seq:    0,
					ack:    0,
					length: 5,
				}).marshal()
				// Write first 5 bytes
				client.Write(hb)
				client.Write([]byte("HELLO"))
				client.Close()
			}()
			received, err := ioutil.ReadAll(s)
			ctx.So(err, ShouldEqual, nil)
			ctx.So(received, ShouldResemble, []byte("HELLO"))
		})
		Convey("Overlapping sequence numbers", func(ctx C) {
			go func() {
				hb := (&snowflakeHeader{
					seq:    0,
					ack:    0,
					length: 5,
				}).marshal()
				// Write first 5 bytes.
				client.Write(hb)
				client.Write([]byte("HELLO"))
				// Exact duplicate packet.
				client.Write(hb)
				client.Write([]byte("HELLO"))

				hb = (&snowflakeHeader{
					seq:    3,
					ack:    0,
					length: 5,
				}).marshal()
				// Overlapping sequence number -- should be
				// rejected (not overwrite what was already
				// received) and bytes past the expected seq
				// ignored.
				client.Write(hb)
				client.Write([]byte("XXXXX"))

				// Now the expected sequence number.
				hb = (&snowflakeHeader{
					seq:    5,
					ack:    0,
					length: 5,
				}).marshal()
				client.Write(hb)
				client.Write([]byte("WORLD"))

				client.Close()
			}()

			received, err := ioutil.ReadAll(s)
			ctx.So(err, ShouldEqual, nil)
			ctx.So(received, ShouldResemble, []byte("HELLOWORLD"))
		})
	})
}
func TestSnowflakeProtoTimeouts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout tests in short mode")
	}
	snowflakeTimeout = time.Second
	Convey("Connection set up", t, func(ctx C) {

		client, server := net.Pipe()

		c := NewSnowflakeConn()
		c.NewSnowflake(client, false)

		s := NewSnowflakeConn()
		s.NewSnowflake(server, false)

		Convey("Check timeout", func(ctx C) {
			var sent, received []byte
			var wg sync.WaitGroup
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			received = make([]byte, len(sent), len(sent))

			wg.Add(2)
			go func() {
				n, err := c.Write(sent)
				ctx.So(n, ShouldEqual, len(sent))
				ctx.So(err, ShouldEqual, nil)
				ctx.So(c.seq, ShouldEqual, 5)
				wg.Done()
			}()
			go func() {
				s.Read(received)
				wg.Done()
			}()
			wg.Wait()
			wg.Add(1)
			time.AfterFunc(snowflakeTimeout, func() {
				//check to see that bytes were acknowledged
				c.seqLock.Lock()
				ctx.So(c.buf.Len(), ShouldEqual, 0)
				c.seqLock.Unlock()
				wg.Done()
			})
			wg.Wait()
		})

		Convey("Check sequence number overflow", func(ctx C) {
			var sent, received []byte
			var wg sync.WaitGroup
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			received = make([]byte, len(sent), len(sent))
			c.seq = math.MaxUint32 - 2
			s.ack = math.MaxUint32 - 2
			c.acked = math.MaxUint32 - 2

			wg.Add(2)
			go func() {
				n, err := c.Write(sent)
				ctx.So(n, ShouldEqual, len(sent))
				ctx.So(err, ShouldEqual, nil)
				ctx.So(c.seq, ShouldEqual, 2)
				wg.Done()
			}()
			go func() {
				s.Read(received)
				wg.Done()
			}()
			wg.Wait()
			wg.Add(1)
			time.AfterFunc(snowflakeTimeout, func() {
				//check to see that bytes were acknowledged
				c.seqLock.Lock()
				ctx.So(s.ack, ShouldEqual, 2)
				ctx.So(c.acked, ShouldEqual, 2)
				ctx.So(c.buf.Len(), ShouldEqual, 0)
				c.seqLock.Unlock()
				wg.Done()
			})
			wg.Wait()
		})
		Convey("Check that NewSnowflake sends buffered data", func(ctx C) {
			var sent, received []byte
			var wg sync.WaitGroup
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			received = make([]byte, len(sent), len(sent))
			s.ack = 5 //simulate old snowflke not acknowledging data

			wg.Add(2)
			go func() {
				n, err := c.Write(sent)
				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, len(sent))
				ctx.So(c.seq, ShouldEqual, 5)
				wg.Done()
			}()
			go func() {
				n, err := s.Read(received)
				ctx.So(err, ShouldEqual, io.EOF)
				ctx.So(n, ShouldEqual, 0)
				ctx.So(s.ack, ShouldEqual, 5)
				wg.Done()
			}()
			wg.Wait()

			wg.Add(1)
			//Make sure bytes weren't acknowledged
			time.AfterFunc(snowflakeTimeout, func() {
				c.seqLock.Lock()
				ctx.So(c.acked, ShouldEqual, 0)
				ctx.So(c.buf.Len(), ShouldEqual, 5)
				c.seqLock.Unlock()
				wg.Done()
			})
			wg.Wait()

			//Now call NewSnowflake
			client, server = net.Pipe()
			s.NewSnowflake(server, false)
			s.ack = 0

			wg.Add(2)
			go func() {
				c.NewSnowflake(client, false)
				ctx.So(c.seq, ShouldEqual, 5)
				wg.Done()
			}()
			go func() {
				n, err := s.Read(received)
				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, 5)
				wg.Done()
			}()
			wg.Wait()

			wg.Add(1)
			time.AfterFunc(snowflakeTimeout, func() {
				//check to see that bytes were acknowledged
				c.seqLock.Lock()
				ctx.So(s.ack, ShouldEqual, 5)
				ctx.So(c.acked, ShouldEqual, 5)
				ctx.So(c.buf.Len(), ShouldEqual, 0)
				c.seqLock.Unlock()
				wg.Done()
			})
			wg.Wait()
		})
		Convey("Check timer update", func(ctx C) {

			var wg sync.WaitGroup
			timer := newSnowflakeTimer(s.seq, c)
			So(timer.seq, ShouldEqual, 0)
			s.seq = 5
			timer.update(s.seq)
			So(timer.seq, ShouldEqual, 5)
			c.seqLock.Lock()
			c.acked = 5
			c.seqLock.Unlock()

			wg.Add(1)
			time.AfterFunc(snowflakeTimeout, func() {
				//check to see that bytes weren't acknowledged
				c.seqLock.Lock()
				ctx.So(c.buf.Len(), ShouldEqual, 0)
				c.seqLock.Unlock()
				wg.Done()
			})
			wg.Wait()
		})
	})
}
