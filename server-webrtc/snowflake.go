package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	pt "git.torproject.org/pluggable-transports/goptlib.git"
	"github.com/pion/webrtc/v2"
)

var ptMethodName = "snowflake"
var ptInfo pt.ServerInfo
var logFile *os.File

func copyLoop(webRTC, orPort net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		if _, err := io.Copy(orPort, webRTC); err != nil {
			log.Printf("copy WebRTC to ORPort error in copyLoop: %v", err)
		}
		wg.Done()
	}()
	go func() {
		if _, err := io.Copy(webRTC, orPort); err != nil {
			log.Printf("copy ORPort to WebRTC error in copyLoop: %v", err)
		}
		wg.Done()
	}()
	wg.Wait()
}

type webRTCConn struct {
	dc *webrtc.DataChannel
	pc *webrtc.PeerConnection
	pr *io.PipeReader

	lock sync.Mutex // Synchronization for DataChannel destruction
	once sync.Once  // Synchronization for PeerConnection destruction
}

func (c *webRTCConn) Read(b []byte) (int, error) {
	return c.pr.Read(b)
}

func (c *webRTCConn) Write(b []byte) (int, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	// log.Printf("webrtc Write %d %+q", len(b), string(b))
	log.Printf("Write %d bytes --> WebRTC", len(b))
	if c.dc != nil {
		c.dc.Send(b)
	}
	return len(b), nil
}

func (c *webRTCConn) Close() (err error) {
	c.once.Do(func() {
		err = c.pc.Close()
	})
	return
}

func (c *webRTCConn) LocalAddr() net.Addr {
	return nil
}

func (c *webRTCConn) RemoteAddr() net.Addr {
	return nil
}

func (c *webRTCConn) SetDeadline(t time.Time) error {
	// nolint:golint
	return fmt.Errorf("SetDeadline not implemented")
}

func (c *webRTCConn) SetReadDeadline(t time.Time) error {
	// nolint:golint
	return fmt.Errorf("SetReadDeadline not implemented")
}

func (c *webRTCConn) SetWriteDeadline(t time.Time) error {
	// nolint:golint
	return fmt.Errorf("SetWriteDeadline not implemented")
}

func datachannelHandler(conn *webRTCConn) {
	defer conn.Close()

	or, err := pt.DialOr(&ptInfo, "", ptMethodName) // TODO: Extended OR
	if err != nil {
		log.Printf("Failed to connect to ORPort: " + err.Error())
		return
	}
	defer or.Close()

	copyLoop(conn, or)
}

// Create a PeerConnection from an SDP offer. Blocks until the gathering of ICE
// candidates is complete and the answer is available in LocalDescription.
// Installs an OnDataChannel callback that creates a webRTCConn and passes it to
// datachannelHandler.
func makePeerConnectionFromOffer(sdp *webrtc.SessionDescription, config *webrtc.Configuration) (*webrtc.PeerConnection, error) {
	pc, err := webrtc.NewPeerConnection(*config)
	if err != nil {
		return nil, fmt.Errorf("accept: NewPeerConnection: %s", err)
	}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Println("OnDataChannel")

		pr, pw := io.Pipe()
		conn := &webRTCConn{pc: pc, dc: dc, pr: pr}

		dc.OnOpen(func() {
			log.Println("OnOpen channel")
		})
		dc.OnClose(func() {
			conn.lock.Lock()
			defer conn.lock.Unlock()
			log.Println("OnClose channel")
			conn.dc = nil
			dc.Close()
			pw.Close()
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			log.Printf("OnMessage <--- %d bytes", len(msg.Data))
			var n int
			n, err = pw.Write(msg.Data)
			if err != nil {
				if inerr := pw.CloseWithError(err); inerr != nil {
					log.Printf("close with error returned error: %v", inerr)
				}
			}
			if n != len(msg.Data) {
				panic("short write")
			}
		})

		go datachannelHandler(conn)
	})

	err = pc.SetRemoteDescription(*sdp)
	if err != nil {
		if err = pc.Close(); err != nil {
			log.Printf("pc.Destroy returned an error: %v", err)
		}
		return nil, fmt.Errorf("accept: SetRemoteDescription: %s", err)
	}
	log.Println("sdp offer successfully received.")

	log.Println("Generating answer...")
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		if err = pc.Close(); err != nil {
			log.Printf("pc.Destroy returned an error: %v", err)
		}
		return nil, err
	}

	err = pc.SetLocalDescription(answer)
	if err != nil {
		if err = pc.Close(); err != nil {
			log.Printf("pc.Destroy returned an error: %v", err)
		}
		return nil, err
	}

	return pc, nil
}

func main() {
	var httpAddr string
	var logFilename string

	flag.StringVar(&httpAddr, "http", "", "listen for HTTP signaling")
	flag.StringVar(&logFilename, "log", "", "log file to write to")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.LUTC)
	if logFilename != "" {
		f, err := os.OpenFile(logFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatalf("can't open log file: %s", err)
		}
		defer logFile.Close()
		log.SetOutput(f)
	}

	log.Println("starting")
	var err error
	ptInfo, err = pt.ServerSetup(nil)
	if err != nil {
		log.Fatal(err)
	}

	webRTCConfig := &webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Start HTTP-based signaling receiver.
	go func() {
		err := receiveSignalsHTTP(httpAddr, webRTCConfig)
		if err != nil {
			log.Printf("receiveSignalsHTTP: %s", err)
		}
	}()

	for _, bindaddr := range ptInfo.Bindaddrs {
		switch bindaddr.MethodName {
		case ptMethodName:
			bindaddr.Addr.Port = 12345 // lies!!!
			pt.Smethod(bindaddr.MethodName, bindaddr.Addr)
		default:
			pt.SmethodError(bindaddr.MethodName, "no such method")
		}
	}
	pt.SmethodsDone()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)

	if os.Getenv("TOR_PT_EXIT_ON_STDIN_CLOSE") == "1" {
		// This environment variable means we should treat EOF on stdin
		// just like SIGTERM: https://bugs.torproject.org/15435.
		go func() {
			if _, err := io.Copy(ioutil.Discard, os.Stdin); err != nil {
				log.Printf("error copying os.Stdin to ioutil.Discard: %v", err)
			}
			log.Printf("synthesizing SIGTERM because of stdin close")
			sigChan <- syscall.SIGTERM
		}()
	}

	// wait for a signal
	<-sigChan
}
