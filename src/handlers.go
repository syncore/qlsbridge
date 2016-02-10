package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

func registerHandlers() {
	allRankings := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		if r.Method != "GET" {
			writeResponseError(http.StatusMethodNotAllowed, w, r)
		}
		if r.URL.Path != allRankingsEndpoint {
			writeResponseError(http.StatusNotFound, w, r)
			return
		}
		allRankingsHandler(w, r)
	})
	rankings := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		if r.Method != "GET" {
			writeResponseError(http.StatusMethodNotAllowed, w, r)
		}
		if r.URL.Path != rankingsEndpoint {
			writeResponseError(http.StatusNotFound, w, r)
			return
		}
		rankingsHandler(w, r)
	})
	if useGzip {
		http.Handle(allRankingsEndpoint, timeoutHandler(GzipHandler(allRankings)))
		http.Handle(rankingsEndpoint, timeoutHandler(GzipHandler(rankings)))
	} else {
		http.Handle(allRankingsEndpoint, timeoutHandler(allRankings))
		http.Handle(rankingsEndpoint, timeoutHandler(rankings))
	}
}

func timeoutHandler(h http.Handler) http.Handler {
	return http.TimeoutHandler(h, time.Duration(2)*time.Second,
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
