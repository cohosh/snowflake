package lib

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"

	"git.torproject.org/pluggable-transports/snowflake.git/common/snowflake-proto"
)

const (
	ReconnectTimeout = 10
	SnowflakeTimeout = 30
)

// When a connection handler starts, +1 is written to this channel; when it
// ends, -1 is written.
var HandlerChan = make(chan int)

// Given an accepted SOCKS connection, establish a WebRTC connection to the
// remote peer and exchange traffic.
func Handler(socks SocksConnector, snowflakes SnowflakeCollector) error {
	var err error
	HandlerChan <- 1
	defer func() {
		HandlerChan <- -1
	}()
	defer socks.Close()
	sConn := proto.NewSnowflakeConn()
	sConn.GenSessionID()

	for err == nil {
		// Obtain an available WebRTC remote. May block.
		snowflake := snowflakes.Pop()
		if nil == snowflake {
			return errors.New("handler: Received invalid Snowflake")
		}
		log.Println("---- Handler: snowflake assigned ----")
		err := socks.Grant(&net.TCPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			return err
		}

		go func() {
			// When WebRTC resets, log message
			snowflake.WaitForReset()
			log.Println("Snowflake reset, gathering new snowflake")
		}()

		sConn.NewSnowflake(snowflake, nil)

		// Begin exchanging data. Either WebRTC or localhost SOCKS will close first.
		// In eithercase, this closes the handler and induces a new handler.
		log.Println("---- Copy Loop started, snowflake opened ---")
		copyLoop(socks, sConn)
		log.Println("---- Copy Loop ended, snowflake closed ---")
		snowflake.Close()
	}
	log.Println("---- Handler: closed ---")
	return nil
}

// Exchanges bytes between two ReadWriters.
// (In this case, between a SOCKS and WebRTC connection.)
func copyLoop(a, b io.ReadWriter) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		io.Copy(b, a)
		log.Println("Stopping copying from socks to sConn")
		wg.Done()
	}()
	go func() {
		io.Copy(a, b)
		log.Println("Stopping copying from sConn to socks")
		wg.Done()
	}()
	wg.Wait()
	log.Println("copy loop ended")
}
