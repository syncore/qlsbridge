package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"

	log "github.com/Sirupsen/logrus"
)

var (
	port    int
	useGzip bool
	logger  *log.Logger
)

const (
	portFlag            = "port"
	useGzipFlag         = "gzip"
	allRankingsEndpoint = "/allrankings"
	rankingsEndpoint    = "/rankings"
)

// qlStatServer represents an individual server returned by the qlstats
// /api/server/skillrating endpoint.
type qlStatServer struct {
	Server string
	Gt     string
	Min    int
	Avg    int
	Max    int
	Pc     int
	Sc     int
	Bc     int
}

// qlStatPlayers represents the ranking data returned by the qlstats /api/server/
// host/players endpoint.
type qlStatPlayers struct {
	Ok         bool
	Players    []rankedPlayer
	ServerInfo struct {
		Server   string
		Gt       string
		Min      int
		Avg      int
		Max      int
		Pc       int
		Sc       int
		Bc       int
		Rating   string
		Map      string
		MapStart string
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
	SteamID string
	Name    string
	Team    int
	Rating  int
	Rd      int
	Time    int64
	Server  string // This is added by us for indexing purposes.
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

func getQLStatsServers() ([]qlStatServer, error) {
	res, err := http.Get("http://api.qlstats.net/api/server/skillrating")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var srvs []qlStatServer
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&srvs); err != nil {
		return nil, err
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
