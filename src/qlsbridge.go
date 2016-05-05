package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	log "github.com/Sirupsen/logrus"
)

var (
	port        int
	useGzip     bool
	timeoutSecs int
	logger      *log.Logger
)

const (
	portFlag              = "port"
	useGzipFlag           = "gzip"
	timeoutFlag           = "timeout"
	allRankingsEndpoint   = "/allrankings"
	rankingsEndpoint      = "/rankings"
	rankedServersEndpoint = "/rankedservers"
)

// qlStatServers is a slice of structs representing the JSON array of
// servers returned by the qlstats /api/server/skillrating endpoint.
type qlStatServers []struct {
	Server string `json:"server"`
	IP     string `json:"ip"`
	Gt     string `json:"gt"`
	Min    int    `json:"min"`
	Avg    int    `json:"avg"`
	Max    int    `json:"max"`
	Pc     int    `json:"pc"`
	Sc     int    `json:"sc"`
	Bc     int    `json:"bc"`
}

// qlStatPlayers represents the ranking data returned by the qlstats /api/server/
// host/players endpoint.
type qlStatPlayers struct {
	Ok         bool
	Players    []rankedPlayer
	ServerInfo struct {
		Server   string
		IP       string
		Gt       string
		Min      int
		Avg      int
		Max      int
		Pc       int
		Sc       int
		Bc       int
		Rating   string
		Map      string
		MapStart interface{} // this is broken in the QLStats API (can be string or int)
	}
}

// apiRankingResponse is the response returned by our ranking endpoints.
type apiRankingResponse struct {
	RankedServerCount int            `json:"rankedServerCount"`
	RankedPlayerCount int            `json:"rankedPlayerCount"`
	RankedPlayers     []rankedPlayer `json:"rankedPlayers"`
}

// rankedPlayer represents an individual ranked player's data returned by the
// qlstats /api/server/host/players endpoint.
type rankedPlayer struct {
	SteamID string `json:"steamID"`
	Name    string `json:"name"`
	Team    int    `json:"team"`
	Rating  int    `json:"rating"`
	Rd      int    `json:"round"`
	Time    int64  `json:"time"`
	Server  string `json:"server"` // This is added by us for indexing purposes.
	IP      string `json:"ip"`     // This is added by us for indexing purposes.
}

func setupLogging() error {
	logfile, err := os.OpenFile("qlsbridge.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND,
		0666)
	if err != nil {
		return err
	}
	logger = log.New()
	logger.Out = logfile
	logger.Formatter = &log.TextFormatter{
		TimestampFormat: "01/02/2006 15:04:05 EST",
	}
	return nil
}

func init() {
	flag.IntVar(&port, portFlag, 40081, "The HTTP server port")
	flag.BoolVar(&useGzip, useGzipFlag, false, "Use gzip compression on response")
	flag.IntVar(&timeoutSecs, timeoutFlag, 10, "The timeout, in secs, for HTTP requests")
	if err := setupLogging(); err != nil {
		panic("Unable to setup logging")
	}
}

func main() {
	flag.Parse()
	start(port)
}

func start(httpPort int) {
	registerHandlers()
	err := http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil)
	if err != nil {
		panic(fmt.Sprintf("Unable to start HTTP server: %s", err))
	}
}

func getPopulatedRankedServers() ([]string, error) {
	srvs, err := getQLStatsServers()
	if err != nil {
		logger.Errorf("Error getting populated ranked servers: %s", err)
		return nil, err
	}
	var servers []string
	for _, s := range srvs {
		if s.Pc > 0 || s.Sc > 0 {
			servers = append(servers, s.Server)
		}
	}
	return servers, nil
}

func getQLStatsServers() (qlStatServers, error) {
	res, err := http.Get("http://api.qlstats.net/api/server/skillrating")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var q qlStatServers
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&q); err != nil {
		return nil, err
	}
	var srvs qlStatServers
	for _, s := range q {
		s.IP = strings.Split(s.Server, ":")[0]
		srvs = append(srvs, s)
	}
	return srvs, nil
}

func getQLStatsPlayerRankings(servers []string) (apiRankingResponse, error) {
	var rankingResponse apiRankingResponse
	rankingResponse.RankedPlayers = make([]rankedPlayer, 0) // JSON: empty vs. null
	var successCount int
	var wg sync.WaitGroup
	var mut sync.Mutex

	for _, s := range servers {
		wg.Add(1)
		go func(addr string) {
			r, err := http.Get(fmt.Sprintf("http://api.qlstats.net/api/server/%s/players",
				addr))
			if err != nil {
				logger.Errorf("Error requesting data for %s: %s", addr, err)
				return
			}
			defer r.Body.Close()
			qp := qlStatPlayers{}
			dec := json.NewDecoder(r.Body)
			if err := dec.Decode(&qp); err != nil {
				logger.Errorf("JSON decode error for %s: %s", addr, err)
				return
			}
			for _, p := range qp.Players {
				mut.Lock()
				p.Server = addr
				p.IP = strings.Split(addr, ":")[0]
				rankingResponse.RankedPlayers = append(rankingResponse.RankedPlayers, p)
				mut.Unlock()
			}
			successCount++
			wg.Done()
		}(s)
	}
	wg.Wait()
	rankingResponse.RankedServerCount = successCount
	rankingResponse.RankedPlayerCount = len(rankingResponse.RankedPlayers)
	return rankingResponse, nil
}
