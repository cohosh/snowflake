package main

import (
	// "golang.org/x/net/internal/timeseries"
	"time"
        "net"
        "fmt"
        "log"
        "os"
)

type CountryStats struct {
    counts map[string]int
}

// Implements Observable
type Metrics struct {
        tablev4 *GeoIPv4Table
        tablev6 *GeoIPv6Table
        countryStats CountryStats
	// snowflakes	timeseries.Float
	clientRoundtripEstimate time.Duration
}

func (s CountryStats) Display() string {
    return fmt.Sprintln(s.counts)
}

func (m *Metrics) UpdateCountryStats(addr string) {

    var country string

    if net.ParseIP(addr).To4() != nil {
        //This is an IPv4 address
        if m.tablev4 == nil {
            return
        }
        country = GetCountryByAddr(m.tablev4, addr)
    } else {
        if m.tablev6 == nil {
            return
        }
        country = GetCountryByAddr(m.tablev6, addr)
    }

    //update map of countries and counts
    if country != "" {
        m.countryStats.counts[country] = m.countryStats.counts[country] + 1
    }

}

func (m *Metrics) LoadGeoipDatabases(geoipDB string, geoip6DB string) {

        // Load geoip databases
        log.Println("Loading geoip databases")
        tablev4 := new(GeoIPv4Table)
        err := GeoIPLoadFile(tablev4, geoipDB)
        if err != nil {
            m.tablev4 = nil
            log.Println("Failed to load geoip database ")
        } else {
            m.tablev4 = nil // garbage collect previous table, if applicable
            m.tablev4 = tablev4
        }

        tablev6 := new(GeoIPv6Table)
        err = GeoIPLoadFile(tablev6, geoip6DB)
        if err != nil {
            m.tablev6 = nil
            log.Println("Failed to load geoip6 database ")
        } else {
            m.tablev6 = nil // garbage collect previous table, if applicable
            m.tablev6 = tablev6
        }
}

func NewMetrics() *Metrics {
	m := new(Metrics)

        f, err := os.OpenFile("metrics.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

        if err != nil {
            log.Println("Couldn't open file for metrics logging")
            return nil
        }

        metricsLogger := log.New(f, "", log.LstdFlags | log.LUTC)

        m.countryStats = CountryStats{
            counts: make(map[string]int),
        }

        // Write to log file every hour with updated metrics
        heartbeat := time.Tick(time.Hour)
        go func() {
            for {
                <-heartbeat
                metricsLogger.Println("Country stats: ", m.countryStats.Display())

                //restore all metrics to original values
                m.countryStats.counts = make(map[string]int)

            }
        }()

	return m
}
