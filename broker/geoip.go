/*
This code is for loading database data that maps ip addresses to countries
for collecting and presenting statistics on snowflake use that might alert us
to censorship events.

The functions here are heavily based off of how tor maintains and searches their
geoip database

The tables used for geoip data must be structured as follows:

Recognized line formats for IPv4 are:
    INTIPLOW,INTIPHIGH,CC
        and
    "INTIPLOW","INTIPHIGH","CC","CC3","COUNTRY NAME"
        where INTIPLOW and INTIPHIGH are IPv4 addresses encoded as 4-byte unsigned
        integers, and CC is a country code.

Recognized line format for IPv6 is:
    IPV6LOW,IPV6HIGH,CC
        where IPV6LOW and IPV6HIGH are IPv6 addresses and CC is a country code.

It also recognizes, and skips over, blank lines and lines that start
with '#' (comments).

*/
package main

import(
    "net"
    "sort"
    "os"
    "bufio"
    "strings"
    "log"
    "strconv"
)

type GeoIPTable interface {
    parseEntry(string) error
    Len() int
    ElementAt(int) GeoIPEntry
}

type GeoIPEntry struct {
    ipLow net.IP
    ipHigh net.IP
    country string
}

type GeoIPv4Table []GeoIPEntry
type GeoIPv6Table []GeoIPEntry

type GeoipError struct {
    problem string
}

func (e *GeoipError) Error() string {
    return e.problem
}

func (table GeoIPv4Table) Len() int { return len(table) }
func (table GeoIPv6Table) Len() int { return len(table) }

func (table GeoIPv4Table) ElementAt(i int) GeoIPEntry { return table[i] }
func (table GeoIPv6Table) ElementAt(i int) GeoIPEntry { return table[i] }

// Convert a geoip IP address represented as unsigned integer to net.IP
func geoipStringToIP(ipStr string) net.IP {
    ip, err := strconv.ParseUint(ipStr, 10, 32)
    if err != nil {
        log.Println("error parsing IP ", ipStr)
        return net.IPv4(0,0,0,0)
    }
    var bytes [4]byte
    bytes[0] = byte(ip & 0xFF)
    bytes[1] = byte((ip >> 8) & 0xFF)
    bytes[2] = byte((ip >> 16) & 0xFF)
    bytes[3] = byte((ip >> 24) & 0xFF)

    return net.IPv4(bytes[3],bytes[2],bytes[1],bytes[0])
}

//Parses a line in the provided geoip file that corresponds
//to an address range and a two character country code
func (table *GeoIPv4Table) parseEntry(candidate string) error {

    if candidate[0] == '#' {
        return nil
    }

    parsedCandidate := strings.Split(candidate, ",")

    if len(parsedCandidate) != 3 {
        log.Println("Received strings", parsedCandidate)
        return &GeoipError{
            problem: "Provided geoip file is incorrectly formatted",
        }
    }

    geoipEntry := GeoIPEntry{
        ipLow:      geoipStringToIP(parsedCandidate[0]),
        ipHigh:     geoipStringToIP(parsedCandidate[1]),
        country:    parsedCandidate[2],
    }

    *table = append(*table, geoipEntry)
    return nil
}

//Parses a line in the provided geoip file that corresponds
//to an address range and a two character country code
func (table *GeoIPv6Table) parseEntry(candidate string) error {

    if candidate[0] == '#' {
        return nil
    }

    parsedCandidate := strings.Split(candidate, ",")

    if len(parsedCandidate) != 3 {
        return &GeoipError{
            problem: "Provided geoip file is incorrectly formatted",
        }
    }

    geoipEntry := GeoIPEntry{
        ipLow:      net.ParseIP(parsedCandidate[0]),
        ipHigh:     net.ParseIP(parsedCandidate[1]),
        country:    parsedCandidate[2],
    }

    *table = append(*table, geoipEntry)
    return nil
}
//Loads provided geoip file into our tables
//Entries are stored in a table
func GeoIPLoadFile(table GeoIPTable, pathname string) error {
    //open file
    geoipFile , err := os.Open(pathname)
    if err != nil {
        log.Println("Error: " + err.Error())
        return err
    }
    defer geoipFile.Close()

    //read in strings and call parse function
    scanner := bufio.NewScanner(geoipFile)
    for scanner.Scan() {
        err = table.parseEntry(scanner.Text())
        if err != nil {
            log.Println("Error: " + err.Error())
            return err
        }
    }

    log.Println("Loaded ", table.Len(), " entries into table")

    return nil
}

//Determines whether the given IP address (key) is included in or less 
//than the IP range of the Geoip entry.
//Outputs 0 if key is greater than the entry's IP range and 1 otherwise
func GeoIPRangeClosure(key net.IP, entry GeoIPEntry) bool {
    a := key.To16()
    b := entry.ipHigh.To16()

    for i, v := range a {
        if v != b[i] {
            return v < b[i]
        }
    }

    return true
}

func GeoIPCheckRange (key net.IP, entry GeoIPEntry) bool {
    a := key.To16()
    b := entry.ipLow.To16()
    c := entry.ipHigh.To16()

    for i, v := range a {
        if v < b[i] || v > c[i] {
            return false
        }
    }

    return true
}

//Returns the country location of an IPv4 or IPv6 address.
func GetCountryByAddr(table GeoIPTable, addr string) string {
    //translate addr string to IP
    ip := net.ParseIP(addr)

    //look IP up in database
    index := sort.Search(table.Len(), func(i int) bool {
        return GeoIPRangeClosure(ip, table.ElementAt(i))
    })

    if index == table.Len() {
        return ""
    }

    // check to see if addr is in the range specified by the returned index
    // search on IPs in invalid ranges (e.g., 127.0.0.0/8) will return the
    //country code of the next highest range
    log.Println("Checking index ", index)
    if ! GeoIPCheckRange(ip, table.ElementAt(index)) {
        return ""
    }

    return table.ElementAt(index).country

}

