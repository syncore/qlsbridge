package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

func wrapHandlerFunc(method, path string, handler http.Handler) http.Handler {
	wrapped := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json; charset=UTF-8")
		if req.Method != method {
			writeResponseError(http.StatusMethodNotAllowed, rw, req)
			return
		}
		if req.URL.Path != path {
			writeResponseError(http.StatusNotFound, rw, req)
			return
		}
		handler = timeoutHandler(handler)
		handler.ServeHTTP(rw, req)
	})
	return http.HandlerFunc(wrapped)
}

func registerHandlers() {
	// GET: http://server/allrankings
	allRankings := wrapHandlerFunc("GET", allRankingsEndpoint,
		http.HandlerFunc(allRankingsHandler))
	// GET: http://server/rankings?servers=ip1,ip2,ipn
	rankings := wrapHandlerFunc("GET", rankingsEndpoint,
		http.HandlerFunc(rankingsHandler))
	// GET: http://server/rankedservers
	rankedServers := wrapHandlerFunc("GET", rankedServersEndpoint,
		http.HandlerFunc(rankedServersHandler))
	if useGzip {
		http.Handle(allRankingsEndpoint, GzipHandler(allRankings))
		http.Handle(rankingsEndpoint, GzipHandler(rankings))
		http.Handle(rankedServersEndpoint, GzipHandler(rankedServers))
	} else {
		http.Handle(allRankingsEndpoint, allRankings)
		http.Handle(rankingsEndpoint, rankings)
		http.Handle(rankedServersEndpoint, rankedServers)
	}
}

func rankedServersHandler(w http.ResponseWriter, r *http.Request) {
	servers, err := getQLStatsServers()
	if err != nil {
		writeResponseError(http.StatusInternalServerError, w, r)
		return
	}
	if err := json.NewEncoder(w).Encode(servers); err != nil {
		writeResponseError(http.StatusInternalServerError, w, r)
		return
	}
}

func timeoutHandler(h http.Handler) http.Handler {
	return http.TimeoutHandler(h, time.Duration(timeoutSecs)*time.Second,
		`{"error": {"code": 503,"message": "Request timeout."}}`)
}

func rankingsHandler(w http.ResponseWriter, r *http.Request) {
	for q := range r.URL.Query() {
		if strings.EqualFold(q, "servers") {
			break
		}
		writeResponseError(http.StatusNotFound, w, r)
		return
	}
	addresses := getQueryStringVals(r.URL.Query(), "servers")
	if addresses == nil {
		writeResponseError(http.StatusNotFound, w, r)
		return
	}
	var parsedaddresses []string
	for _, addr := range addresses {
		host, err := net.ResolveTCPAddr("tcp4", addr)
		if err != nil {
			continue
		}
		parsedaddresses = append(parsedaddresses, fmt.Sprintf("%s:%d", host.IP,
			host.Port))
	}
	if len(parsedaddresses) == 0 {
		writeResponseError(http.StatusNotFound, w, r)
		return
	}
	players, err := getQLStatsPlayerRankings(parsedaddresses)
	if err != nil {
		writeResponseError(http.StatusInternalServerError, w, r)
		return
	}
	if err := json.NewEncoder(w).Encode(players); err != nil {
		writeResponseError(http.StatusInternalServerError, w, r)
		return
	}
}

func allRankingsHandler(w http.ResponseWriter, r *http.Request) {
	servers, err := getPopulatedRankedServers()
	if err != nil {
		writeResponseError(http.StatusInternalServerError, w, r)
		return
	}
	players, err := getQLStatsPlayerRankings(servers)
	if err != nil {
		writeResponseError(http.StatusInternalServerError, w, r)
		return
	}
	if err := json.NewEncoder(w).Encode(players); err != nil {
		writeResponseError(http.StatusInternalServerError, w, r)
		return
	}
}
func getQueryStringVals(m map[string][]string, querystring string) []string {
	var vals []string
	for k := range m {
		if strings.EqualFold(k, querystring) {
			vals = strings.Split(m[k][0], ",")
			break
		}
	}
	if vals == nil {
		return nil
	}
	if vals[0] == "" {
		return nil
	}
	return vals
}

func writeResponseError(statusCode int, w http.ResponseWriter, r *http.Request) {
	var msg string
	switch statusCode {
	case http.StatusNotFound: // 404
		msg = "Not found"
		break
	case http.StatusMethodNotAllowed: // 405
		msg = "Not allowed"
		break
	case http.StatusInternalServerError: // 500
		msg = "Server error"
		break
	}
	w.WriteHeader(statusCode)
	fmt.Fprintf(w, `{"error": {"code": %d,"message": "%s"}}`, statusCode, msg)
}
