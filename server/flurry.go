package main

import (
	"net"
	"sync"

	"git.torproject.org/pluggable-transports/snowflake.git/common/proto"
)

// Map of open E2E snowflake connections with client
type Flurries struct {
	flurries map[string]*Flurry
	lock     sync.Mutex
}

type Flurry struct {
	Addr net.Addr
	Conn *proto.SnowflakeConn
	Or   net.Conn
}

func newFlurries() *Flurries {
	return &Flurries{
		flurries: make(map[string]*Flurry),
	}
}

func (f *Flurries) Get(addr net.Addr) *Flurry {
	f.lock.Lock()
	defer f.lock.Unlock()
	if flurry, ok := f.flurries[addr.String()]; ok {
		return flurry
	} else {
		return nil
	}
}

func (f *Flurries) Add(addr net.Addr, conn *proto.SnowflakeConn, or net.Conn) *Flurry {
	f.lock.Lock()
	defer f.lock.Unlock()
	flurry := &Flurry{
		Addr: addr,
		Conn: conn,
		Or:   or,
	}
	f.flurries[addr.String()] = flurry
	return flurry
}

func (f *Flurries) Delete(addr net.Addr) {
	f.lock.Lock()
	defer f.lock.Unlock()
	delete(f.flurries, addr.String())
}
