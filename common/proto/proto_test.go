package proto

import (
	"io"
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
		c.NewSnowflake(client, nil)

		s := NewSnowflakeConn()
		s.NewSnowflake(server, nil)

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
			received = make([]byte, 3, 3)

			var n int
			var err error

			wg.Add(2)
			go func() {
				c.Write(sent)
				c.Write(sent)
				wg.Done()
			}()

			go func() {
				n, err = s.Read(received)
				buffer = append(buffer, received[:n]...)
				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, 3)
				ctx.So(buffer, ShouldResemble, sent[:3])

				n, err = s.Read(received)
				buffer = append(buffer, received[:n]...)
				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, 2)
				ctx.So(buffer, ShouldResemble, sent)

				n, err = s.Read(received)
				buffer = append(buffer, received[:n]...)
				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, 3)
				ctx.So(buffer, ShouldResemble, append(sent, sent[:3]...))

				n, err = s.Read(received)
				buffer = append(buffer, received[:n]...)
				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, 2)
				ctx.So(buffer, ShouldResemble, append(sent, sent...))

				s.seqLock.Lock()
				ctx.So(s.ack, ShouldEqual, 2*5)
				s.seqLock.Unlock()
				wg.Done()
			}()
			wg.Wait()

		})
	})
}
func TestSnowflakeProtoTimeouts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout tests in short mode")
	}
	Convey("Connection set up", t, func(ctx C) {

		client, server := net.Pipe()

		c := NewSnowflakeConn()
		c.NewSnowflake(client, nil)

		s := NewSnowflakeConn()
		s.NewSnowflake(server, nil)

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

		Convey("Check out-of-order sequence numbers", func(ctx C) {
			var sent, received []byte
			var wg sync.WaitGroup
			sent = []byte{'H', 'E', 'L', 'L', 'O'}
			received = make([]byte, len(sent), len(sent))
			c.seq = 5

			wg.Add(2)
			go func() {
				n, err := c.Write(sent)
				ctx.So(err, ShouldEqual, nil)
				ctx.So(n, ShouldEqual, len(sent))
				ctx.So(c.seq, ShouldEqual, 10)
				wg.Done()
			}()
			go func() {
				n, err := s.Read(received)
				ctx.So(err, ShouldEqual, io.EOF)
				ctx.So(n, ShouldEqual, 0)
				ctx.So(s.ack, ShouldEqual, 0)
				wg.Done()
			}()
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
			s.NewSnowflake(server, nil)
			s.ack = 0

			wg.Add(2)
			go func() {
				c.NewSnowflake(client, nil)
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
			c.acked = 5

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
